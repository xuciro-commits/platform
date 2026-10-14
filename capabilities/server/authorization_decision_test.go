package platformserver

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"platformserver/platform"
	"platformserver/platform/authz"
)

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
		Authorizations []authorizationEvidence `json:"authorizations"`
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
	if validateAuthorizationEvidence(tn.ID, draft.authorizations) != nil {
		t.Fatal("recorded denial has an invalid layer shape")
	}
	draft.authorizations[0].Decision.Layers[0].Name = "allow"
	if validateAuthorizationEvidence(tn.ID, draft.authorizations) == nil {
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
	value := et.rows.get("I1").value.Interface()
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
