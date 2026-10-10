package platformserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/build"
	"platformserver/platform"
)

type dummyAppWithAgent struct {
	agent platform.Agent
}

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
	_, err := NewTenant("t-agent-test", NewAgents("t-agent-test"), app)
	if err == nil || !strings.Contains(err.Error(), "not a query of") {
		t.Fatalf("expected error containing 'not a query of', got: %v", err)
	}
}

// CRM test structures for HTTP test
type CRMTestAccount struct {
	platform.Record
	Name string `json:"name" field:"required,search"`
}

type CRMTestOpportunity struct {
	platform.Record
	Account platform.Ref[CRMTestAccount] `json:"account" field:"required" inverse:"opportunities"`
	Title   string                       `json:"title" field:"required,search"`
	Stage   string                       `json:"stage" choices:"open,won,lost"`
}

type testCRMApp struct {
	ledger *platform.Ledger
}

func newTestCRMApp(tenant string) *testCRMApp {
	actions := []platform.Action{
		{Schema: "crm.account.create", Target: "crm.account", Title: "Create", Description: "Create account", Payload: []platform.Field{{Name: "name", Type: "string"}}, Roles: []string{"sales", "sales-manager"}},
		{Schema: "crm.opportunity.open", Target: "crm.opportunity", Title: "Open", Description: "Open opportunity", Payload: []platform.Field{{Name: "title", Type: "string"}, {Name: "account", Type: "string"}}, Roles: []string{"sales", "sales-manager"}},
	}
	return &testCRMApp{
		ledger: platform.NewLedger(tenant, "crm", platform.NewCatalog(actions...), "crm.account", "crm.opportunity"),
	}
}

func (c *testCRMApp) Manifest() platform.Manifest {
	return platform.Manifest{
		ID:      "crm",
		Version: "1",
		Actions: c.ledger.Catalog,
		Entities: []platform.Entity{
			{Type: "crm.account", Title: "Account", Model: CRMTestAccount{}, Standard: platform.Standard{Create: true, Roles: []string{"sales", "sales-manager"}}},
			{Type: "crm.opportunity", Title: "Opportunity", Model: CRMTestOpportunity{}, Standard: platform.Standard{Create: true, Roles: []string{"sales", "sales-manager"}}},
		},
		Queries: []platform.NamedQuery{
			{Name: "open-opportunities", Title: "Open opportunities", Object: "crm.opportunity", By: "account", Domain: json.RawMessage(`[["stage","=","open"]]`)},
		},
		Languages: platform.Languages{"zh-CN": {"Account": "客户", "Accounts": "客户"}},
	}
}
func (c *testCRMApp) Snapshot() (json.RawMessage, error)       { return c.ledger.Snapshot() }
func (c *testCRMApp) Restore(raw json.RawMessage) error        { return c.ledger.Restore(raw) }
func (c *testCRMApp) Declarations() []*pb.AuthorityDeclaration { return c.ledger.Declarations() }
func (c *testCRMApp) Read(platform.Caller, string) (any, *kernel.Error) {
	return nil, nil
}
func (c *testCRMApp) Input(platform.Caller, string, []byte, time.Time) (any, *kernel.Error) {
	return nil, nil
}
func (c *testCRMApp) Submit(caller platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return c.ledger.Receive(caller, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		id := s.GetTarget().GetId()
		switch s.GetSchema().GetName() {
		case "crm.account.create":
			var p struct{ Name string }
			json.Unmarshal(s.GetPayload(), &p)
			acc := CRMTestAccount{Name: p.Name}
			acc.ID = id
			return func(r *pb.ChangeRecord) { caller.Put(r, acc) }, nil
		case "crm.opportunity.open":
			var p struct{ Title, Account string }
			json.Unmarshal(s.GetPayload(), &p)
			opp := CRMTestOpportunity{Title: p.Title, Account: platform.Ref[CRMTestAccount](p.Account), Stage: "open"}
			opp.ID = id
			return func(r *pb.ChangeRecord) { caller.Put(r, opp) }, nil
		}
		return func(*pb.ChangeRecord) {}, nil
	})
}

func TestNamedQueryHTTP(t *testing.T) {
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	crmApp := newTestCRMApp("t-crm-http")
	seatSales := Seat{Subjects: []string{"alice"}, Member: platform.Member{ID: "alice", Roles: map[string]string{"crm": "sales"}}}
	seatOther := Seat{Subjects: []string{"bob"}, Member: platform.Member{ID: "bob", Roles: map[string]string{"other": "user"}}}
	console := NewConsole("t-crm-http", seatSales, seatOther)
	tn, err := NewTenant("t-crm-http", console, crmApp)
	if err != nil {
		t.Fatal(err)
	}

	sales, _ := tn.Member("alice")
	// Seed ACME account and an opportunity for ACME
	accRaw, _ := json.Marshal(map[string]any{"name": "ACME Corp"})
	if _, kerr := tn.Submit(sales, &pb.Submission{
		TenantId: "t-crm-http", PrincipalId: "alice", Authority: "crm",
		IdempotencyKey: "seed-acc-1", Target: &pb.EntityRef{Type: "crm.account", Id: "ACME"},
		Schema: &pb.SchemaRef{Name: "crm.account.create", Version: 1}, Payload: accRaw,
	}, now); kerr != nil {
		t.Fatalf("seed account: %v", kerr)
	}

	oppRaw, _ := json.Marshal(map[string]any{"title": "Big deal", "account": "ACME"})
	if _, kerr := tn.Submit(sales, &pb.Submission{
		TenantId: "t-crm-http", PrincipalId: "alice", Authority: "crm",
		IdempotencyKey: "seed-opp-1", Target: &pb.EntityRef{Type: "crm.opportunity", Id: "OPP-1"},
		Schema: &pb.SchemaRef{Name: "crm.opportunity.open", Version: 1}, Payload: oppRaw,
	}, now); kerr != nil {
		t.Fatalf("seed opp: %v", kerr)
	}

	h := NewHost(Tokens(map[string]string{"alice-token": "alice", "bob-token": "bob"}), tn)
	call := func(token, path string) (int, string) {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		h.Handler().ServeHTTP(rec, req)
		return rec.Code, strings.TrimSpace(rec.Body.String())
	}

	// 1. GET /v1/queries/crm/open-opportunities?for=ACME (200 with records)
	status, body := call("alice-token", "/v1/queries/crm/open-opportunities?for=ACME")
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", status, body)
	}
	var page RecordPage
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatalf("decode page: %v", err)
	}
	if page.Total != 1 || len(page.Records) != 1 {
		t.Fatalf("expected 1 record, got total %d, records %d: %s", page.Total, len(page.Records), body)
	}

	// 2. missing ?for (400)
	status, body = call("alice-token", "/v1/queries/crm/open-opportunities")
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing ?for, got %d: %s", status, body)
	}

	// 3. unknown name (404)
	status, body = call("alice-token", "/v1/queries/crm/unknown?for=ACME")
	if status != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown query, got %d: %s", status, body)
	}

	// 4. non-CRM member (403/404, no records in body)
	status, body = call("bob-token", "/v1/queries/crm/open-opportunities?for=ACME")
	if status != http.StatusForbidden && status != http.StatusNotFound {
		t.Fatalf("expected 403 or 404 for non-CRM member, got %d: %s", status, body)
	}
	if strings.Contains(body, `"records"`) && !strings.Contains(body, `"records":[]`) && !strings.Contains(body, `"records":null`) {
		t.Fatalf("expected no records in body, got: %s", body)
	}
}

func TestReleaseCandidateIncludesQueryAsset(t *testing.T) {
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	crmApp := newTestCRMApp("t-rc-test")
	seatDana := Seat{Subjects: []string{"dana"}, Member: platform.Member{ID: "dana", Roles: map[string]string{build.ID: build.Builder, "crm": "sales-manager"}}}
	b := build.New("t-rc-test")
	tn, err := NewTenant("t-rc-test", NewConsole("t-rc-test", seatDana), b, crmApp)
	if err != nil {
		t.Fatal(err)
	}

	dana, _ := tn.Member("dana")
	// Create builder page with a section using the query
	pagePayload, _ := json.Marshal(map[string]any{
		"name": "itemspage", "title": "Items", "object": "crm.account",
		"sections": []map[string]any{
			{"widget": "table", "object": "crm.opportunity", "query": "crm.open-opportunities", "fields": []string{"title"}},
		},
	})
	if _, kerr := tn.Submit(dana, &pb.Submission{
		TenantId: "t-rc-test", PrincipalId: "dana", Authority: build.ID,
		IdempotencyKey: "p-create-1", Target: &pb.EntityRef{Type: build.PageType, Id: "P1"},
		Schema: &pb.SchemaRef{Name: build.PageType + ".create", Version: 1}, Payload: pagePayload,
	}, now); kerr != nil {
		t.Fatalf("create page: %v (%s)", kerr, kerr.Message)
	}

	// Publish the page
	if _, kerr := tn.Submit(dana, &pb.Submission{
		TenantId: "t-rc-test", PrincipalId: "dana", Authority: build.ID,
		IdempotencyKey: "p-pub-1", Target: &pb.EntityRef{Type: build.PageType, Id: "P1"},
		Schema: &pb.SchemaRef{Name: build.SchemaRelease, Version: 1}, Payload: []byte(`{}`),
	}, now); kerr != nil {
		t.Fatalf("publish page: %v (%s)", kerr, kerr.Message)
	}

	candidate, err := tn.ReleaseCandidate([]platform.AssetRef{{App: build.ID, Kind: platform.AssetPage, Name: "itemspage"}})
	if err != nil {
		t.Fatalf("ReleaseCandidate: %v", err)
	}
	found := false
	for _, asset := range candidate.Assets {
		if asset.Ref.Kind == platform.AssetQuery {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected candidate.Assets to include AssetQuery, but got: %+v", candidate.Assets)
	}
}
