package platformserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
)

// notes is a minimal app: notes on topics. With a requirement on another notes
// app it becomes a bridge that copies each note there first.
type notes struct {
	id, peer string
	ledger   *Ledger
	texts    map[string]string
}

func newNotes(tenant, id, peer string) *notes {
	uses := []string{}
	if peer != "" {
		uses = []string{peer + ".note"}
	}
	catalog := NewCatalog(Action{Schema: id + ".note", Target: id + ".topic", Capability: "notes", Title: "Note", Roles: []string{"writer"}, Uses: uses})
	return &notes{id: id, peer: peer, ledger: NewLedger(tenant, id, catalog, id+".topic"), texts: map[string]string{}}
}

func (n *notes) Manifest() Manifest {
	m := Manifest{ID: n.id, Version: "1", Actions: n.ledger.Catalog, Reads: []string{n.id + "-notes"}, Inputs: map[string]bool{n.id + "-feed": true}}
	if n.peer != "" {
		m.Requires = []string{n.peer}
	}
	return m
}
func (n *notes) Declarations() []*pb.AuthorityDeclaration { return n.ledger.Declarations() }
func (n *notes) Read(Caller, string) (any, *kernel.Error) { return n.texts, nil }
func (n *notes) Submit(c Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return n.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		if n.peer != "" {
			copied := &pb.Submission{TenantId: s.GetTenantId(), PrincipalId: c.ID, Authority: n.peer, IdempotencyKey: "copy:" + s.GetIdempotencyKey(),
				Target: &pb.EntityRef{Type: n.peer + ".topic", Id: s.GetTarget().GetId()}, Schema: &pb.SchemaRef{Name: n.peer + ".note", Version: 1}, Payload: s.GetPayload()}
			if _, err := c.Submit(n.peer, copied, now); err != nil {
				return nil, err
			}
		}
		return func(*pb.ChangeRecord) { n.texts[s.GetTarget().GetId()] = string(s.GetPayload()) }, nil
	})
}
func (n *notes) Input(c Caller, _ string, body []byte, _ time.Time) (any, *kernel.Error) {
	n.texts["feed"] = string(body)
	return nil, nil
}

func note(tenant, member, app, key, topic, text string) *pb.Submission {
	return &pb.Submission{TenantId: tenant, PrincipalId: member, Authority: app, IdempotencyKey: key,
		Target: &pb.EntityRef{Type: app + ".topic", Id: topic}, Schema: &pb.SchemaRef{Name: app + ".note", Version: 1}, Payload: []byte(text)}
}

func setup(t *testing.T, record func(Entry)) (*Tenant, *notes, *notes) {
	dir := NewDirectory("t-1",
		Seat{Subjects: []string{"ana"}, Member: Member{ID: "ana", Roles: map[string]string{"a": "writer", "b": "writer", PlatformApp: Admin}}},
		Seat{Subjects: []string{"bo"}, Member: Member{ID: "bo", Roles: map[string]string{"b": "writer"}}})
	a, b := newNotes("t-1", "a", ""), newNotes("t-1", "b", "a")
	tn, err := NewTenant("t-1", dir, a, b)
	if err != nil {
		t.Fatal(err)
	}
	tn.Record = record
	return tn, a, b
}

func TestTenantComposition(t *testing.T) {
	if _, err := NewTenant("t", newNotes("t", "b", "a")); err == nil {
		t.Error("an unmet requirement was accepted")
	}
	if _, err := NewTenant("t", newNotes("t", "a", ""), newNotes("t", "a", "")); err == nil {
		t.Error("duplicate names were accepted")
	}
	var journal []Entry
	tn, a, b := setup(t, func(e Entry) { journal = append(journal, e) })
	ana, _ := tn.app(PlatformApp).(*Directory).Member("ana")
	bo, _ := tn.app(PlatformApp).(*Directory).Member("bo")
	now := time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)

	// The bridge's call reaches app a with the member's own role there.
	if _, err := tn.Submit(ana, note("t-1", "ana", "b", "k1", "x", "hello"), now); err != nil {
		t.Fatal(err)
	}
	if a.texts["x"] != "hello" || b.texts["x"] != "hello" {
		t.Fatalf("a %v, b %v", a.texts, b.texts)
	}
	if _, err := tn.Submit(bo, note("t-1", "bo", "b", "k2", "y", "no"), now); err == nil || err.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
		t.Fatalf("bo has no role in a, so the bridge must be refused: %v", err)
	}
	schemas := func(m Member) []string {
		var out []string
		for _, x := range tn.Catalog(m) {
			out = append(out, x.Schema)
		}
		return out
	}
	if got := schemas(bo); !slices.Equal(got, []string{SchemaNoticeRead}) {
		t.Fatalf("bo is offered %v; b.note uses a.note, which bo may not call", got)
	}
	if got := schemas(ana); !slices.Equal(got, []string{SchemaAdd, SchemaGrant, SchemaRevoke, SchemaScope,
		SchemaConnectorOn, SchemaConnectorOff, SchemaSettingSet, SchemaWorkRetry, SchemaNoticeRead, "a.note", "b.note"}) {
		t.Fatalf("ana's catalog %v", got)
	}
	// An app may call only what it requires.
	if _, err := As("a", ana).Submit("b", note("t-1", "ana", "b", "k3", "z", "x"), now); err == nil {
		t.Fatal("a call without a host or requirement went through")
	}
	if _, err := tn.Input(ana, "a-feed", []byte("tick"), now); err != nil {
		t.Fatal(err)
	}

	// A second tenant replays the journal into the same state.
	again, a2, b2 := setup(t, nil)
	if err := again.Replay(journal); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(a2.texts, b2.texts) != fmt.Sprint(a.texts, b.texts) || len(journal) != 2 {
		t.Fatalf("replayed %v %v from %d entries", a2.texts, b2.texts, len(journal))
	}
}

func TestHostHTTPAndDirectory(t *testing.T) {
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
		{"GET", "/v1/apps", "ana-token", 200, `"id":"b","version":"1","requires":["a"]`},
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
	if _, body := call("GET", "/v1/actions", "bo-token", ""); !strings.Contains(body, `"b.note"`) {
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
		{"x3", SchemaScope, `{"attribute":"lines","values":["L1"]}`, 200},
		{"x4", SchemaAdd, `{"subject":"client:agent"}`, 200},
		{"x5", SchemaAdd, `{"subject":"client:agent"}`, 409},
		{"x6", SchemaAdd, `{"subject":"agent"}`, 400},
	} {
		if status, body := submit("ana-token", "ana", c.schema, MemberType, c.key, c.payload); status != c.status {
			t.Errorf("%s %s: %d %s", c.schema, c.payload, status, body)
		}
	}
	if status, body := call("GET", "/v1/members", "ana-token", ""); status != 200 ||
		!strings.Contains(body, `"id":"agent-1","tenant":"t-1","roles":{},"subjects":["client:agent"]`) || !strings.Contains(body, `"attributes":{"lines":["L1"]}`) {
		t.Fatalf("members: %d %s", status, body)
	}
	if status, _ := call("GET", "/v1/members", "bo-token", ""); status != 403 {
		t.Fatalf("a member without the admin role read the directory: %d", status)
	}
	if _, body := call("GET", "/v1/audit", "ana-token", ""); strings.Count(body, `"app":"platform"`) != 4 || !strings.Contains(body, `"target":"platform.member/agent-1"`) {
		t.Fatalf("audit: %s", body)
	}
	if _, body := call("GET", "/v1/apps", "ana-token", ""); !strings.Contains(body, `"roles":["writer"],"capabilities":[{"name":"notes","enabled":true,"actions":["b.note"]}],"inputs":["b-feed"],"uses":["b.note → a.note"]`) {
		t.Fatalf("apps: %s", body)
	}
	if _, body := call("GET", "/v1/actions", "bo-token", ""); strings.Contains(body, `"b.note"`) || !strings.Contains(body, `"a.note"`) {
		t.Fatalf("after revoking b, bo sees %s", body)
	}
}
