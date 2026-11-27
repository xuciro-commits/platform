package records

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestLedgerProjectionTrialAndRebuild(t *testing.T) {
	plan := ledgerPlan{balances: []ledgerBalancePlan{
		{name: "bykey", keys: []string{"bucket"}, measures: []ledgerMeasurePlan{{name: "qty", kind: "integer"}}, nonnegative: []string{"qty"}},
		{name: "total", measures: []ledgerMeasurePlan{{name: "qty", kind: "integer"}}},
	}}
	fact := func(id, on, bucket string, qty int64) ledgerFact {
		return ledgerFact{id: id, on: on, fields: map[string]json.RawMessage{"bucket": ledgerJSON(bucket), "qty": ledgerJSON(qty)}}
	}
	var image ledgerProjection
	first := fact("F0", "2026-01-01", "A", 10)
	var err error
	image, err = image.apply(plan, first)
	if err != nil {
		t.Fatal(err)
	}
	original := image
	next, err := image.apply(plan, fact("F1", "2026-01-02", "A", -6))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := next.apply(plan, fact("F2", "2026-01-02", "A", -6)); err == nil {
		t.Fatal("overspend accepted")
	}
	if got := original.balance("bykey", []json.RawMessage{ledgerJSON("A")})["qty"]; string(got) != "10" {
		t.Fatalf("trial mutated image: %s", got)
	}
	if got := next.balance("bykey", []json.RawMessage{ledgerJSON("A")})["qty"]; string(got) != "4" {
		t.Fatalf("balance: %s", got)
	}
	// Accepted batch rows may arrive in ID order, not business order.
	var replay ledgerProjection
	replay, err = replay.apply(plan.replayPlan(), fact("A", "2026-01-02", "A", -6))
	if err != nil {
		t.Fatal(err)
	}
	replay, err = replay.apply(plan.replayPlan(), first)
	if err != nil {
		t.Fatal(err)
	}
	if got := replay.balance("bykey", []json.RawMessage{ledgerJSON("A")})["qty"]; string(got) != "4" {
		t.Fatalf("reordered replay: %s", got)
	}
	// Independent oracle: plain map of signed fact quantities, no projection code.
	expected := map[string]int64{"A": 4}
	image = next
	for i := 0; i < 10000; i++ {
		b := fmt.Sprintf("B%d", i%19)
		qty := int64(i%7 + 1)
		image, err = image.apply(plan, fact(fmt.Sprintf("P%d", i), "2026-02-01", b, qty))
		if err != nil {
			t.Fatal(err)
		}
		expected[b] += qty
	}
	for key, want := range expected {
		if got := image.balance("bykey", []json.RawMessage{ledgerJSON(key)})["qty"]; string(got) != fmt.Sprint(want) {
			t.Fatalf("%s: %s != %d", key, got, want)
		}
	}
	if got := image.balance("bykey", []json.RawMessage{ledgerJSON("A")})["qty"]; string(got) != "4" {
		t.Fatal("unrelated key changed")
	}
}

func TestLedgerProjectionExactAmounts(t *testing.T) {
	plan := ledgerPlan{balances: []ledgerBalancePlan{{name: "total", measures: []ledgerMeasurePlan{{name: "number", kind: "decimal"}, {name: "cash", kind: "money"}}}}}
	var image ledgerProjection
	for i := 0; i < 3; i++ {
		var err error
		image, err = image.apply(plan, ledgerFact{id: fmt.Sprint(i), on: "2026-01-01", fields: map[string]json.RawMessage{"number": json.RawMessage("0.1"), "cash": json.RawMessage(`{"amount":10,"currency":"USD"}`)}})
		if err != nil {
			t.Fatal(err)
		}
	}
	values := image.balance("total", nil)
	if string(values["number"]) != "0.3" || string(values["cash"]) != `{"amount":30,"currency":"USD"}` {
		t.Fatalf("inexact measures: %s", ledgerJSON(values))
	}
	if _, err := image.apply(plan, ledgerFact{id: "foreign", on: "2026-01-01", fields: map[string]json.RawMessage{"number": json.RawMessage("0.1"), "cash": json.RawMessage(`{"amount":10,"currency":"EUR"}`)}}); err == nil {
		t.Fatal("mixed currencies accepted")
	}
}
