package host

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	"platformserver/services/ontology"
	build "platformserver/services/resources/authoring"
)

func TestInvariantK28ActionParametersCarryMoney(t *testing.T) {
	live := newInvariantTenant(t, "invariant-k28")
	const typ, schema = "invariantproject.invoice", "invariantproject.invoice.settle"
	paid := platform.Expr{Kind: "choice", Lit: &platform.ExprLiteral{Value: platform.Raw("paid")}}
	settle := func(name, inputType string) ontology.Action {
		return ontology.Action{Name: name, Title: "Settle", From: []string{"open"}, Roles: []string{ontology.BuilderRole},
			Inputs: []ontology.Input{{Name: "final", Title: "Final amount", Type: inputType, Required: true}},
			Transaction: &platform.BusinessTxn{Subject: typ, Steps: []platform.TxnStep{{
				Name: "settle", Kind: "update", Target: platform.TxnRef{Type: typ, ID: "$subject"},
				Changes: []platform.TxnSet{{Field: "amount", Expr: &platform.Expr{Ref: "input.final", Kind: "money"}}, {Field: "state", Expr: &paid}},
			}}}}
	}
	live.accept(t, "build.object.create", build.ObjectType, "INVOICE", map[string]any{"name": "invoice", "title": "Invoice",
		"fields": []ontology.Field{{Name: "amount", Title: "Amount", Type: "money"}},
		"states": []ontology.State{{Name: "open", Title: "Open"}, {Name: "paid", Title: "Paid"}}})
	const other = "invariantproject.receipt"
	live.accept(t, "build.object.create", build.ObjectType, "RECEIPT", map[string]any{"name": "receipt", "title": "Receipt",
		"fields": []ontology.Field{{Name: "amount", Title: "Amount", Type: "money"}},
		"states": []ontology.State{{Name: "open", Title: "Open"}, {Name: "paid", Title: "Paid"}}})
	bad := settle("void", "currency")
	bad.Transaction.Subject, bad.Transaction.Steps[0].Target.Type = other, other
	refused := func() *platform.Error {
		if _, refusal := live.Submit(live.member(t), live.submission("build.actiontype.create", build.ActionTypeType, "BAD", map[string]any{"name": other + ".void", "object": other, "definition": bad}), invariantClock); refusal != nil {
			return refusal
		}
		_, refusal := live.Submit(live.member(t), live.submission("build.actiontype.publish", build.ActionTypeType, "BAD", map[string]any{}), invariantClock)
		return refusal
	}()
	if refused == nil || !strings.Contains(refused.Message, `"final"`) {
		t.Fatalf("saving an action whose input has an unknown type must name the input, got %#v", refused)
	}
	live.accept(t, "build.actiontype.create", build.ActionTypeType, "SETTLE", map[string]any{"name": schema, "object": typ, "definition": settle("settle", "money")})
	live.accept(t, "build.actiontype.publish", build.ActionTypeType, "SETTLE", map[string]any{})
	live.accept(t, "build.object.publish", build.ObjectType, "INVOICE", map[string]any{})
	live.accept(t, typ+".create", typ, "I1", map[string]any{"amount": map[string]any{"amount": 10000, "currency": "USD"}})
	live.accept(t, schema, typ, "I1", map[string]any{"final": map[string]any{"amount": 9000, "currency": "USD"}})
	view, refusal := live.RecordOf(t.Context(), live.member(t), typ, "I1", invariantClock)
	if refusal != nil {
		t.Fatal(refusal.Message)
	}
	var record struct {
		Amount platform.Money `json:"amount"`
		State  string         `json:"state"`
	}
	_ = json.Unmarshal(platform.Raw(view.Record), &record)
	if record.Amount != (platform.Money{Amount: 9000, Currency: "USD"}) || record.State != "paid" {
		t.Fatalf("a money parameter must reach the transaction: %+v", record)
	}
	live.accept(t, typ+".create", typ, "I2", map[string]any{"amount": map[string]any{"amount": 500, "currency": "USD"}})
	for _, bad := range []any{12, map[string]any{"amount": 9.5, "currency": "USD"}, map[string]any{"amount": 900, "currency": "usd"}, map[string]any{"amount": 900}} {
		_, refusal := live.Submit(live.member(t), live.submission(schema, typ, "I2", map[string]any{"final": bad}), invariantClock)
		if refusal == nil || refusal.Code != pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT {
			t.Fatalf("a malformed money parameter %v must be refused as invalid input, got %#v", bad, refusal)
		}
	}
	before := live.recordImage(t)
	if after := live.restore(t, live.entries).recordImage(t); !reflect.DeepEqual(before, after) {
		t.Fatal("replay must read the settled amount back")
	}
}
