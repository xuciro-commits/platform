package platformserver

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"platformserver/platform"
)

func TestPostgresCapabilityPreparationUsesCurrentReadScope(t *testing.T) {
	dsn := os.Getenv("PLATFORM_TEST_DATABASE")
	if dsn == "" {
		t.Skip("PLATFORM_TEST_DATABASE not set")
	}
	ctx := context.Background()
	id := fmt.Sprintf("capability-scope-%d", time.Now().UnixNano())
	j, err := OpenJournal(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer forgetJournal(t, j, id)
	app := &formattedStock{stock: newStock(id), format: "stock.ledger.v1"}
	live := composeTenant(t, id, []Seat{seatOf("ana", "stock:clerk", "invariants:operator")}, app, newConstraintOwner(id))
	if _, err = j.Entries(ctx, id, 0); err != nil {
		t.Fatal(err)
	}
	live.attachJournal(ctx, j)
	now := time.Now().UTC()
	decide(t, live, "ana", "stock", "stock.item.create", "stock.item", "I1", map[string]any{"name": "one", "qty": 7}, now)
	decide(t, live, "ana", "invariants", "invariants.entry.create", "invariants.entry", "E1", validConstraintEntry("first"), now)
	decide(t, live, "ana", "invariants", "invariants.entry.create", "invariants.entry", "E2", validConstraintEntry("second"), now)
	if err = live.migrateRuntimeState(ctx, j); err != nil {
		t.Fatal(err)
	}
	m := memberOf(t, live, "ana")
	ref := platform.AssetRef{App: "stock", Kind: platform.AssetAction, Name: "stock.item.edit"}
	if len(live.Capabilities(m)) == 0 {
		t.Fatal("capabilities disappeared after SQL conversion")
	}
	if d, refusal := live.DescribeCapability(m, ref, 0); refusal != nil || d.Target != "stock.item" {
		t.Fatal(d, refusal)
	}
	answer, refusal := live.InvokeCapability(m, platform.CapabilityInvocation{
		Ref: ref, Key: "bind-current-qty", Target: "I1", Record: "stock.item/I1", Inputs: []byte(`{}`),
		Bindings: map[string]platform.Binding{"qty": {Source: "subject", Path: []string{"qty"}}},
	}, now.Add(time.Second))
	if refusal != nil || answer.State != "completed" {
		t.Fatal(answer, refusal)
	}
	row, refusal := live.RecordOf(m, "stock.item", "I1", now)
	if refusal != nil || row.Record.(Item).Qty != 7 || row.Record.(Item).Revision != 2 {
		t.Fatal(row, refusal)
	}
	if live.records.readView != nil || live.records.types["stock.item"].rows.Stored() != 0 {
		t.Fatal("capability retained an SQL transaction or row cache")
	}
	// Refusal details also read current records. Filter them before leaving
	// the submission's view, including delivery of the same durable refusal.
	q := platform.CapabilityInvocation{
		Ref: platform.AssetRef{App: "invariants", Kind: platform.AssetAction, Name: "invariants.entry.edit"},
		Key: "duplicate-number", Target: "E2", Inputs: []byte(`{"number":"first"}`),
	}
	for range 2 {
		answer, refusal = live.InvokeCapability(m, q, now)
		if refusal == nil || len(answer.Issues) == 0 || answer.Issues[0].Constraint != "number" {
			t.Fatal("unique refusal details unavailable", answer, refusal)
		}
	}
}
