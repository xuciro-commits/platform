package host

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	runtimeprotocol "platformserver/kernel/protocol"
	"platformserver/kernel/transport"
	"platformserver/platform"
	"platformserver/services/security/idp"
)

func TestInvariantOnboardingEmailVerificationAndRateLimits(t *testing.T) {
	now := invariantClock
	key, err := idp.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	signer, err := idp.NewLocal(key)
	if err != nil {
		t.Fatal(err)
	}
	mailbox, err := idp.StartLoopbackMailbox(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mailbox.Close() })

	live := newInvariantTenant(t, "onboarding-51a", runtimeprotocol.Seat{
		Subjects: []string{"user:alice@example.test"},
		Member:   platform.Member{ID: "alice", Roles: map[string]string{"platform": "admin"}},
	})
	live.settings.Defaults(map[string]string{"name": "晨星制造"})

	accounts := idp.NewAccounts(signer, idp.AccountOptions{
		Path:          t.TempDir() + "/idp-accounts.json",
		TokenTTL:      12 * time.Hour,
		CodeTTL:       10 * time.Minute,
		MaxCodesEmail: 3,
		MaxCodesIP:    6,
		MaxVerifyIP:   10,
		RateWindow:    10 * time.Minute,
		Captured:      mailbox,
	})
	h := NewHost(nil, live.Tenant)
	h.Now = func() time.Time { return now }
	h.Development = true
	h.MailEndpoint = mailbox.Endpoint()
	h.EnableLocalOnboarding(accounts)
	handler := h.Handler()

	callJSON := func(method, path, ip, bearer string, body any) (*httptest.ResponseRecorder, []byte) {
		t.Helper()
		var raw []byte
		if body != nil {
			raw, _ = json.Marshal(body)
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(raw))
		req.RemoteAddr = ip + ":54321"
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec, rec.Body.Bytes()
	}

	rec, raw := callJSON(http.MethodGet, "/v1/sign-in", "127.0.0.1", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/sign-in status = %d: %s", rec.Code, raw)
	}
	var signIn transport.SignIn
	if err := json.Unmarshal(raw, &signIn); err != nil || !signIn.LocalAuth {
		t.Fatalf("expected localAuth=true in /v1/sign-in, got %+v (err=%v)", signIn, err)
	}

	rec, raw = callJSON(http.MethodPost, "/v1/auth/email/code", "198.51.100.10", "", runtimeprotocol.EmailCodeRequest{Email: "  Alice@Example.TEST "})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /v1/auth/email/code status = %d: %s", rec.Code, raw)
	}
	var codeAns runtimeprotocol.EmailCodeAnswer
	if err := json.Unmarshal(raw, &codeAns); err != nil || codeAns.ExpiresInSeconds != 600 {
		t.Fatalf("unexpected code answer: %+v (err=%v)", codeAns, err)
	}

	rec, raw = callJSON(http.MethodGet, "/v1/dev/mailbox?to=alice@example.test", "127.0.0.1", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/dev/mailbox status = %d: %s", rec.Code, raw)
	}
	var letters []runtimeprotocol.CapturedMail
	if err := json.Unmarshal(raw, &letters); err != nil || len(letters) != 1 {
		t.Fatalf("expected 1 captured letter via SMTP, got %d (err=%v): %s", len(letters), err, raw)
	}
	digits := regexp.MustCompile(`\b(\d{6})\b`).FindStringSubmatch(letters[0].Text)
	if len(digits) < 2 {
		t.Fatalf("letter body lacks 6-digit code: %q", letters[0].Text)
	}
	firstCode := digits[1]

	now = now.Add(10 * time.Minute)
	rec, raw = callJSON(http.MethodPost, "/v1/auth/email/verify", "198.51.100.10", "", runtimeprotocol.EmailVerifyRequest{
		Email: "alice@example.test",
		Code:  firstCode,
	})
	if rec.Code != http.StatusUnauthorized && rec.Code != http.StatusBadRequest {
		t.Fatalf("expected expired code to fail after 10m, got %d: %s", rec.Code, raw)
	}

	now = now.Add(time.Minute)
	rec, raw = callJSON(http.MethodPost, "/v1/auth/email/code", "198.51.100.10", "", runtimeprotocol.EmailCodeRequest{Email: "alice@example.test"})
	if rec.Code != http.StatusOK {
		t.Fatalf("second code request failed: %d %s", rec.Code, raw)
	}
	_, raw = callJSON(http.MethodGet, "/v1/dev/mailbox?to=alice@example.test", "127.0.0.1", "", nil)
	_ = json.Unmarshal(raw, &letters)
	if len(letters) != 2 {
		t.Fatalf("expected 2 captured letters, got %d", len(letters))
	}
	secondCode := regexp.MustCompile(`\b(\d{6})\b`).FindStringSubmatch(letters[len(letters)-1].Text)[1]

	now = now.Add(2 * time.Minute)
	rec, raw = callJSON(http.MethodPost, "/v1/auth/email/verify", "198.51.100.10", "", runtimeprotocol.EmailVerifyRequest{
		Email: "ALICE@EXAMPLE.TEST",
		Code:  secondCode,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("verify fresh code failed: %d %s", rec.Code, raw)
	}
	var firstSession runtimeprotocol.AuthSession
	if err := json.Unmarshal(raw, &firstSession); err != nil {
		t.Fatal(err)
	}
	if firstSession.Token == "" || firstSession.AccountID == "" || firstSession.Email != "alice@example.test" || firstSession.Subject != "user:alice@example.test" {
		t.Fatalf("unexpected verified session: %+v", firstSession)
	}
	if len(firstSession.Tenants) != 1 || firstSession.Tenants[0].ID != "onboarding-51a" || firstSession.Tenants[0].Name != "晨星制造" {
		t.Fatalf("expected tenant membership in verified session, got %+v", firstSession.Tenants)
	}

	rec, raw = callJSON(http.MethodPost, "/v1/auth/email/verify", "198.51.100.10", "", runtimeprotocol.EmailVerifyRequest{
		Email: "alice@example.test",
		Code:  secondCode,
	})
	if rec.Code == http.StatusOK {
		t.Fatalf("reused one-time code was accepted a second time: %s", raw)
	}

	rec, _ = callJSON(http.MethodGet, "/v1/auth/session", "198.51.100.10", "user:alice@example.test", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unsigned user subject bearer token must be rejected, got %d", rec.Code)
	}

	rec, raw = callJSON(http.MethodGet, "/v1/auth/session", "198.51.100.10", firstSession.Token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/auth/session failed: %d %s", rec.Code, raw)
	}

	rec, raw = callJSON(http.MethodPost, "/v1/auth/email/code", "198.51.100.11", "", runtimeprotocol.EmailCodeRequest{Email: "alice@example.test"})
	if rec.Code != http.StatusOK {
		t.Fatalf("re-signup code request failed: %d %s", rec.Code, raw)
	}
	_, raw = callJSON(http.MethodGet, "/v1/dev/mailbox?to=alice@example.test", "127.0.0.1", "", nil)
	_ = json.Unmarshal(raw, &letters)
	thirdCode := regexp.MustCompile(`\b(\d{6})\b`).FindStringSubmatch(letters[len(letters)-1].Text)[1]
	rec, raw = callJSON(http.MethodPost, "/v1/auth/email/verify", "198.51.100.11", "", runtimeprotocol.EmailVerifyRequest{
		Email: "alice@example.test",
		Code:  thirdCode,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("re-signup verify failed: %d %s", rec.Code, raw)
	}
	var secondSession runtimeprotocol.AuthSession
	_ = json.Unmarshal(raw, &secondSession)
	if secondSession.AccountID != firstSession.AccountID || secondSession.Subject != firstSession.Subject || accounts.Count() != 1 {
		t.Fatalf("same email created a second account: first=%+v second=%+v count=%d", firstSession, secondSession, accounts.Count())
	}

	for i := 0; i < 2; i++ {
		rec, _ = callJSON(http.MethodPost, "/v1/auth/email/code", "198.51.100.20", "", runtimeprotocol.EmailCodeRequest{Email: "alice@example.test"})
	}
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("expected per-email rate limit 429 with Retry-After, got %d (Retry-After=%q)", rec.Code, rec.Header().Get("Retry-After"))
	}

	for i := 0; i < 7; i++ {
		email := "stranger-" + string(rune('a'+i)) + "@example.test"
		rec, _ = callJSON(http.MethodPost, "/v1/auth/email/code", "203.0.113.77", "", runtimeprotocol.EmailCodeRequest{Email: email})
	}
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("expected per-IP rate limit 429 with Retry-After, got %d (Retry-After=%q)", rec.Code, rec.Header().Get("Retry-After"))
	}

	prod := NewHost(nil, live.Tenant)
	prod.Development = false
	prod.MailEndpoint = mailbox.Endpoint()
	prod.EnableLocalOnboarding(accounts)
	prodRec := httptest.NewRecorder()
	prodReq := httptest.NewRequest(http.MethodGet, "/v1/dev/mailbox?to=alice@example.test", nil)
	prodReq.RemoteAddr = "127.0.0.1:54321"
	prod.Handler().ServeHTTP(prodRec, prodReq)
	if prodRec.Code != http.StatusNotFound {
		t.Fatalf("non-development host exposed /v1/dev/mailbox: got %d", prodRec.Code)
	}
	if strings.Contains(firstSession.Token, ".") == false {
		t.Fatalf("expected signed JWT from local IDP, got %q", firstSession.Token)
	}
}
