package records

import (
	"encoding/json"
	"testing"
)

func TestLedgerPeriodBoundaries(t *testing.T) {
	b := ledgerBalancePlan{name: "bykey", keys: []string{"bucket"}, measures: []ledgerMeasurePlan{{name: "qty", kind: "integer"}}}
	plan := ledgerPlan{balances: []ledgerBalancePlan{b}}
	var p ledgerProjection
	for i, f := range []struct {
		day string
		qty int
	}{{"2026-01-01", 10}, {"2026-02-01", -3}, {"2026-02-10", 4}, {"2026-02-15", -2}, {"2026-03-01", 7}} {
		var err error
		p, err = p.apply(plan, ledgerFact{id: string(rune('A' + i)), on: f.day, fields: map[string]json.RawMessage{"bucket": ledgerJSON("A"), "qty": ledgerJSON(f.qty)}})
		if err != nil {
			t.Fatal(err)
		}
	}
	row, err := p.period(b, []json.RawMessage{ledgerJSON("A")}, "2026-02-02", "2026-02-20")
	if err != nil {
		t.Fatal(err)
	}
	for name, x := range map[string]struct {
		got  ledgerTotals
		want string
	}{"opening": {row.Opening, "7"}, "debit": {row.Debit, "4"}, "credit": {row.Credit, "2"}, "closing": {row.Closing, "9"}} {
		if string(x.got["qty"]) != x.want {
			t.Fatalf("%s: %s want %s", name, x.got["qty"], x.want)
		}
	}
	row, err = p.period(b, []json.RawMessage{ledgerJSON("A")}, "", "2026-02-28")
	if err != nil || string(row.Closing["qty"]) != "9" {
		t.Fatalf("as of: %+v %v", row, err)
	}
}
