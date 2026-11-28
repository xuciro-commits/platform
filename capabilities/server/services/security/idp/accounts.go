package idp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/mail"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/protocol"
	"platformserver/platform"
)

type Account struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	Subject      string    `json:"subject"`
	CreatedAt    time.Time `json:"createdAt"`
	LastSignInAt time.Time `json:"lastSignInAt"`
}

type AccountOptions struct {
	Path          string
	TokenTTL      time.Duration
	CodeTTL       time.Duration
	RateWindow    time.Duration
	MaxCodesEmail int
	MaxCodesIP    int
	MaxVerifyIP   int
	Captured      *LoopbackMailbox
}

type emailChallenge struct {
	salt      string
	digest    []byte
	expiresAt time.Time
	attempts  int
	used      bool
}

type Accounts struct {
	signer     *Local
	opts       AccountOptions
	mu         sync.Mutex
	byEmail    map[string]*Account
	challenges map[string]*emailChallenge
	emailHits  map[string][]time.Time
	ipCodeHits map[string][]time.Time
	ipAuthHits map[string][]time.Time
	mailSeq    uint64
}

func NewAccounts(signer *Local, opts AccountOptions) *Accounts {
	if opts.TokenTTL <= 0 {
		opts.TokenTTL = 12 * time.Hour
	}
	if opts.CodeTTL <= 0 {
		opts.CodeTTL = 10 * time.Minute
	}
	if opts.RateWindow <= 0 {
		opts.RateWindow = 10 * time.Minute
	}
	if opts.MaxCodesEmail <= 0 {
		opts.MaxCodesEmail = 3
	}
	if opts.MaxCodesIP <= 0 {
		opts.MaxCodesIP = 12
	}
	if opts.MaxVerifyIP <= 0 {
		opts.MaxVerifyIP = 24
	}
	a := &Accounts{
		signer:     signer,
		opts:       opts,
		byEmail:    map[string]*Account{},
		challenges: map[string]*emailChallenge{},
		emailHits:  map[string][]time.Time{},
		ipCodeHits: map[string][]time.Time{},
		ipAuthHits: map[string][]time.Time{},
	}
	a.loadLocked()
	return a
}

func (a *Accounts) Key() []byte { return a.signer.Key() }

func (a *Accounts) Captured() *LoopbackMailbox { return a.opts.Captured }

func (a *Accounts) Mint(subject string, now time.Time) string {
	token, err := a.signer.Mint(subject, a.opts.TokenTTL, now)
	if err != nil {
		log.Printf("mint token: %v", err)
	}
	return token
}

func (a *Accounts) Authenticate(development bool, now func() time.Time) Authenticate {
	return func(credential string) (string, bool) {
		credential = strings.TrimSpace(credential)
		if credential == "" {
			return "", false
		}
		if strings.Contains(credential, ".") {
			return a.signer.VerifyAt(credential, now())
		}
		if development && !strings.HasPrefix(credential, "user:") && !strings.HasPrefix(credential, "client:") {
			return credential, true
		}
		return "", false
	}
}

func (a *Accounts) Count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.loadLocked()
	return len(a.byEmail)
}

func NormalizeEmail(raw string) (string, bool) {
	trimmed := strings.ToLower(strings.TrimSpace(raw))
	if trimmed == "" || len(trimmed) > 254 || strings.ContainsAny(trimmed, " \t\r\n") {
		return "", false
	}
	parsed, err := mail.ParseAddress(trimmed)
	if err != nil || strings.ToLower(parsed.Address) != trimmed {
		return "", false
	}
	local, domain, ok := strings.Cut(trimmed, "@")
	if !ok || local == "" || domain == "" || !strings.Contains(domain, ".") || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return "", false
	}
	return trimmed, true
}

func (a *Accounts) RequestCode(rawEmail, ip string, now time.Time, deliver func(to, subject, text, id string) error) (protocol.EmailCodeAnswer, time.Duration, *platform.Error) {
	now = now.UTC()
	a.mu.Lock()
	if retry, ok := allowWindow(a.ipCodeHits, ip, a.opts.MaxCodesIP, a.opts.RateWindow, now); !ok {
		a.mu.Unlock()
		return protocol.EmailCodeAnswer{}, retry, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Too many requests from this address. Try again shortly.")
	}
	email, valid := NormalizeEmail(rawEmail)
	if !valid {
		a.mu.Unlock()
		return protocol.EmailCodeAnswer{}, 0, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Enter a valid email address.")
	}
	if retry, ok := allowWindow(a.emailHits, email, a.opts.MaxCodesEmail, a.opts.RateWindow, now); !ok {
		a.mu.Unlock()
		return protocol.EmailCodeAnswer{}, retry, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Too many verification codes requested for this email. Try again shortly.")
	}
	code, err := randomSixDigits()
	if err != nil {
		a.mu.Unlock()
		return protocol.EmailCodeAnswer{}, 0, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Verification code could not be generated.")
	}
	salt := randomHex(12)
	a.challenges[email] = &emailChallenge{
		salt:      salt,
		digest:    a.codeDigest(email, salt, code),
		expiresAt: now.Add(a.opts.CodeTTL),
	}
	a.mailSeq++
	msgID := fmt.Sprintf("idp-code:%d:%s", a.mailSeq, salt[:8])
	a.mu.Unlock()

	minutes := int(a.opts.CodeTTL.Minutes())
	if minutes < 1 {
		minutes = 1
	}
	subject := "Your Platform verification code"
	text := fmt.Sprintf("Your Platform verification code is %s.\n\nThis code expires in %d minutes and can be used once.", code, minutes)
	if deliver != nil {
		if err := deliver(email, subject, text, msgID); err != nil {
			return protocol.EmailCodeAnswer{}, 0, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Verification email could not be sent.")
		}
	}
	return protocol.EmailCodeAnswer{ExpiresInSeconds: int(a.opts.CodeTTL.Seconds())}, 0, nil
}

func (a *Accounts) VerifyCode(rawEmail, rawCode, ip string, now time.Time, workspaces func(string) []protocol.AuthWorkspace) (protocol.AuthSession, time.Duration, *platform.Error) {
	now = now.UTC()
	a.mu.Lock()
	defer a.mu.Unlock()
	if retry, ok := allowWindow(a.ipAuthHits, ip, a.opts.MaxVerifyIP, a.opts.RateWindow, now); !ok {
		return protocol.AuthSession{}, retry, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Too many verification attempts from this address. Try again shortly.")
	}
	email, valid := NormalizeEmail(rawEmail)
	code := strings.TrimSpace(rawCode)
	if !valid || len(code) != 6 {
		return protocol.AuthSession{}, 0, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Enter the 6-digit verification code.")
	}
	ch := a.challenges[email]
	if ch == nil || ch.used || !now.Before(ch.expiresAt) {
		return protocol.AuthSession{}, 0, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Verification code is invalid or expired.")
	}
	want := a.codeDigest(email, ch.salt, code)
	if !hmac.Equal(ch.digest, want) {
		ch.attempts++
		if ch.attempts >= 5 {
			ch.used = true
		}
		return protocol.AuthSession{}, 0, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Verification code is invalid or expired.")
	}
	ch.used = true
	a.loadLocked()
	acc := a.byEmail[email]
	if acc == nil {
		acc = &Account{
			ID:           "acc_" + randomHex(12),
			Email:        email,
			Subject:      "user:" + email,
			CreatedAt:    now,
			LastSignInAt: now,
		}
		a.byEmail[email] = acc
	} else {
		acc.LastSignInAt = now
	}
	if err := a.saveLocked(); err != nil {
		return protocol.AuthSession{}, 0, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Account could not be saved.")
	}
	token, err := a.signer.Mint(acc.Subject, a.opts.TokenTTL, now)
	if err != nil {
		return protocol.AuthSession{}, 0, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Session token could not be issued.")
	}
	tenants := []protocol.AuthWorkspace{}
	if workspaces != nil {
		tenants = workspaces(acc.Subject)
	}
	return protocol.AuthSession{
		Token:     token,
		AccountID: acc.ID,
		Email:     acc.Email,
		Subject:   acc.Subject,
		Tenants:   tenants,
	}, 0, nil
}

func (a *Accounts) SessionOf(subject string, workspaces func(string) []protocol.AuthWorkspace) (protocol.AuthSession, bool) {
	if subject == "" {
		return protocol.AuthSession{}, false
	}
	a.mu.Lock()
	a.loadLocked()
	email := strings.TrimPrefix(subject, "user:")
	acc := a.byEmail[strings.ToLower(email)]
	a.mu.Unlock()
	tenants := []protocol.AuthWorkspace{}
	if workspaces != nil {
		tenants = workspaces(subject)
	}
	if acc != nil {
		return protocol.AuthSession{
			AccountID: acc.ID,
			Email:     acc.Email,
			Subject:   acc.Subject,
			Tenants:   tenants,
		}, true
	}
	return protocol.AuthSession{
		AccountID: subject,
		Email:     email,
		Subject:   subject,
		Tenants:   tenants,
	}, true
}

func (a *Accounts) codeDigest(email, salt, code string) []byte {
	mac := hmac.New(sha256.New, a.signer.Key())
	mac.Write([]byte("email-otp:" + email + ":" + salt + ":" + code))
	return mac.Sum(nil)
}

func (a *Accounts) loadLocked() {
	if a.opts.Path == "" {
		return
	}
	raw, err := os.ReadFile(a.opts.Path)
	if err != nil || len(raw) == 0 {
		return
	}
	var list []Account
	if json.Unmarshal(raw, &list) == nil {
		for i := range list {
			item := list[i]
			a.byEmail[item.Email] = &item
		}
	}
}

func (a *Accounts) saveLocked() error {
	if a.opts.Path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(a.opts.Path), 0o700); err != nil {
		return err
	}
	keys := make([]string, 0, len(a.byEmail))
	for k := range a.byEmail {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	list := make([]Account, 0, len(keys))
	for _, k := range keys {
		list = append(list, *a.byEmail[k])
	}
	raw, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(a.opts.Path, append(raw, '\n'))
}

func allowWindow(table map[string][]time.Time, key string, limit int, window time.Duration, now time.Time) (time.Duration, bool) {
	if key == "" {
		key = "unknown"
	}
	if len(table) > 10000 && table[key] == nil {
		clear(table)
	}
	cutoff := now.Add(-window)
	kept := table[key][:0]
	for _, stamp := range table[key] {
		if stamp.After(cutoff) {
			kept = append(kept, stamp)
		}
	}
	if len(kept) >= limit {
		tableRetry := kept[0].Add(window).Sub(now)
		if tableRetry < time.Second {
			tableRetry = time.Second
		}
		table[key] = kept
		return tableRetry, false
	}
	table[key] = append(kept, now)
	return 0, true
}

func randomSixDigits() (string, error) {
	var buf [4]byte
	for {
		if _, err := rand.Read(buf[:]); err != nil {
			return "", err
		}
		v := binary.BigEndian.Uint32(buf[:])
		if v < 4000000000 {
			return fmt.Sprintf("%06d", v%1000000), nil
		}
	}
}

func randomHex(size int) string {
	raw := make([]byte, size)
	_, _ = rand.Read(raw)
	return hex.EncodeToString(raw)
}
