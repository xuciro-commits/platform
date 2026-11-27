package host

import (
	"encoding/json"
	"platformserver/kernel/records"
	"platformserver/platform"
	"platformserver/services/objects"
	build "platformserver/services/resources/authoring"
	"strings"
	"sync"
	"testing"
)

// This is the host boundary, not just a test of the accumulator.
func TestInvariantK47LedgerFactsAndProjection(t *testing.T) {
	live := newInvariantTenant(t, "invariant-ledger")
	const fact = "invariantproject.posting"
	const balance = fact + ".balance"
	live.accept(t, "build.object.create", build.ObjectType, "POSTING", map[string]any{
		"name": "posting", "title": "Posting", "ledger": true,
		"fields":   []map[string]any{{"name": "bucket", "title": "Bucket", "type": "text", "required": true}, {"name": "qty", "title": "Quantity", "type": "integer", "required": true}},
		"balances": []map[string]any{{"name": "balance", "title": "Balance", "keys": []string{"bucket"}, "measures": []string{"qty"}, "constraints": []platform.Constraint{{Name: "available", Kind: "expression", Fields: []string{"qty"}, Expr: &platform.Expr{Op: ">=", Kind: "boolean", Args: []platform.Expr{platform.ExprReference("integer", "qty"), {Kind: "integer", Lit: &platform.ExprLiteral{Value: platform.Raw(0)}}}}, Message: "Not enough quantity"}}}},
	})
	live.accept(t, "build.object.publish", build.ObjectType, "POSTING", nil)
	live.accept(t, fact+".create", fact, "A", map[string]any{"bucket": "K", "qty": 10})
	member := live.member(t)
	var wg sync.WaitGroup
	refusals := make(chan *platform.Error, 2)
	for _, id := range []string{"B", "C"} {
		submission := live.submission(fact+".create", fact, id, map[string]any{"bucket": "K", "qty": -6})
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, refusal := live.Submit(member, submission, invariantClock)
			refusals <- refusal
		}()
	}
	wg.Wait()
	close(refusals)
	accepted, rejected := 0, 0
	for refusal := range refusals {
		if refusal == nil {
			accepted++
		} else if strings.Contains(refusal.Message, "Not enough quantity") {
			rejected++
		} else {
			t.Fatalf("wrong refusal: %+v", refusal)
		}
	}
	if accepted != 1 || rejected != 1 {
		t.Fatalf("concurrent 6+6: %d accepted, %d refused", accepted, rejected)
	}
	read := func(l *invariantTenant) json.RawMessage {
		t.Helper()
		page, err := l.Records(t.Context(), l.member(t), balance, platform.Query{}, invariantClock)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Records) != 1 {
			t.Fatalf("balance count: %s", platform.Raw(page))
		}
		return platform.Raw(page.Records[0])
	}
	before := read(live)
	if diff, err := live.Projection.ReconcileLedger(fact); err != nil || len(diff) > 0 {
		t.Fatalf("reconcile: %v %v", diff, err)
	}
	raw, _, err := live.Snapshot(func() int64 { return int64(len(live.entries)) })
	if err != nil {
		t.Fatal(err)
	}
	restored := newInvariantTenantReplayWithApps(t, live.ID, nil)
	if err := restored.Restore(raw); err != nil {
		t.Fatalf("snapshot restore: %v", err)
	}
	if after := read(restored); string(after) != string(before) {
		t.Fatalf("snapshot balance differs: %s != %s", after, before)
	}
	period, refusal := live.aggregateObjectSet(live.member(t), objects.ObjectSetRequest{Set: platform.ObjectSet{Object: balance}, Groups: []string{"bucket"}, Measures: []string{"sum:qty"}, From: "2026-10-01", To: "2026-10-31"}, invariantClock)
	if refusal != nil || len(period.Rows) != 1 {
		t.Fatalf("ledger period: %+v %v", period, refusal)
	}
	for k, want := range map[string]string{"opening:qty": "0", "debit:qty": "10", "credit:qty": "6", "closing:qty": "4"} {
		if string(platform.Raw(period.Rows[0][k])) != want {
			t.Fatalf("period %s: %v want %s", k, period.Rows[0][k], want)
		}
	}
	var row map[string]any
	if json.Unmarshal(before, &row) != nil || row["qty"] != float64(4) {
		t.Fatalf("balance: %s", before)
	}
	info, ok := live.Entity(fact)
	if !ok {
		t.Fatal("ledger type missing")
	}
	for _, schema := range info.Standard {
		if schema == fact+".edit" || schema == fact+".archive" {
			t.Fatalf("mutable ledger action declared: %s", schema)
		}
	}
	et := live.Projection.Type(balance)
	for id, row := range et.Rows().All() {
		copy := *row
		copy.Value = records.Copy(et.Info().Go, row.Value.Interface())
		if err := et.SetRow(id, &copy); err == nil {
			t.Fatal("second balance writer allowed")
		}
	}
	for _, schema := range []string{fact + ".edit", fact + ".archive", balance + ".create", balance + ".edit", balance + ".archive"} {
		typ := fact
		if strings.HasPrefix(schema, balance+".") {
			typ = balance
		}
		if _, refusal := live.Submit(live.member(t), live.submission(schema, typ, "A", map[string]any{}), invariantClock); refusal == nil {
			t.Fatalf("immutable record accepted %s", schema)
		}
	}
	fresh := live.restore(t, live.entries)
	if after := read(fresh); string(after) != string(before) {
		t.Fatalf("replay balance differs: %s != %s", after, before)
	}
}

func TestInvariantK48LedgerReversalAndClose(t *testing.T) {
	live := newInvariantTenant(t, "invariant-ledger-reverse")
	const fact = "invariantproject.posting"
	const balance = fact + ".balance"
	live.accept(t, "build.object.create", build.ObjectType, "POSTING", map[string]any{
		"name": "posting", "title": "Posting", "ledger": true, "closedBefore": "2026-10-01",
		"fields":   []map[string]any{{"name": "bucket", "title": "Bucket", "type": "text", "required": true}, {"name": "qty", "title": "Quantity", "type": "integer", "required": true}},
		"balances": []map[string]any{{"name": "balance", "title": "Balance", "keys": []string{"bucket"}, "measures": []string{"qty"}}},
	})
	live.accept(t, "build.object.publish", build.ObjectType, "POSTING", nil)
	live.accept(t, fact+".create", fact, "A", map[string]any{"bucket": "K", "qty": 10, "on": "2026-10-01"})
	live.accept(t, fact+".create", fact, "B", map[string]any{"bucket": "K", "qty": -6})
	before := live.recordImage(t)[fact]
	refuse := func(schema, id string, payload any, reason string) {
		t.Helper()
		_, err := live.Submit(live.member(t), live.submission(schema, fact, id, payload), invariantClock)
		if err == nil || reason != "" && !strings.Contains(err.Message, reason) {
			t.Fatalf("%s %s refusal: %+v", schema, id, err)
		}
	}
	refuse(fact+".create", "C", map[string]any{"bucket": "K", "qty": 1, "on": "2026-09-30"}, "closed")
	refuse(fact+".reverse", "B", map[string]any{"on": "2026-09-30"}, "closed")
	live.accept(t, fact+".reverse", fact, "B", map[string]any{})
	refuse(fact+".reverse", "B", map[string]any{}, "already reversed")
	original, err := live.RecordOf(t.Context(), live.member(t), fact, "B", invariantClock)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if json.Unmarshal(platform.Raw(original.Record), &value) != nil || value["reversed"] != true || value["qty"] != float64(-6) {
		t.Fatalf("reversed marker: %s", platform.Raw(original.Record))
	}
	page, err := live.Records(t.Context(), live.member(t), fact, platform.Query{}, invariantClock)
	if err != nil || len(page.Records) != 3 {
		t.Fatalf("fact count: %+v %v", page, err)
	}
	var reversal bool
	for _, raw := range page.Records {
		var v map[string]any
		_ = json.Unmarshal(platform.Raw(raw), &v)
		if v["reverses"] == "B" && v["qty"] == float64(6) {
			reversal = true
		}
	}
	if !reversal {
		t.Fatal("missing opposite fact")
	}
	var oldRows, newRows []struct{ Value json.RawMessage }
	_ = json.Unmarshal(before, &oldRows)
	_ = json.Unmarshal(live.recordImage(t)[fact], &newRows)
	for _, old := range oldRows {
		found := false
		for _, next := range newRows {
			if string(old.Value) == string(next.Value) {
				found = true
			}
		}
		if !found {
			t.Fatalf("original fact was rewritten: %s", old.Value)
		}
	}
	read := func(l *invariantTenant) json.RawMessage {
		t.Helper()
		page, err := l.Records(t.Context(), l.member(t), balance, platform.Query{}, invariantClock)
		if err != nil || len(page.Records) != 1 {
			t.Fatalf("balance page: %+v %v", page, err)
		}
		return platform.Raw(page.Records[0])
	}
	row := read(live)
	if !strings.Contains(string(row), `"qty":10`) {
		t.Fatalf("reversal balance: %s", row)
	}
	fresh := live.restore(t, live.entries)
	if string(read(fresh)) != string(row) {
		t.Fatal("reversal replay differs")
	}
}
