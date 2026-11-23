package host

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"platformserver/platform"
	"platformserver/services/ontology"
	build "platformserver/services/resources/authoring"
)

func TestInvariantK30StateConditionedValuesSurviveTransitions(t *testing.T) {
	live := newInvariantTenant(t, "invariant-k30")
	const typ = "invariantproject.case"
	closed := &platform.Expr{Kind: "choice", Lit: &platform.ExprLiteral{Value: platform.Raw("closed")}}
	live.accept(t, "build.object.create", build.ObjectType, "CASE", map[string]any{"name": "case", "title": "Case",
		"fields": []ontology.Field{{Name: "title", Title: "Title", Type: "text"}, {Name: "nextstep", Title: "Next step", Type: "text", When: "state=open"}},
		"states": []ontology.State{{Name: "open", Title: "Open"}, {Name: "closed", Title: "Closed"}}})
	close := ontology.Action{Name: "close", Title: "Close", From: []string{"open"}, Roles: []string{ontology.BuilderRole},
		Transaction: &platform.BusinessTxn{Subject: typ, Steps: []platform.TxnStep{{
			Name: "close", Kind: "update", Target: platform.TxnRef{Type: typ, ID: "$subject"}, Changes: []platform.TxnSet{{Field: "state", Expr: closed}},
		}}}}
	live.accept(t, "build.actiontype.create", build.ActionTypeType, "CLOSE", map[string]any{"name": typ + ".close", "object": typ, "definition": close})
	live.accept(t, "build.actiontype.publish", build.ActionTypeType, "CLOSE", map[string]any{})
	live.accept(t, "build.object.publish", build.ObjectType, "CASE", map[string]any{})

	live.accept(t, typ+".create", typ, "C1", map[string]any{"title": "Printer", "nextstep": "Call the vendor"})
	live.accept(t, typ+".close", typ, "C1", map[string]any{})
	read := func() map[string]any {
		t.Helper()
		view, refusal := live.RecordOf(t.Context(), live.member(t), typ, "C1", invariantClock)
		if refusal != nil {
			t.Fatal(refusal.Message)
		}
		var rec map[string]any
		_ = json.Unmarshal(platform.Raw(view.Record), &rec)
		return rec
	}
	if got := read(); got["state"] != "closed" || got["nextstep"] != "Call the vendor" {
		t.Fatalf("leaving the state a field applies in must keep its value: %v", got)
	}
	live.accept(t, typ+".edit", typ, "C1", map[string]any{"title": "Printer on floor 3"})
	if got := read(); got["title"] != "Printer on floor 3" || got["nextstep"] != "Call the vendor" {
		t.Fatalf("editing another field must keep the retained value: %v", got)
	}
	if _, refusal := live.Submit(live.member(t), live.submission(typ+".edit", typ, "C1", map[string]any{"nextstep": "Order a part"}), invariantClock); refusal == nil || !strings.Contains(refusal.Message, "applies only when") {
		t.Fatalf("writing a new value into a field that does not apply must still be refused, got %#v", refusal)
	}
	live.accept(t, typ+".edit", typ, "C1", map[string]any{"nextstep": nil})
	if got := read(); got["nextstep"] != nil {
		t.Fatalf("a retained value can be cleared: %v", got)
	}
	before := live.recordImage(t)
	if after := live.restore(t, live.entries).recordImage(t); !reflect.DeepEqual(before, after) {
		t.Fatal("replay must keep the retained value")
	}
}
