package platformserver

import agentowner "platformserver/engine/agents"

import "platformserver/studio/build"

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

	"platformserver/platform"
)

type dummyAppWithAgent struct {
	ledger *platform.Ledger
	agent  platform.Agent
}

func (d *dummyAppWithAgent) AcceptedLedger() *platform.Ledger { return d.ledger }

func (d *dummyAppWithAgent) Manifest() platform.Manifest {
	return platform.Manifest{
		ID:      "testapp",
		Version: "1",
		Actions: platform.NewCatalog(platform.Action{
			Schema:      "testapp.do",
			Target:      "testapp.item",
			Title:       "Do something",
			Description: "Does something",
			Payload:     []platform.Field{{Name: "note", Type: "string"}},
			Roles:       []string{"user"},
		}),
		Agents: []platform.Agent{d.agent},
	}
}
func (d *dummyAppWithAgent) Snapshot() (json.RawMessage, error)       { return nil, nil }
func (d *dummyAppWithAgent) Restore(json.RawMessage) error            { return nil }
func (d *dummyAppWithAgent) Declarations() []*pb.AuthorityDeclaration { return nil }
func (d *dummyAppWithAgent) Read(platform.Caller, string) (any, *kernel.Error) {
	return nil, nil
}
func (d *dummyAppWithAgent) Input(platform.Caller, string, []byte, time.Time) (any, *kernel.Error) {
	return nil, nil
}
func (d *dummyAppWithAgent) Submit(platform.Caller, *pb.Submission, time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return nil, nil
}

func TestAgentUnknownQueryFailsComposition(t *testing.T) {
	ag := platform.Agent{
		Name:         "bad-agent",
		Title:        "Bad",
		Instructions: "None",
		Tools:        []string{"query:nonexistent"},
	}
	app := &dummyAppWithAgent{agent: ag}
	app.ledger = platform.NewLedger("t-agent-test", "testapp", app.Manifest().Actions, "testapp.item")
	_, err := NewTenant("t-agent-test", agentowner.New("t-agent-test"), app)
	if err == nil || !strings.Contains(err.Error(), "not a query of") {
		t.Fatalf("expected error containing 'not a query of', got: %v", err)
	}
}

// Page and query checks use published owner declarations, not a second app.
func queryBindingTenant(t *testing.T, id string) *Tenant {
	t.Helper()
	tn := composeTenant(t, id, []platform.Seat{seatOf("builder", "build:builder"), seatOf("user", "build:user"), seatOf("other", "platform:auditor")}, build.New(id))
	publishObject(t, tn, "builder", "PARENT", map[string]any{"name": "parent", "title": "Parent", "fields": []build.Field{{Name: "name", Title: "Name", Type: "text", Required: true}}}, txnAt)
	publishObject(t, tn, "builder", "CHILD", map[string]any{"name": "child", "title": "Child", "fields": []build.Field{
		{Name: "parent", Title: "Parent", Type: "reference", Ref: "build.parent", Inverse: "children", Required: true},
		{Name: "title", Title: "Title", Type: "text", Required: true}, {Name: "enabled", Title: "Enabled", Type: "boolean"},
	}}, txnAt)
	return tn
}

func publishBindingQuery(t *testing.T, tn *Tenant) {
	t.Helper()
	decide(t, tn, "builder", "build", "build.query.create", build.QueryType, "QUERY", platform.NamedQuery{Name: "enabledchildren", Title: "Enabled children", Description: "Filter related records", Object: "build.child", By: "parent", Domain: json.RawMessage(`[["enabled","=",true]]`), Limit: 20}, txnAt)
	decide(t, tn, "builder", "build", build.SchemaQuery, build.QueryType, "QUERY", map[string]any{}, txnAt)
}

func TestNamedQueryHTTP(t *testing.T) {
	tn := queryBindingTenant(t, "named-query-http")
	publishBindingQuery(t, tn)
	for _, id := range []string{"A", "B"} {
		decide(t, tn, "builder", "build", "build.parent.create", "build.parent", id, map[string]any{"name": id}, txnAt)
	}
	for _, row := range []struct {
		id, parent string
		enabled    bool
	}{{"included", "A", true}, {"filtered", "A", false}, {"different-parent", "B", true}} {
		decide(t, tn, "builder", "build", "build.child.create", "build.child", row.id, map[string]any{"parent": row.parent, "title": row.id, "enabled": row.enabled}, txnAt)
	}
	handler := NewHost(Tokens(map[string]string{"reader-token": "user", "other-token": "other"}), tn).Handler()
	call := func(token, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	response := call("reader-token", "/v1/queries/build/enabledchildren?for=A")
	var page RecordPage
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &page) != nil || page.Total != 1 || len(page.Records) != 1 {
		t.Fatalf("scoped named query: %d %s", response.Code, response.Body.String())
	}
	if toMap(page.Records[0])["id"] != "included" {
		t.Fatalf("parent or filter escaped: %+v", page.Records)
	}
	for _, test := range []struct {
		path   string
		status int
	}{
		{"/v1/queries/build/enabledchildren", http.StatusBadRequest},
		{"/v1/queries/build/unknown?for=A", http.StatusNotFound},
	} {
		if answer := call("reader-token", test.path); answer.Code != test.status {
			t.Fatalf("%s: %d %s", test.path, answer.Code, answer.Body.String())
		}
	}
	denied := call("other-token", "/v1/queries/build/enabledchildren?for=A")
	if denied.Code != http.StatusForbidden && denied.Code != http.StatusNotFound {
		t.Fatalf("member without owner access: %d %s", denied.Code, denied.Body.String())
	}
	body := denied.Body.String()
	if strings.Contains(body, `"records"`) && !strings.Contains(body, `"records":[]`) && !strings.Contains(body, `"records":null`) {
		t.Fatalf("refusal exposed records: %s", body)
	}
}

func TestReleaseCandidateIncludesQueryAsset(t *testing.T) {
	tn := queryBindingTenant(t, "query-candidate")
	publishBindingQuery(t, tn)
	decide(t, tn, "builder", "build", "build.page.create", build.PageType, "PAGE", map[string]any{
		"name": "relatedrows", "title": "Related rows", "object": "build.parent",
		"sections": []map[string]any{{"widget": "table", "object": "build.child", "query": "build.enabledchildren", "fields": []string{"title"}}},
	}, txnAt)
	decide(t, tn, "builder", "build", build.SchemaRelease, build.PageType, "PAGE", map[string]any{}, txnAt)
	candidate, err := tn.ReleaseCandidate([]platform.AssetRef{{App: "build", Kind: platform.AssetPage, Name: "relatedrows"}})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, asset := range candidate.Assets {
		if asset.Ref == (platform.AssetRef{App: "build", Kind: platform.AssetQuery, Name: "enabledchildren"}) {
			found = true
		}
	}
	if !found {
		t.Fatal("page closure omitted its declared query")
	}
}
