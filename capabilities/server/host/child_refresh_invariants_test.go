package host

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"platformserver/kernel/protocol"
	"platformserver/platform"
	"platformserver/services/ontology"
	build "platformserver/services/resources/authoring"
)

func TestInvariantK32AddingAChildRefreshesItsParentWithoutEditingIt(t *testing.T) {
	const tenant, budget, spend = "invariant-k32", "invariantproject.budget", "invariantproject.spend"
	staff := platform.Member{ID: "staff-1", Tenant: tenant, Roles: map[string]string{build.ID: ontology.UserRole}}
	live := newInvariantTenant(t, tenant, protocol.Seat{Subjects: []string{staff.ID}, Member: staff})
	live.accept(t, "build.object.create", build.ObjectType, "BUDGET", map[string]any{"name": "budget", "title": "Budget",
		"sets":  []map[string]any{{"name": "spends", "object": spend, "direction": "inverse", "from": "spends", "limit": 200, "order": []map[string]any{{"expr": map[string]any{"op": "", "kind": "text", "ref": "id"}}}}},
		"reads": map[string]any{"collections": map[string]any{"spends": map[string]any{"app": build.ID, "object": spend, "policy": "complete", "set": "spends"}}},
		"fields": []map[string]any{{"name": "limit", "title": "Limit", "type": "integer", "required": true},
			{"name": "spent", "title": "Spent", "type": "integer", "formulaExpr": map[string]any{"op": "sum", "kind": "integer", "agg": map[string]any{"source": "spends", "field": []string{"amount"}}}}},
		"access": []ontology.Access{{Role: ontology.UserRole, Read: "all"}}})
	live.accept(t, "build.object.create", build.ObjectType, "SPEND", map[string]any{"name": "spend", "title": "Spend",
		"fields": []ontology.Field{{Name: "budget", Title: "Budget", Type: "reference", Ref: budget, Inverse: "spends", Required: true}, {Name: "amount", Title: "Amount", Type: "integer", Required: true}},
		"access": []ontology.Access{{Role: ontology.UserRole, Read: "all", Create: true, Edit: true}}})
	live.accept(t, "build.object.publish", build.ObjectType, "SPEND", nil)
	live.accept(t, "build.object.publish", build.ObjectType, "BUDGET", nil)
	live.accept(t, budget+".create", budget, "B1", map[string]any{"limit": 100})

	as := func(schema, typ, id string, payload any) *platform.Error {
		t.Helper()
		sub := live.submission(schema, typ, id, payload)
		sub.PrincipalId = staff.ID
		_, refusal := live.Submit(staff, sub, invariantClock)
		return refusal
	}
	spent := func() any {
		t.Helper()
		view, refusal := live.RecordOf(t.Context(), staff, budget, "B1", invariantClock)
		if refusal != nil {
			t.Fatal(refusal.Message)
		}
		var rec map[string]any
		_ = json.Unmarshal(platform.Raw(view.Record), &rec)
		return rec["spent"]
	}
	if refusal := as(spend+".create", spend, "S1", map[string]any{"budget": "B1", "amount": 30}); refusal != nil {
		t.Fatalf("a member who may add a spend but not edit the budget must be able to add it: %s", refusal.Message)
	}
	if got := spent(); got != float64(30) {
		t.Fatalf("the budget's total must follow the new spend: %v", got)
	}
	if refusal := as(spend+".edit", spend, "S1", map[string]any{"amount": 45}); refusal != nil {
		t.Fatalf("editing the spend: %s", refusal.Message)
	}
	if got := spent(); got != float64(45) {
		t.Fatalf("the budget's total must follow the edited spend: %v", got)
	}
	if refusal := as(budget+".edit", budget, "B1", map[string]any{"limit": 500}); refusal == nil || !strings.Contains(refusal.Message, "verb") {
		t.Fatalf("the member still may not edit the budget itself, got %#v", refusal)
	}
	before := live.recordImage(t)
	if after := live.restore(t, live.entries, protocol.Seat{Subjects: []string{staff.ID}, Member: staff}).recordImage(t); !reflect.DeepEqual(before, after) {
		t.Fatal("replay must read the refreshed totals back")
	}
}
