package platformserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	controlowner "platformserver/kernel/control"
	"platformserver/platform"
	"strings"
	"testing"
	"time"
)

func TestDeploymentHostAdministratorsAreExplicit(t *testing.T) {
	tenant, err := NewTenant("host-admin-boundary", newConsole("host-admin-boundary", platform.Seat{Subjects: []string{"tenant-admin"}, Member: platform.Member{ID: "tenant-admin", Roles: map[string]string{controlowner.PlatformApp: controlowner.Admin}}}))
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
	t.Cleanup(func() { controlowner.UseTokenKey([]byte("development-only")) })
	sampleToken := func() string {
		at := time.Now()
		directory := newConsole("tenant", platform.Seat{Member: platform.Member{ID: "member", Roles: map[string]string{}}})
		raw, err := directory.AcceptedState()
		if err != nil {
			t.Fatal(err)
		}
		var state map[string]json.RawMessage
		if err = json.Unmarshal(raw, &state); err != nil {
			t.Fatal(err)
		}
		state["tokens"] = platform.Raw(map[string]controlowner.Token{"token": {ID: "token", Member: "member", Change: "change", Issued: at}})
		if err = directory.ApplyAcceptedState(platform.Raw(state)); err != nil {
			t.Fatal(err)
		}
		secret, ok := directory.Minted("token", "member", at)
		if !ok {
			t.Fatal("configured token could not be minted")
		}
		return secret
	}
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
	first := sampleToken()
	if err := d.configurePersonalTokens(); err != nil {
		t.Fatal(err)
	}
	if sampleToken() != first {
		t.Fatal("same key does not retain token")
	}
	t.Setenv("PLATFORM_PERSONAL_TOKEN_KEY", strings.Repeat("b", 32))
	if err := d.configurePersonalTokens(); err != nil {
		t.Fatal(err)
	}
	if sampleToken() == first {
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
		tn, err := NewTenant("token-recovery", newConsole("token-recovery", platform.Seat{Subjects: []string{"admin"}, Member: platform.Member{ID: "admin", Roles: map[string]string{controlowner.PlatformApp: controlowner.Admin}}}))
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
	sub := &pb.Submission{TenantId: live.ID, PrincipalId: admin.ID, Authority: controlowner.PlatformApp, IdempotencyKey: "issue", Target: &pb.EntityRef{Type: controlowner.TokenType, Id: "ci"}, Schema: &pb.SchemaRef{Name: controlowner.SchemaTokenIssue, Version: 1}, Payload: []byte(`{"label":"CI"}`)}
	if _, err := live.Submit(admin, sub, now); err != nil {
		t.Fatal(err)
	}
	secret, ok := testConsoleOf(live).Minted("ci", "admin", now)
	if !ok {
		t.Fatal("issuing process did not deliver secret")
	}
	recovered := compose()
	d := Deployment{}
	if err := d.restoreTenants(ctx, journal, "", []*Tenant{recovered}, map[string]int64{}, map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	if _, ok := testConsoleOf(recovered).MemberByToken(secret, now.Add(time.Minute)); !ok {
		t.Fatal("token failed after recovery")
	}
	if _, ok := testConsoleOf(recovered).Minted("ci", "admin", now.Add(time.Minute)); ok {
		t.Fatal("recovery reopened one-time secret")
	}
	sub.Target.Id = "new-ci"
	sub.IdempotencyKey = "issue-after-restart"
	if _, err := recovered.Submit(admin, sub, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, ok := testConsoleOf(recovered).Minted("new-ci", "admin", now.Add(time.Minute)); !ok {
		t.Fatal("new token cannot deliver secret after restart")
	}
}
