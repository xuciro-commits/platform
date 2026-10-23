package platformserver

import "platformserver/internal/construction"

import (
	"encoding/json"
	resultformat "platformserver/internal/acceptance/result"
	"slices"
	"strings"
	"testing"
	"time"

	"platformserver/apps/ai"
	"platformserver/apps/build"
	"platformserver/platform"
	"platformserver/platform/authz"
)

func TestCapabilityDiscoveryAndResultsUseCurrentOwnerAuthorization(t *testing.T) {
	const id = "capability-owner-authorization"
	at := txnAt
	tenant := composeTenant(t, id, []Seat{
		seatOf("builder", "build:builder", "ai:admin", "platform:admin"),
		seatOf("user", "build:user"),
		seatOf("app:candidate-test", "ai:admin", "platform:admin"),
	}, ai.New(id), build.New(id))
	if err := configureFunctionFixtureModel(tenant, "probe/model", at); err != nil {
		t.Fatal(err)
	}
	publishObject(t, tenant, "builder", "NOTES", map[string]any{
		"name": "note", "title": "Note",
		"fields": []construction.Field{{Name: "text", Title: "Text", Type: "text", Read: []string{construction.Builder}}},
	}, at)
	definition := platform.RecordAdviceFunction("build.note", []string{"text"}, []string{construction.User})
	definition.Name = "advice"
	decide(t, tenant, "builder", construction.ID, construction.FunctionType+".create", construction.FunctionType, "ADVICE", definition, at)
	decide(t, tenant, "builder", construction.ID, build.SchemaFunction, construction.FunctionType, "ADVICE", map[string]any{}, at)
	decide(t, tenant, "builder", construction.ID, "build.note.create", "build.note", "N", map[string]any{"text": "Synthetic task"}, at)

	// The grant permits the function even though the member's primary role
	// is builder. Discovery and result reads must not invent a single-role gate.
	directory := testConsoleOf(tenant)
	directory.mu.Lock()
	directory.members["builder"].Grants = []platform.Grant{{App: construction.ID, Role: construction.Builder}, {App: construction.ID, Role: construction.User}}
	directory.mu.Unlock()
	member := memberOf(t, tenant, "builder")
	if member.Roles[construction.ID] != construction.Builder || !member.Holds(construction.ID, construction.User) {
		t.Fatalf("test needs a non-primary user grant: %+v", member)
	}
	ref := platform.AssetRef{App: construction.ID, Kind: platform.AssetFunction, Name: definition.Name}
	for _, version := range []int{0, 1} {
		if descriptor, problem := tenant.DescribeCapability(member, ref, version); problem != nil || descriptor.Ref != ref {
			t.Fatalf("function version %d ignored its effective grant: %+v %v", version, descriptor, problem)
		}
	}
	call, problem := tenant.InvokeCapability(member, platform.CapabilityInvocation{
		Ref: ref, Version: 1, Key: "advice", Inputs: json.RawMessage(`{"source":"N"}`),
	}, at)
	if problem != nil || call.State != "pending" {
		t.Fatalf("authorized retained function did not start: %+v %v", call, problem)
	}
	if result, problem := tenant.ReadCapabilityAI(member, call.Call, at); problem != nil || result.State != "pending" {
		t.Fatalf("function result ignored its effective grant: %+v %v", result, problem)
	}

	owner := tenant.app(construction.ID)
	owner.Manifest().Actions.UsePolicies(func() []authz.Policy {
		return []authz.Policy{{ID: "deny-advice", Effect: "deny", Permission: definition.Name}}
	})
	assertDenied := func(reason string) {
		t.Helper()
		for _, version := range []int{0, 1} {
			if _, problem := tenant.DescribeCapability(member, ref, version); problem == nil {
				t.Fatalf("%s still disclosed function version %d", reason, version)
			}
		}
		if _, problem := tenant.ReadCapabilityAI(member, call.Call, at); problem == nil {
			t.Fatalf("%s still disclosed the function result", reason)
		}
	}
	assertDenied("owner deny policy")
	owner.Manifest().Actions.UsePolicies(func() []authz.Policy { return nil })
	directory.mu.Lock()
	directory.members["builder"].Grants = []platform.Grant{{App: construction.ID, Role: construction.User}}
	directory.mu.Unlock()
	assertDenied("revoked source-field role with the function role retained")
	directory.mu.Lock()
	directory.members["builder"].Grants = []platform.Grant{{App: construction.ID, Role: construction.Builder}}
	directory.mu.Unlock()
	assertDenied("revoked grant in a stale member snapshot")
}

func TestOwnerAuthorizationExplainsRowsFieldsAndSavedEvidence(t *testing.T) {
	tn := stockTenant(t)
	decide(t, tn, "ana", "stock", "stock.item.create", "stock.item", "I1", map[string]any{"name": "Machine", "line": "L1", "note": "Inspection"}, txnAt)
	request := platform.AuthzRequest{Principal: "bo", Tenant: tn.ID, Kind: "read", Type: "stock.item", Record: "I1", Fields: []string{"note"}}
	decision, err := tn.ExplainOperation(request, txnAt)
	if err != nil || decision.Allow || decision.Layers[len(decision.Layers)-1].Name != "row" {
		t.Fatalf("hidden row explanation: %+v %v", decision, err)
	}
	request.Principal = "lead"
	decision, err = tn.ExplainOperation(request, txnAt)
	if err != nil || !decision.Allow || len(decision.Layers) != 9 {
		t.Fatalf("authorized layers: %+v %v", decision, err)
	}
	request.Kind = "related"
	decision, _ = tn.ExplainOperation(request, txnAt)
	if decision.Allow {
		t.Fatal("related predicate accepted")
	}
	request.Kind = "read"
	request.Tenant = "other"
	decision, _ = tn.ExplainOperation(request, txnAt)
	if decision.Allow || decision.Layers[0].Name != "tenant" {
		t.Fatal("cross-tenant authorization accepted")
	}
	var saved []byte
	tn.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) { saved = entry.Body; return entry.Body, nil }
	decide(t, tn, "ana", "stock", "stock.item.edit", "stock.item", "I1", map[string]any{"note": "Updated"}, txnAt)
	var result struct {
		Authorizations []resultformat.AuthorizationEvidence `json:"authorizations"`
	}
	if json.Unmarshal(saved, &result) != nil || len(result.Authorizations) == 0 || !result.Authorizations[0].Decision.Allow {
		t.Fatal("accepted result omitted authorization basis")
	}
	if !slices.Contains(result.Authorizations[0].Roles, "clerk") {
		t.Fatal("accepted basis omitted original role")
	}
}

func TestAuthorizationRevocationPreservesAcceptedRecovery(t *testing.T) {
	live := stockTenant(t)
	var entries []Entry
	live.Record = func(entry Entry) { entries = append(entries, entry) }
	decide(t, live, "ana", "stock", "stock.item.create", "stock.item", "I1", map[string]any{"name": "Press", "line": "L1"}, txnAt)
	stale := memberOf(t, live, "ana")
	dir := live.app(PlatformApp).(*Console)
	dir.mu.Lock()
	dir.members[stale.ID].Roles, dir.members[stale.ID].Grants = map[string]string{}, nil
	dir.mu.Unlock()
	c := platform.NewCaller(runtime{live}, stale, "stock", false, false)
	question := platform.AuthzRequest{Principal: stale.ID, Tenant: stale.Tenant, Kind: "read", Type: "stock.item", Record: "I1"}
	decision := c.Authorize(question, txnAt)
	if decision.Allow || decision.Layers[len(decision.Layers)-1].Name != "verb" {
		t.Fatal("stale role survived revocation")
	}
	recovered := stockTenant(t)
	recovered.app("stock").Manifest().Actions.UsePolicies(func() []authz.Policy { t.Fatal("recovery reauthorized an accepted result"); return nil })
	if err := recovered.Replay(entries); err != nil {
		t.Fatal(err)
	}
	if row, ok := platform.Get[Item](recovered.automation("stock", false), "I1"); !ok || row.Name != "Press" {
		t.Fatal("accepted historical row was not restored")
	}
}

func TestAuthorizationDefinitionChangesWithItsSourceContractAndPolicies(t *testing.T) {
	tn := stockTenant(t)
	c := platform.NewCaller(runtime{tn}, memberOf(t, tn, "ana"), "stock", false, false)
	request := platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "read", Type: "stock.item"}
	before := c.Authorize(request, txnAt)
	et := tn.records.types["stock.item"]
	note, _ := et.info.Field("note")
	bin, _ := et.info.Field("bin")
	et.info.Derived = []platform.DerivationInfo{{Refs: [][]int{bin.Index}, Fields: [][]int{note.Index}, Names: []string{"bin", "note"}}}
	sourced := c.Authorize(request, txnAt)
	if before.Definition == "" || sourced.Definition == before.Definition {
		t.Fatal("different source contracts retained the same authorization basis")
	}
	tn.app("stock").Manifest().Actions.UsePolicies(func() []authz.Policy {
		return []authz.Policy{{ID: "source-freeze", Effect: "deny", Permission: "*"}}
	})
	frozen := c.Authorize(request, txnAt)
	if frozen.Allow || frozen.Definition == sourced.Definition || frozen.Policy != "source-freeze" {
		t.Fatalf("changed policy was absent from the recorded definition basis: before=%s after=%+v", sourced.Definition, frozen)
	}
}

func TestDeclaredInputAndWorkShareDenyPrecedence(t *testing.T) {
	tn, owner, _ := setup(t, nil)
	owner.Manifest().Actions.UsePolicies(func() []authz.Policy { return []authz.Policy{{ID: "frozen", Effect: "deny", Permission: "*"}} })
	m := memberOf(t, tn, "ana")
	_, refusal := tn.Input(m, "a-feed", []byte("blocked"), txnAt)
	if refusal == nil || !strings.Contains(refusal.Message, "deny") || len(owner.texts) != 0 {
		t.Fatal("input bypassed the shared deny decision")
	}
	draft := tn.newStagedDecision()
	c := platform.NewCaller(draft, platform.Member{ID: "app:a", Tenant: tn.ID}, "a", false, true)
	refusal = draft.authorizeNative(c, "job:declared", "action", nil, true)
	if refusal == nil || len(draft.authorizations) != 1 || draft.authorizations[0].Decision.Policy != "frozen" {
		t.Fatal("owner work bypassed the shared deny decision")
	}
	if resultformat.ValidateAuthorizationEvidence(tn.ID, draft.authorizations) != nil {
		t.Fatal("recorded denial has an invalid layer shape")
	}
	draft.authorizations[0].Decision.Layers[0].Name = "allow"
	if resultformat.ValidateAuthorizationEvidence(tn.ID, draft.authorizations) == nil {
		t.Fatal("recovery accepted reordered authorization layers")
	}
}

func TestGeneratedTextWithMissingSourceIsWithheld(t *testing.T) {
	tn := stockTenant(t)
	et := tn.records.types["stock.item"]
	for i := range et.info.Fields {
		if et.info.Fields[i].Name == "note" {
			et.info.Fields[i].SourceType = "generated"
		}
	}
	note := Item{Record: platform.Record{ID: "missing-source"}, Name: "Press", Note: "Generated detail"}
	raw := platform.Raw(tn.narrowed(memberOf(t, tn, "ana"), note, time.Now(), false))
	if strings.Contains(string(raw), "Generated detail") {
		t.Fatal("generated text without record provenance escaped")
	}
}

func TestDerivedSourceUsesTheOriginalReadPolicy(t *testing.T) {
	tn := stockTenant(t)
	decide(t, tn, "ana", "stock", "stock.item.create", "stock.item", "I1", map[string]any{"name": "Press", "line": "L1", "note": "Private note"}, txnAt)
	tn.app("stock").Manifest().Actions.UsePolicies(func() []authz.Policy {
		return []authz.Policy{{ID: "no-read", Effect: "deny", Permission: "stock.item.read"}}
	})
	if tn.mayRead(memberOf(t, tn, "ana"), txnAt, false)("stock.item/I1#note") {
		t.Fatal("source derivation bypassed the original read policy")
	}
}

func TestDerivedFreeTextRequiresDeclaredSourceType(t *testing.T) {
	tn := stockTenant(t)
	decide(t, tn, "ana", "stock", "stock.item.create", "stock.item", "I1", map[string]any{"name": "Machine", "line": "L1", "note": "Inspection"}, txnAt)
	tn.records.mu.Lock()
	et := tn.records.types["stock.item"]
	value := et.rows.Get("I1").Value.Interface()
	tn.records.mu.Unlock()
	read := tn.narrowed(memberOf(t, tn, "ana"), value, txnAt, false)
	var fields map[string]any
	_ = json.Unmarshal(platform.Raw(read), &fields)
	if fields["note"] != "" {
		t.Fatal("undeclared text escaped through derived read")
	}
	for i := range et.info.Fields {
		if et.info.Fields[i].Name == "note" {
			et.info.Fields[i].SourceType = "user-input"
		}
	}
	read = tn.narrowed(memberOf(t, tn, "ana"), value, txnAt, false)
	_ = json.Unmarshal(platform.Raw(read), &fields)
	if fields["note"] != "Inspection" {
		t.Fatal("declared original input was withheld")
	}
}

func TestSealedAuthorizationEvidenceKeepsChangedOutcomesAndDefinitions(t *testing.T) {
	tn := stockTenant(t)
	decide(t, tn, "ana", "stock", "stock.item.create", "stock.item", "I1", map[string]any{"name": "Machine", "line": "L1", "note": "Inspection"}, txnAt)
	draft := tn.newStagedDecision()
	c := platform.NewCaller(draft, memberOf(t, tn, "lead"), "stock", false, false)
	request := platform.AuthzRequest{Principal: c.ID, Tenant: tn.ID, Kind: "read", Type: "stock.item", Record: "I1", Fields: []string{"note"}}
	for i := 0; i < 100; i++ {
		if answer := c.Authorize(request, txnAt); !answer.Allow {
			t.Fatal("ordinary authorized read was refused")
		}
	}
	tn.app("stock").Manifest().Actions.UsePolicies(func() []authz.Policy { return []authz.Policy{{ID: "review-hold", Effect: "deny", Permission: "*"}} })
	if answer := c.Authorize(request, txnAt); answer.Allow {
		t.Fatal("a repeated question reused permission after its policy changed")
	}
	saved, err := draft.savedAuthorizations()
	if err != nil || len(saved) != 2 || !saved[0].Decision.Allow || saved[1].Decision.Allow || saved[0].Decision.Definition == saved[1].Decision.Definition {
		t.Fatalf("sealing lost a distinct authorization outcome or retained repeated copies: %+v %v", saved, err)
	}
	if len(draft.authorizations) != 101 {
		t.Fatal("sealing changed the actual authorization checks")
	}
}
