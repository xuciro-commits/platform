package platformserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	"strings"
	"testing"
	"time"
)

func TestDeploymentHostAdministratorsAreExplicit(t *testing.T) {
	tenant, err := NewTenant("host-admin-boundary", NewConsole("host-admin-boundary", Seat{Subjects: []string{"tenant-admin"}, Member: platform.Member{ID: "tenant-admin", Roles: map[string]string{PlatformApp: Admin}}}))
	if err != nil {
		t.Fatal(err)
	}
	auth := func(token string) (string, bool) { return token, token != "" }
	for _, setting := range []string{"", " ops , , support "} {
		d := Deployment{HostAdmins: setting}
		host := NewHost(auth, tenant)
		host.HostAdmins = d.hostAdministrators()
		handler := host.Handler()
		for _, subject := range []string{"tenant-admin", "ops", "support", "outsider"} {
			req := httptest.NewRequest("GET", "/v1/host/me", nil)
			req.Header.Set("Authorization", "Bearer "+subject)
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)
			expected := http.StatusUnauthorized
			if setting != "" && (subject == "ops" || subject == "support") {
				expected = http.StatusOK
			}
			if res.Code != expected {
				t.Fatalf("host admins=%q subject=%q: %d, want %d", setting, subject, res.Code, expected)
			}
		}
	}
}

func TestDeliveryPersonalTokensRequirePrivateKey(t *testing.T) {
	tokenKeyMu.RLock()
	before := append([]byte{}, tokenKey...)
	tokenKeyMu.RUnlock()
	t.Cleanup(func() { UseTokenKey(before) })
	d := Deployment{Issuer: "https://issuer.example.test"}
	t.Setenv("PLATFORM_PERSONAL_TOKEN_KEY", "")
	if d.configurePersonalTokens() == nil {
		t.Fatal("OIDC uses the development token key")
	}
	t.Setenv("PLATFORM_PERSONAL_TOKEN_KEY", "short")
	if d.configurePersonalTokens() == nil {
		t.Fatal("short token key accepted")
	}
	t.Setenv("PLATFORM_PERSONAL_TOKEN_KEY", strings.Repeat("a", 32))
	if err := d.configurePersonalTokens(); err != nil {
		t.Fatal(err)
	}
	first := tokenSecret("tenant", "token", "change")
	if err := d.configurePersonalTokens(); err != nil {
		t.Fatal(err)
	}
	if tokenSecret("tenant", "token", "change") != first {
		t.Fatal("same key does not retain token")
	}
	t.Setenv("PLATFORM_PERSONAL_TOKEN_KEY", strings.Repeat("b", 32))
	if err := d.configurePersonalTokens(); err != nil {
		t.Fatal(err)
	}
	if tokenSecret("tenant", "token", "change") == first {
		t.Fatal("another host key accepts the token")
	}
}

func TestRecoveredPersonalTokenDoesNotReopenSecretWindow(t *testing.T) {
	ctx := context.Background()
	journal, err := OpenFileJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	compose := func() *Tenant {
		tn, err := NewTenant("token-recovery", NewConsole("token-recovery", Seat{Subjects: []string{"admin"}, Member: platform.Member{ID: "admin", Roles: map[string]string{PlatformApp: Admin}}}))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	live := compose()
	if _, err := journal.Entries(ctx, live.ID, 0); err != nil {
		t.Fatal(err)
	}
	live.attachJournal(ctx, journal)
	admin, _ := live.Member("admin")
	now := time.Now().UTC()
	sub := &pb.Submission{TenantId: live.ID, PrincipalId: admin.ID, Authority: PlatformApp, IdempotencyKey: "issue", Target: &pb.EntityRef{Type: TokenType, Id: "ci"}, Schema: &pb.SchemaRef{Name: SchemaTokenIssue, Version: 1}, Payload: []byte(`{"label":"CI"}`)}
	if _, err := live.Submit(admin, sub, now); err != nil {
		t.Fatal(err)
	}
	secret, ok := consoleOf(live).Minted("ci", "admin", now)
	if !ok {
		t.Fatal("issuing process did not deliver secret")
	}
	recovered := compose()
	d := Deployment{}
	if err := d.restoreTenants(ctx, journal, "", []*Tenant{recovered}, map[string]int64{}, map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	if _, ok := consoleOf(recovered).MemberByToken(secret, now.Add(time.Minute)); !ok {
		t.Fatal("token failed after recovery")
	}
	if _, ok := consoleOf(recovered).Minted("ci", "admin", now.Add(time.Minute)); ok {
		t.Fatal("recovery reopened one-time secret")
	}
	sub.Target.Id = "new-ci"
	sub.IdempotencyKey = "issue-after-restart"
	if _, err := recovered.Submit(admin, sub, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, ok := consoleOf(recovered).Minted("new-ci", "admin", now.Add(time.Minute)); !ok {
		t.Fatal("new token cannot deliver secret after restart")
	}
}
