package host

import (
	"encoding/json"
	"testing"

	"platformserver/platform"
	"platformserver/services/ontology"
	build "platformserver/services/resources/authoring"
)

func TestInvariantK1TransactionAuthoringAndReplay(t *testing.T) {
	live := newInvariantTenant(t, "invariant-transaction-authoring")
	object := invariantObject("workitem", "Work item")
	object["states"] = []ontology.State{{Name: "open", Title: "Open"}, {Name: "done", Title: "Done"}}
	completion := platform.Expr{Kind: "choice", Lit: &platform.ExprLiteral{Value: platform.Raw("done")}}
	action := ontology.Action{
		Name: "complete", Title: "Complete", From: []string{"open"},
		Transaction: &platform.BusinessTxn{Subject: "invariantproject.workitem", Steps: []platform.TxnStep{{
			Name: "complete", Kind: "update", Target: platform.TxnRef{Type: "invariantproject.workitem", ID: "$subject"},
			Changes: []platform.TxnSet{{Field: "state", Expr: &completion}},
		}}},
	}
	live.accept(t, "build.object.create", build.ObjectType, "ITEM", object)
	live.accept(t, "build.actiontype.create", build.ActionTypeType, "ACTION", map[string]any{
		"name": "invariantproject.workitem.complete", "object": "invariantproject.workitem", "definition": action,
	})
	live.accept(t, "build.actiontype.publish", build.ActionTypeType, "ACTION", map[string]any{})
	live.accept(t, "build.object.publish", build.ObjectType, "ITEM", map[string]any{})
	live.accept(t, "invariantproject.workitem.create", "invariantproject.workitem", "WORK", map[string]any{"note": "open"})
	live.accept(t, "invariantproject.workitem.complete", "invariantproject.workitem", "WORK", map[string]any{})

	read := func(scope *invariantTenant) string {
		t.Helper()
		view, refusal := scope.RecordOf(t.Context(), scope.member(t), "invariantproject.workitem", "WORK", invariantClock.Add(2))
		if refusal != nil {
			t.Fatal(refusal)
		}
		var item struct {
			State string `json:"state"`
		}
		if err := json.Unmarshal(platform.Raw(view.Record), &item); err != nil {
			t.Fatal(err)
		}
		return item.State
	}
	if state := read(live); state != "done" {
		t.Fatalf("transaction left the record in %q", state)
	}
	replayed := live.restore(t, live.entries)
	if state := read(replayed); state != "done" {
		t.Fatalf("replay left the record in %q", state)
	}
}
