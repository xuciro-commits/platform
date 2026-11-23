package host

import (
	"encoding/json"
	"reflect"
	"testing"

	"platformserver/kernel/protocol"
	"platformserver/platform"
	"platformserver/services/actions"
	"platformserver/services/ontology"
	build "platformserver/services/resources/authoring"
	"platformserver/services/work"
)

func TestInvariantK33AnApprovalMoveRefreshesTotalsOverDerivedFields(t *testing.T) {
	const tenant, budget, spend = "invariant-k33", "invariantproject.budget", "invariantproject.spend"
	staff := platform.Member{ID: "staff-1", Tenant: tenant, Roles: map[string]string{build.ID: ontology.UserRole}}
	live := newInvariantTenantWithApps(t, tenant, []platform.Service{work.New(tenant, actions.NewAuthority)}, protocol.Seat{Subjects: []string{staff.ID}, Member: staff})
	live.accept(t, "build.object.create", build.ObjectType, "SPEND", map[string]any{"name": "spend", "title": "Spend",
		"fields": []map[string]any{{"name": "budget", "title": "Budget", "type": "reference", "ref": budget, "inverse": "spends", "required": true},
			{"name": "amount", "title": "Amount", "type": "integer", "required": true}, {"name": "owner", "title": "Owner", "type": "member", "default": "member"},
			{"name": "double", "title": "Double", "type": "integer", "formula": "amount * 2"}},
		"scope":  map[string]any{"owner": "owner"},
		"states": []ontology.State{{Name: "draft", Title: "Draft"}, {Name: "pending", Title: "Pending"}, {Name: "approved", Title: "Approved"}},
		"access": []ontology.Access{{Role: ontology.UserRole, Read: "own", Create: true, Edit: true}}})
	live.accept(t, "build.object.create", build.ObjectType, "BUDGET", map[string]any{"name": "budget", "title": "Budget",
		"sets":  []map[string]any{{"name": "spends", "object": spend, "direction": "inverse", "from": "spends", "limit": 200, "order": []map[string]any{{"expr": map[string]any{"op": "", "kind": "text", "ref": "id"}}}}},
		"reads": map[string]any{"collections": map[string]any{"spends": map[string]any{"app": build.ID, "object": spend, "policy": "complete", "set": "spends"}}},
		"fields": []map[string]any{{"name": "limit", "title": "Limit", "type": "integer", "required": true},
			{"name": "doubled", "title": "Doubled", "type": "integer", "formulaExpr": map[string]any{"op": "sum", "kind": "integer", "agg": map[string]any{"source": "spends", "field": []string{"double"}}}}},
		"access": []map[string]any{{"role": ontology.UserRole, "read": "all"}}})
	approved := &platform.Expr{Kind: "choice", Lit: &platform.ExprLiteral{Value: platform.Raw("approved")}}
	submit := ontology.Action{Name: "submit", Title: "Submit", From: []string{"draft"},
		Approval:    &ontology.ActionApproval{Pending: "pending", Levels: []ontology.ApproverLevel{{Title: "Builder", Role: ontology.BuilderRole}}},
		Transaction: &platform.BusinessTxn{Subject: spend, Steps: []platform.TxnStep{{Name: "approve", Kind: "update", Target: platform.TxnRef{Type: spend, ID: "$subject"}, Changes: []platform.TxnSet{{Field: "state", Expr: approved}}}}}}
	live.accept(t, "build.actiontype.create", build.ActionTypeType, "SUBMIT", map[string]any{"name": spend + ".submit", "object": spend, "definition": submit})
	live.accept(t, "build.actiontype.publish", build.ActionTypeType, "SUBMIT", map[string]any{})
	live.accept(t, "build.object.publish", build.ObjectType, "SPEND", map[string]any{})
	live.accept(t, "build.object.publish", build.ObjectType, "BUDGET", nil)
	live.accept(t, budget+".create", budget, "B1", map[string]any{"limit": 100})

	as := func(schema, typ, id string, payload any) *platform.Error {
		t.Helper()
		sub := live.submission(schema, typ, id, payload)
		sub.PrincipalId = staff.ID
		_, refusal := live.Submit(staff, sub, invariantClock)
		return refusal
	}
	doubled := func() any {
		t.Helper()
		view, refusal := live.RecordOf(t.Context(), staff, budget, "B1", invariantClock)
		if refusal != nil {
			t.Fatal(refusal.Message)
		}
		var rec map[string]any
		_ = json.Unmarshal(platform.Raw(view.Record), &rec)
		return rec["doubled"]
	}
	if refusal := as(spend+".create", spend, "S1", map[string]any{"budget": "B1", "amount": 30}); refusal != nil {
		t.Fatalf("create: %s", refusal.Message)
	}
	if refusal := as(spend+".submit", spend, "S1", map[string]any{}); refusal != nil {
		t.Fatalf("holding a spend for approval must refresh the budget over its derived field: %s", refusal.Message)
	}
	if got := doubled(); got != float64(60) {
		t.Fatalf("the budget's total must still count the held spend's derived field: %v", got)
	}
	before := live.recordImage(t)
	stored := make([]Entry, len(live.entries))
	for i, e := range live.entries {
		raw, _ := json.Marshal(e)
		_ = json.Unmarshal(raw, &stored[i])
	}
	fresh := newInvariantTenantReplayWithApps(t, tenant, []platform.Service{work.New(tenant, actions.NewAuthority)}, protocol.Seat{Subjects: []string{staff.ID}, Member: staff})
	if err := fresh.recoverEntries(stored); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if after := fresh.recordImage(t); !reflect.DeepEqual(before, after) {
		t.Fatal("replay must read the refreshed totals back")
	}
}
