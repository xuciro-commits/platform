package platformserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// notes is a minimal app: notes on topics.
type notes struct {
	id     string
	ledger *platform.Ledger
	texts  map[string]string
}

func newNotes(tenant, id string) *notes {
	catalog := platform.NewCatalog(platform.Action{Schema: id + ".note", Target: id + ".topic", Capability: "notes", Title: "Note",
		Description: "Write a note on a topic.", Payload: []platform.Field{}, Roles: []string{"writer"}})
	return &notes{id: id, ledger: platform.NewLedger(tenant, id, catalog, id+".topic"), texts: map[string]string{}}
}

func (n *notes) Manifest() platform.Manifest {
	return platform.Manifest{ID: n.id, Version: "1", Actions: n.ledger.Catalog, Reads: []string{n.id + "-notes"}, Inputs: map[string]bool{n.id + "-feed": true}}
}
func (n *notes) Snapshot() (json.RawMessage, error) { return n.ledger.SnapshotWith(n.texts) }
func (n *notes) Restore(raw json.RawMessage) error {
	n.texts = map[string]string{}
	return n.ledger.RestoreWith(raw, &n.texts)
}
func (n *notes) Declarations() []*pb.AuthorityDeclaration          { return n.ledger.Declarations() }
func (n *notes) Read(platform.Caller, string) (any, *kernel.Error) { return n.texts, nil }
func (n *notes) Submit(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return n.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		return func(*pb.ChangeRecord) { n.texts[s.GetTarget().GetId()] = string(s.GetPayload()) }, nil
	})
}
func (n *notes) Input(c platform.Caller, _ string, body []byte, _ time.Time) (any, *kernel.Error) {
	n.texts["feed"] = string(body)
	return nil, nil
}

func note(tenant, member, app, key, topic, text string) *pb.Submission {
	return &pb.Submission{TenantId: tenant, PrincipalId: member, Authority: app, IdempotencyKey: key,
		Target: &pb.EntityRef{Type: app + ".topic", Id: topic}, Schema: &pb.SchemaRef{Name: app + ".note", Version: 1}, Payload: []byte(text)}
}

func setup(t *testing.T, record func(Entry)) (*Tenant, *notes, *notes) {
	dir := NewConsole("t-1",
		Seat{Subjects: []string{"ana"}, Member: platform.Member{ID: "ana", Roles: map[string]string{"a": "writer", "b": "writer", PlatformApp: Admin}}},
		Seat{Subjects: []string{"bo"}, Member: platform.Member{ID: "bo", Roles: map[string]string{"b": "writer"}}})
	a, b := newNotes("t-1", "a"), newNotes("t-1", "b")
	tn, err := NewTenant("t-1", dir, a, b)
	if err != nil {
		t.Fatal(err)
	}
	tn.Record = record
	return tn, a, b
}

func TestTenantComposition(t *testing.T) {
	if _, err := NewTenant("t", newNotes("t", "a"), newNotes("t", "a")); err == nil {
		t.Error("duplicate names were accepted")
	}
	var journal []Entry
	tn, a, b := setup(t, func(e Entry) { journal = append(journal, e) })
	ana, _ := tn.app(PlatformApp).(*Console).Member("ana")
	bo, _ := tn.app(PlatformApp).(*Console).Member("bo")
	now := time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)

	// Each submission reaches the app declaring its action, with the member's role there.
	if _, err := tn.Submit(ana, note("t-1", "ana", "a", "k1", "x", "hello"), now); err != nil {
		t.Fatal(err)
	}
	if _, err := tn.Submit(bo, note("t-1", "bo", "b", "k2", "y", "there"), now); err != nil {
		t.Fatal(err)
	}
	if _, err := tn.Submit(bo, note("t-1", "bo", "a", "k3", "z", "no"), now); err == nil || err.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
		t.Fatalf("bo has no role in a: %v", err)
	}
	if a.texts["x"] != "hello" || b.texts["y"] != "there" || len(a.texts) != 1 {
		t.Fatalf("a %v, b %v", a.texts, b.texts)
	}
	schemas := func(m platform.Member) []string {
		var out []string
		for _, x := range tn.Catalog(m) {
			out = append(out, x.Schema)
		}
		return out
	}
	if got := schemas(bo); !slices.Equal(got, []string{SchemaNotificationRead, "b.note"}) {
		t.Fatalf("bo is offered %v", got)
	}
	if got := schemas(ana); !slices.Equal(got, []string{SchemaAdd, SchemaGrant, SchemaRevoke,
		SchemaConnectorOn, SchemaConnectorOff, SchemaSettingSet, SchemaWorkRetry, SchemaProtocolBind, SchemaNotificationRead,
		SchemaEndpointAdd, SchemaEndpointRemove, SchemaEffectRetry, SchemaEffectDiscard, SchemaEffectApprove, "a.note", "b.note"}) {
		t.Fatalf("ana's catalog %v", got)
	}
	if _, err := tn.Input(ana, "a-feed", []byte("tick"), now); err != nil {
		t.Fatal(err)
	}

	// A second tenant replays the journal into the same state.
	again, a2, b2 := setup(t, nil)
	if err := again.Replay(journal); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(a2.texts, b2.texts) != fmt.Sprint(a.texts, b.texts) || len(journal) != 3 {
		t.Fatalf("replayed %v %v from %d entries", a2.texts, b2.texts, len(journal))
	}
}

func TestHostHTTPAndConsole(t *testing.T) {
	tn, _, _ := setup(t, nil)
	h := NewHost(Tokens(map[string]string{"ana-token": "ana", "bo-token": "bo", "stranger": "nobody"}), tn)
	call := func(method, path, token, body string) (int, string) {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.Handler().ServeHTTP(rec, req)
		return rec.Code, strings.TrimSpace(rec.Body.String())
	}
	submit := func(token, member, schema, target, key, payload string) (int, string) {
		id := "bo"
		if schema == SchemaAdd {
			id = "agent-1"
		}
		raw, _ := json.Marshal(map[string]any{"tenantId": "t-1", "principalId": member, "authority": PlatformApp, "idempotencyKey": key,
			"target": map[string]string{"type": target, "id": id}, "schema": map[string]any{"name": schema, "version": 1},
			"payload": []byte(payload)})
		return call("POST", "/v1/submissions", token, string(raw))
	}
	for _, c := range []struct {
		method, path, token string
		status              int
		contains            string
	}{
		{"GET", "/v1/me", "ana-token", 200, `"principalId":"ana"`},
		{"GET", "/v1/me", "stranger", 401, ``},
		{"GET", "/v1/a-notes", "ana-token", 200, `{}`},
		{"GET", "/v1/nothing", "ana-token", 404, ``},
		{"GET", "/v1/apps", "ana-token", 200, `"id":"b","version":"1","reads":["b-notes"]`},
		{"GET", "/v1/declarations", "ana-token", 200, `"dataClass":"a.topic"`},
		{"OPTIONS", "/v1/submissions", "", http.StatusNoContent, ``},
	} {
		if status, body := call(c.method, c.path, c.token, ""); status != c.status || !strings.Contains(body, c.contains) {
			t.Errorf("%s %s: %d %s", c.method, c.path, status, body)
		}
	}
	// Grant bo a role in a: on the next request bo's catalog offers both notes.
	if status, body := submit("ana-token", "ana", SchemaGrant, MemberType, "g1", `{"app":"a","role":"writer"}`); status != 200 {
		t.Fatalf("grant: %d %s", status, body)
	}
	if _, body := call("GET", "/v1/actions", "bo-token", ""); !strings.Contains(body, `"a.note"`) || !strings.Contains(body, `"b.note"`) {
		t.Fatalf("after the grant bo sees %s", body)
	}
	if status, _ := submit("bo-token", "bo", SchemaGrant, MemberType, "g2", `{"app":"platform","role":"admin"}`); status != 403 {
		t.Fatalf("a member without the admin role granted itself: %d", status)
	}
	submit("ana-token", "ana", SchemaRevoke, MemberType, "r1", `{"app":"b"}`)
	for _, c := range []struct {
		key, schema, payload string
		status               int
	}{
		{"x1", SchemaGrant, `{"app":"a","role":"owner"}`, 400},     // a role app a does not define
		{"x2", SchemaGrant, `{"app":"nope","role":"writer"}`, 400}, // an app the tenant does not run
		{"x4", SchemaAdd, `{"subject":"client:agent"}`, 200},
		{"x5", SchemaAdd, `{"subject":"client:agent"}`, 409},
		{"x6", SchemaAdd, `{"subject":"agent"}`, 400},
	} {
		if status, body := submit("ana-token", "ana", c.schema, MemberType, c.key, c.payload); status != c.status {
			t.Errorf("%s %s: %d %s", c.schema, c.payload, status, body)
		}
	}
	if status, body := call("GET", "/v1/members", "ana-token", ""); status != 200 ||
		!strings.Contains(body, `"id":"agent-1","tenant":"t-1","roles":{},"subjects":["client:agent"]`) {
		t.Fatalf("members: %d %s", status, body)
	}
	if status, _ := call("GET", "/v1/members", "bo-token", ""); status != 403 {
		t.Fatalf("a member without the admin role read the directory: %d", status)
	}
	if _, body := call("GET", "/v1/audit", "ana-token", ""); strings.Count(body, `"app":"platform"`) != 3 || !strings.Contains(body, `"target":"platform.member/agent-1"`) {
		t.Fatalf("audit: %s", body)
	}
	if _, body := call("GET", "/v1/apps", "ana-token", ""); !strings.Contains(body, `"roles":["writer"],"capabilities":[{"name":"notes","enabled":true,"actions":["b.note"]}],"inputs":["b-feed"],"uses":[]`) {
		t.Fatalf("apps: %s", body)
	}
	if _, body := call("GET", "/v1/actions", "bo-token", ""); strings.Contains(body, `"b.note"`) || !strings.Contains(body, `"a.note"`) {
		t.Fatalf("after revoking b, bo sees %s", body)
	}
}

// ADR-0018: one sign-in reaches every tenant the person is a member of on this
// host; /v1/me names the apps they may open; the host serves the workspace.
func TestWorkspaceSurface(t *testing.T) {
	one, _, _ := setup(t, nil)
	other, err := NewTenant("t-2", NewConsole("t-2", Seat{Subjects: []string{"ana"}, Member: platform.Member{ID: "ana-2", Roles: map[string]string{"b": "writer"}}}),
		newNotes("t-2", "b"))
	if err != nil {
		t.Fatal(err)
	}
	web := t.TempDir()
	os.WriteFile(filepath.Join(web, "index.html"), []byte("<title>Workspace</title>"), 0o644)
	os.WriteFile(filepath.Join(web, "app.js"), []byte("js"), 0o644)
	h := NewHost(Tokens(map[string]string{"ana-token": "ana", "bo-token": "bo"}), one, other)
	h.Web, h.Development = web, true
	call := func(path, token, tenant string) string {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		if tenant != "" {
			req.Header.Set(TenantHeader, tenant)
		}
		rec := httptest.NewRecorder()
		h.Handler().ServeHTTP(rec, req)
		return fmt.Sprint(rec.Code, " ", strings.TrimSpace(rec.Body.String()))
	}
	for _, c := range []struct{ path, token, tenant, contains string }{
		{"/v1/me", "ana-token", "", `"apps":[{"id":"platform","title":"Settings","role":"admin"},{"id":"a","title":"a","role":"writer"},{"id":"b","title":"b","role":"writer"}]`},
		{"/v1/me", "ana-token", "", `"tenants":["t-1","t-2"]`},
		{"/v1/me", "ana-token", "t-2", `"principalId":"ana-2"`},
		{"/v1/me", "bo-token", "t-2", `401`},
		{"/v1/me", "bo-token", "", `"apps":[{"id":"b","title":"b","role":"writer"}]`},
		{"/v1/sign-in", "", "", `{"identities":[{"token":"ana","tenant":"t-1","member":"ana"`},
		{"/", "", "", `200 <title>Workspace</title>`},
		{"/app.js", "", "", `200 js`},
		{"/crm/customers", "", "", `200 <title>Workspace</title>`},
	} {
		if got := call(c.path, c.token, c.tenant); !strings.Contains(got, c.contains) {
			t.Errorf("%s as %s in %q: %s", c.path, c.token, c.tenant, got)
		}
	}
	h.Issuer, h.Client = "https://id.example/", "platform-web"
	if got := call("/v1/sign-in", "", ""); got != `200 {"client":"platform-web","issuer":"https://id.example/"}` {
		t.Errorf("a production host signs in with %s", got)
	}
}
