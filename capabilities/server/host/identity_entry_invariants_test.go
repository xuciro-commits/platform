package host

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"platformserver/kernel/protocol"
	"platformserver/platform"
	"platformserver/services/security/control"
	"strings"
	"testing"
	"time"
)

func TestInvariantIdentityEntryAndInvitation(t *testing.T) {
	const subject = "user:colleague@example.test"
	adminSeat := protocol.Seat{Subjects: []string{"user:owner@example.test"}, Member: platform.Member{ID: "owner", Roles: map[string]string{"platform": "admin"}}}
	live := newInvariantTenant(t, "identity-test", adminSeat)
	now := invariantClock
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	received := make(chan string, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
		r := textproto.NewReader(bufio.NewReader(conn))
		_, _ = conn.Write([]byte("220 capture SMTP\r\n"))
		for {
			line, err := r.ReadLine()
			if err != nil {
				return
			}
			switch {
			case strings.HasPrefix(line, "EHLO") || strings.HasPrefix(line, "HELO"):
				_, _ = conn.Write([]byte("250 capture\r\n"))
			case strings.HasPrefix(line, "MAIL FROM:") || strings.HasPrefix(line, "RCPT TO:"):
				_, _ = conn.Write([]byte("250 accepted\r\n"))
			case line == "DATA":
				_, _ = conn.Write([]byte("354 send data\r\n"))
				raw, err := r.ReadDotBytes()
				if err != nil {
					return
				}
				received <- string(raw)
				_, _ = conn.Write([]byte("250 queued\r\n"))
			case line == "QUIT":
				_, _ = conn.Write([]byte("221 goodbye\r\n"))
				return
			default:
				_, _ = conn.Write([]byte("250 OK\r\n"))
			}
		}
	}()
	t.Setenv("PLATFORM_IDENTITY_SMTP_URL", "smtp://"+listener.Addr().String())
	t.Setenv("PLATFORM_IDENTITY_SMTP_FROM", "no-reply@example.test")
	t.Setenv("PLATFORM_IDENTITY_SMTP_ALLOW_PRIVATE", "1")
	t.Setenv("PLATFORM_PUBLIC_URL", "https://platform.example.test")
	h := NewHost(Tokens(map[string]string{"owner": "user:owner@example.test", "colleague": subject, "stranger": "user:stranger@example.test", "expired": "user:expired@example.test"}), live.Tenant)
	h.Now = func() time.Time { return now }
	h.Issuer = "https://idp.example.test"
	h.SelfService = true
	h.CreateTenant = func(req protocol.CreateTenantRequest, by string) (*Tenant, error) {
		if req.Admin != by || by != "user:stranger@example.test" {
			t.Fatal("caller chose another administrator")
		}
		created := newInvariantTenant(t, req.ID, protocol.Seat{Subjects: []string{by}, Member: platform.Member{ID: "new-owner", Roles: map[string]string{"platform": "admin"}}})
		h.tenants = append(h.tenants, created.Tenant)
		return created.Tenant, nil
	}
	handler := h.Handler()
	call := func(method, path, token, tenant string, body any, want int) *httptest.ResponseRecorder {
		t.Helper()
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(method, path, bytes.NewReader(b))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if tenant != "" {
			req.Header.Set(TenantHeader, tenant)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Fatalf("%s %s got %d want %d: %s", method, path, rec.Code, want, rec.Body.String())
		}
		return rec
	}
	identity := func(token string) protocol.IdentityEntry {
		t.Helper()
		var x protocol.IdentityEntry
		rec := call("GET", "/v1/auth/me", token, "", nil, 200)
		if err := json.Unmarshal(rec.Body.Bytes(), &x); err != nil {
			t.Fatal(err)
		}
		return x
	}
	admin, _ := live.Member("owner")
	submit := func(schema, id string, payload any) {
		t.Helper()
		sub := live.submission(schema, control.MemberType, id, payload)
		sub.Authority, sub.PrincipalId = "platform", "owner"
		if _, refusal := live.Submit(admin, sub, now); refusal != nil {
			t.Fatalf("%s: %v", schema, refusal)
		}
	}
	call("GET", "/v1/auth/me", "", "", nil, http.StatusUnauthorized)
	if entry := identity("stranger"); len(entry.Workspaces) != 0 || !entry.CanCreate {
		t.Fatalf("zero workspace identity: %+v", entry)
	}
	call("GET", "/v1/me", "stranger", "", nil, 401)
	call("POST", "/v1/auth/workspaces", "stranger", "", map[string]string{"id": "created-test", "name": "New team", "admin": "user:owner@example.test"}, 400)
	call("POST", "/v1/auth/workspaces", "stranger", "", map[string]string{"id": "created-test", "name": "New team"}, 201)
	call("GET", "/v1/me", "stranger", "created-test", nil, 200)
	call("POST", "/v1/auth/workspaces", "stranger", "", map[string]string{"id": "created-test", "name": "New team"}, 409)
	submit(control.SchemaInvite, "colleague", map[string]any{"subject": subject})
	submit(control.SchemaGrant, "colleague", map[string]any{"app": "platform", "role": "auditor"})
	if entry := identity("colleague"); len(entry.Invitations) != 0 {
		t.Fatal("draft was visible")
	}
	submit(control.SchemaInviteSend, "colleague", map[string]any{})
	live.Dispatch(now)
	select {
	case letter := <-received:
		if !strings.Contains(letter, "To: colleague@example.test") || !strings.Contains(letter, "https://platform.example.test/") {
			t.Fatalf("invitation mail did not address the verified login: %s", letter)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no SMTP invitation was received")
	}
	if entry := identity("colleague"); len(entry.Invitations) != 1 || len(entry.Workspaces) != 0 {
		t.Fatalf("pending: %+v", entry)
	}
	call("GET", "/v1/me", "colleague", live.ID, nil, 401)
	call("POST", "/v1/auth/invitations/"+live.ID+"/accept", "stranger", "", nil, 403)
	call("POST", "/v1/auth/invitations/"+live.ID+"/accept", "colleague", "", nil, 200)
	call("GET", "/v1/me", "colleague", live.ID, nil, 200)
	if member, ok := live.Member("colleague"); !ok || !member.Active() || !member.Holds("platform", "auditor") {
		t.Fatalf("accepted member: %+v", member)
	}
	call("POST", "/v1/auth/invitations/"+live.ID+"/accept", "colleague", "", nil, 403)
	submit(control.SchemaInvite, "expired", map[string]any{"subject": "user:expired@example.test"})
	submit(control.SchemaInviteSend, "expired", map[string]any{})
	if len(identity("expired").Invitations) != 1 {
		t.Fatal("pending invitation missing")
	}
	now = now.Add(8 * 24 * time.Hour)
	call("POST", "/v1/auth/invitations/"+live.ID+"/accept", "expired", "", nil, 403)
	expired, _ := live.IdentityOwner.MemberBySubject("user:expired@example.test")
	if expired.Active() {
		t.Fatal("expired invitation active")
	}
	submit(control.SchemaInviteRevoke, "expired", map[string]any{})
	call("GET", "/v1/me", "expired", live.ID, nil, 401)
	sub := live.submission(control.SchemaRevoke, control.MemberType, "owner", map[string]any{"app": "platform", "role": "admin"})
	sub.Authority, sub.PrincipalId = "platform", "owner"
	if _, refusal := live.Submit(admin, sub, now); refusal == nil {
		t.Fatal("last administrator revoked")
	}
	restored := live.restore(t, live.entries, adminSeat)
	if member, ok := restored.Member("colleague"); !ok || !member.Active() || member.Invitation == nil || member.Invitation.State != "accepted" {
		t.Fatalf("replayed invitation: %+v", member)
	}
}
