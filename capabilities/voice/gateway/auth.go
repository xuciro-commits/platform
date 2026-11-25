package gateway

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"sync"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/icholy/digest"
)

const (
	nonceLife = 5 * time.Minute
	maxNonces = 1024
)

type verdict int

const (
	challenge verdict = iota
	stale
	forbidden
	allowed
)

type authenticator struct {
	realm  string
	trunks map[string]string
	now    func() time.Time
	mu     sync.Mutex
	nonces map[string]time.Time
}

func newAuthenticator(realm string, trunks map[string]string, now func() time.Time) *authenticator {
	return &authenticator{realm: realm, trunks: trunks, now: now, nonces: map[string]time.Time{}}
}

func (a *authenticator) set(trunks map[string]string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.trunks = trunks
}

func (a *authenticator) password(trunk string) (string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	password, ok := a.trunks[trunk]
	return password, ok
}

func (a *authenticator) check(req *sip.Request) (string, verdict) {
	header := req.GetHeader("Authorization")
	if header == nil {
		return "", challenge
	}
	creds, err := digest.ParseCredentials(header.Value())
	if err != nil || creds.Realm != a.realm {
		return "", challenge
	}
	password, known := a.password(creds.Username)
	if !known {
		return "", forbidden
	}
	if !a.fresh(creds.Nonce) {
		return "", stale
	}
	chal := &digest.Challenge{Realm: a.realm, Nonce: creds.Nonce, Algorithm: "MD5"}
	if creds.QOP == "auth" {
		chal.QOP = []string{"auth"}
	}
	expected, err := digest.Digest(chal, digest.Options{
		Method: req.Method.String(), URI: creds.URI, Username: creds.Username, Password: password,
		Count: creds.Nc, Cnonce: creds.Cnonce,
	})
	if err != nil || subtle.ConstantTimeCompare([]byte(expected.Response), []byte(creds.Response)) != 1 {
		return "", forbidden
	}
	return creds.Username, allowed
}

func (a *authenticator) fresh(nonce string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	issued, ok := a.nonces[nonce]
	return ok && a.now().Sub(issued) < nonceLife
}

func (a *authenticator) issue() string {
	nonce := randomHex(16)
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	for n, issued := range a.nonces {
		if now.Sub(issued) >= nonceLife || len(a.nonces) >= maxNonces {
			delete(a.nonces, n)
		}
	}
	a.nonces[nonce] = now
	return nonce
}

func (a *authenticator) refuse(req *sip.Request, tx sip.ServerTransaction, v verdict) {
	if v == forbidden {
		tx.Respond(sip.NewResponseFromRequest(req, sip.StatusForbidden, "Forbidden", nil))
		return
	}
	chal := digest.Challenge{Realm: a.realm, Nonce: a.issue(), Algorithm: "MD5", QOP: []string{"auth"}, Stale: v == stale}
	res := sip.NewResponseFromRequest(req, sip.StatusUnauthorized, "Unauthorized", nil)
	res.AppendHeader(sip.NewHeader("WWW-Authenticate", chal.String()))
	tx.Respond(res)
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}
