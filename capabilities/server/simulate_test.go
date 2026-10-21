package platformserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"platformserver/internal/construction"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/build"
	"platformserver/platform"
)

// A builder tries an action as a user: the dry run shows what it would write
// or why it is refused, and nothing of it is kept.
func TestSimulateIsDiscarded(t *testing.T) {
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	seat := func(id, role string) Seat {
		return Seat{Subjects: []string{id}, Member: platform.Member{ID: id, Roles: map[string]string{construction.ID: role}}}
	}
	tn, err := NewTenant("sim", NewConsole("sim", seat("dana", construction.Builder), seat("eli", construction.User)), build.New("sim"))
	if err != nil {
		t.Fatal(err)
	}
	var journal []Entry
	tn.Record = func(e Entry) { journal = append(journal, e) }
	member := func(id string) platform.Member { m, _ := tn.Member(id); return m }
	keys := 0
	sub := func(who, schema, typ, id string, payload any) *pb.Submission {
		keys++
		raw, _ := json.Marshal(payload)
		return &pb.Submission{TenantId: "sim", PrincipalId: who, Authority: construction.ID, IdempotencyKey: fmt.Sprint("k", keys),
			Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}
	}
	must := func(who string, s *pb.Submission) {
		t.Helper()
		if _, err := tn.Submit(member(who), s, now); err != nil {
			t.Fatalf("%s: %s %s", s.GetSchema().GetName(), err.Code, err.Message)
		}
	}
	must("dana", sub("dana", construction.ObjectType+".create", construction.ObjectType, "O1", map[string]any{"name": "visit", "title": "Visit",
		"fields": []map[string]any{{"name": "guest", "title": "Guest", "type": "text"}},
		"states": []map[string]any{{"name": "open", "title": "Open"}, {"name": "done", "title": "Done"}},
		"actions": []map[string]any{{"name": "close", "title": "Close", "from": []string{"open"}, "to": "done",
			"conditions": []map[string]any{{"field": "guest", "operator": "=", "value": "Ada", "message": "Only Ada can close this visit"}}}}}))
	must("dana", sub("dana", construction.SchemaPublish, construction.ObjectType, "O1", map[string]any{}))
	visit := construction.TypeOf("visit")
	must("eli", sub("eli", visit+".create", visit, "V1", map[string]any{"guest": "Ada"}))
	must("eli", sub("eli", visit+".create", visit, "V2", map[string]any{"guest": "Bob"}))
	before, entries := snapshot(tn), len(journal)

	out, kerr := tn.Simulate(member("dana"), "eli", sub("eli", visit+".close", visit, "V1", map[string]any{}), now)
	if kerr != nil {
		t.Fatalf("dry run error: %v", kerr)
	}
	if !out.Accepted || len(out.Changes) != 1 || !strings.Contains(string(out.Changes[0].Record), `"state":"done"`) {
		t.Fatalf("dry run of an allowed action: %+v", out)
	}
	out, kerr = tn.Simulate(member("dana"), "eli", sub("eli", visit+".close", visit, "V2", map[string]any{}), now)
	if kerr != nil || out.Accepted || !strings.Contains(out.Refusal, "Only Ada can close this visit") {
		t.Fatalf("dry run of a refused action: %+v, %v", out, err)
	}
	if out, err := tn.Simulate(member("eli"), "", sub("eli", visit+".close", visit, "V1", map[string]any{}), now); err != nil || !out.Accepted {
		t.Fatalf("a member could not try their own action: %+v %v", out, err)
	}
	if snapshot(tn) != before || len(journal) != entries {
		t.Fatal("a dry run left records, notices or journal entries behind")
	}
}

func TestSimulationOwnIdentityAuthorizationDiagnosticsAndRevision(t *testing.T) {
	tn := stockTenant(t)
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	ana := memberOf(t, tn, "ana")
	lead := memberOf(t, tn, "lead")
	declared, _ := tn.app("stock").Manifest().Actions.Action("stock.item.edit")
	declared.Payload = append([]platform.Field(nil), declared.Payload...)
	for i := range declared.Payload {
		if declared.Payload[i].Name == "qty" {
			min := 0.0
			declared.Payload[i].Constraints = &platform.InputConstraints{Min: &min}
		}
	}
	tn.app("stock").Manifest().Actions.Add(declared)
	decide(t, tn, "ana", "stock", "stock.item.create", "stock.item", "I1", map[string]any{"name": "Bolt", "qty": 2, "line": "L1"}, at)
	request := submission(tn, ana, "stock", "stock.item.edit", "stock.item", "I1", map[string]any{"qty": 7})
	request.TenantId, request.PrincipalId, request.Authority = "untrusted", lead.ID, PlatformApp
	original := proto.Clone(request)
	before := snapshot(tn)
	count := len(tn.app("stock").AcceptedLedger().RecordsFor(tn.ID))
	var journal int
	tn.Record = func(Entry) { journal++ }
	tn.AcceptResult = func(Entry, string, string) ([]byte, error) {
		journal++
		t.Fatal("a simulation attempted to persist")
		return nil, nil
	}
	for _, as := range []string{"", ana.ID} {
		out, err := tn.Simulate(ana, as, request, at)
		if err != nil || !out.Accepted || len(out.Changes) != 1 || !strings.Contains(string(out.Changes[0].Record), `"qty":7`) {
			t.Fatalf("ordinary own trial: %+v %v", out, err)
		}
	}
	if !proto.Equal(original, request) {
		t.Fatal("trial rewrote the caller's request")
	}
	if _, err := tn.Simulate(ana, lead.ID, request, at); err == nil || err.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
		t.Fatalf("ordinary member impersonated another actor: %v", err)
	}
	denied := submission(tn, ana, "stock", "stock.item.count", "stock.item", "I1", map[string]any{"qty": 9})
	out, err := tn.Simulate(ana, "", denied, at)
	if err != nil || out.Accepted || out.RefusalCode != pb.ErrorCode_ERROR_CODE_POLICY_DENIED.String() {
		t.Fatalf("forbidden own action: %+v %v", out, err)
	}
	invalid := submission(tn, ana, "stock", "stock.item.edit", "stock.item", "I1", map[string]any{"qty": -1})
	out, err = tn.Simulate(ana, "", invalid, at)
	if err != nil || out.Accepted || out.RefusalCode != pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT.String() || len(out.Issues) != 1 || out.Issues[0].Path[0] != "qty" {
		t.Fatalf("field diagnostics: %+v %v", out, err)
	}
	stale := uint32(0)
	request.ExpectedRevision = &stale
	out, err = tn.Simulate(ana, "", request, at)
	if err != nil || out.Accepted || out.RefusalCode != pb.ErrorCode_ERROR_CODE_CONFLICT.String() || out.Unsupported {
		t.Fatalf("stale expected revision: %+v %v", out, err)
	}
	if snapshot(tn) != before || journal != 0 || len(tn.app("stock").AcceptedLedger().RecordsFor(tn.ID)) != count {
		t.Fatal("trial changed records, audit, ledger, notices, effects, or journal")
	}
}

func TestSimulationRouteAllowsOwnActionAndRefreshesRevokedPermissions(t *testing.T) {
	tn := stockTenant(t)
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	ana := memberOf(t, tn, "ana")
	request := submission(tn, ana, "stock", "stock.bin.create", "stock.bin", "B1", map[string]any{"code": "A-01"})
	h := NewHost(Tokens(map[string]string{"ana": "ana"}), tn)
	h.Now = func() time.Time { return at }
	raw, err := protojson.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/simulate", strings.NewReader(string(raw)))
	req.Header.Set("Authorization", "Bearer ana")
	response := httptest.NewRecorder()
	h.Handler().ServeHTTP(response, req)
	var out Simulation
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &out) != nil || !out.Accepted {
		t.Fatalf("self trial HTTP route: %d %s", response.Code, response.Body.String())
	}
	console := tn.app(PlatformApp).(*Console)
	console.mu.Lock()
	console.members[ana.ID].Roles = map[string]string{}
	console.members[ana.ID].Grants = nil
	console.mu.Unlock()
	out, refusal := tn.Simulate(ana, "", request, at)
	if refusal == nil && out.Accepted {
		t.Fatal("stale caller permissions survived revocation")
	}
	if len(tn.app("stock").AcceptedLedger().RecordsFor(tn.ID)) != 0 || tn.records.types["stock.bin"].rows.Len() != 0 {
		t.Fatal("HTTP trial persisted a record")
	}
}

type unsupportedTrialStock struct{ *stock }

func (a *unsupportedTrialStock) Submit(c platform.Caller, s *pb.Submission, at time.Time) (*pb.ChangeRecord, *kernel.Error) {
	if s.GetSchema().GetName() != "stock.item.trial" {
		return a.stock.Submit(c, s, at)
	}
	return a.ledger.Receive(c, s, at, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		return func(r *pb.ChangeRecord) {
			c.Put(r, Item{Record: platform.Record{ID: s.GetTarget().GetId()}, Name: "Private", Line: "L1"})
			c.Notify(platform.Notification{Title: "Private trial notice", Key: "trial"}, at, platform.Recipient{Member: c.ID})
			c.Deliver("", "", "", at)
		}, nil
	})
}
func TestSimulationUnsupportedEffectDoesNotFallBackToExecution(t *testing.T) {
	owner := newStock("unsupported-trial")
	if err := owner.ledger.Extend(nil, []platform.Action{{Schema: "stock.item.trial", Target: "stock.item", Capability: "trial", Title: "Trial", Description: "Trial", Payload: []platform.Field{}, Roles: []string{"clerk"}}}); err != nil {
		t.Fatal(err)
	}
	tn := composeTenant(t, "unsupported-trial", []Seat{seatOf("ana", "stock:clerk")}, &unsupportedTrialStock{owner})
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	ana := memberOf(t, tn, "ana")
	before := snapshot(tn)
	tn.AcceptResult = func(Entry, string, string) ([]byte, error) {
		t.Fatal("unsupported trial attempted to persist")
		return nil, nil
	}
	tn.Record = func(Entry) { t.Fatal("unsupported trial wrote a journal entry") }
	out, err := tn.Simulate(ana, "", submission(tn, ana, "stock", "stock.item.trial", "stock.item", "I1", map[string]any{}), at)
	if err != nil || out.Accepted || !out.Unsupported || out.RefusalCode != pb.ErrorCode_ERROR_CODE_CONFLICT.String() || len(out.Changes) != 0 {
		t.Fatalf("unsupported trial: %+v %v", out, err)
	}
	if snapshot(tn) != before || len(owner.ledger.RecordsFor(tn.ID)) != 0 || len(tn.events) != 0 || len(tn.notices.all) != 0 || len(tn.outbound) != 0 {
		t.Fatal("unsupported trial leaked private records, notices or effects")
	}
}

func TestSimulationResultsUseBuilderReadPermissions(t *testing.T) {
	tn := stockTenant(t, build.New("t-1"))
	console := tn.app(PlatformApp).(*Console)
	console.members["op"].Roles[construction.ID] = construction.Builder
	builder, _ := tn.Member("op") // reads only line L1, without the lead-only secret
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	before := snapshot(tn)
	for _, line := range []string{"L1", "L2"} {
		payload, _ := json.Marshal(map[string]string{"name": "Private part", "line": line, "secret": "lead-only value"})
		sub := &pb.Submission{TenantId: "original", PrincipalId: "original", Authority: "original",
			Target: &pb.EntityRef{Type: "stock.item", Id: "TEST"}, Schema: &pb.SchemaRef{Name: "stock.item.create", Version: 1}, Payload: payload}
		original := proto.Clone(sub)
		out, err := tn.Simulate(builder, "lead", sub, now)
		if err != nil || !out.Accepted {
			t.Fatalf("simulate %s: %+v, %v", line, out, err)
		}
		if !proto.Equal(original, sub) {
			t.Fatal("simulation altered the supplied submission")
		}
		if line == "L1" {
			if len(out.Changes) != 1 || strings.Contains(string(out.Changes[0].Record), "lead-only value") {
				t.Fatalf("field read mask not applied: %+v", out)
			}
		} else if len(out.Changes) != 0 {
			t.Fatalf("out-of-scope test record leaked: %+v", out)
		}
	}
	foreign := builder
	foreign.Tenant = "foreign"
	if _, err := tn.Simulate(foreign, "lead", nil, now); err == nil {
		t.Fatal("foreign builder simulated this tenant")
	}
	if snapshot(tn) != before {
		t.Fatal("simulation changed production records or audit state")
	}
}
