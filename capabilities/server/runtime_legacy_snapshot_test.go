package platformserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestPostgresLegacySnapshotMissingHistoryRetriesOriginalJournal(t *testing.T) {
	dsn := os.Getenv("PLATFORM_TEST_DATABASE")
	if dsn == "" {
		t.Skip("PLATFORM_TEST_DATABASE not set")
	}
	ctx := context.Background()
	id := fmt.Sprintf("legacy-snapshot-history-%d", time.Now().UnixNano())
	j, err := OpenJournal(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer forgetJournal(t, j, id)
	compose := func() *Tenant {
		return composeTenant(t, id, []Seat{seatOf("ana", "platform:admin", "stock:clerk")},
			&formattedStock{stock: newStock(id), format: "stock.ledger.v1"})
	}
	live := compose()
	if _, err := j.Entries(ctx, id, 0); err != nil {
		t.Fatal(err)
	}
	live.attachJournal(ctx, j)
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	first := decide(t, live, "ana", "stock", "stock.item.create", "stock.item", "I1", map[string]any{"name": "Original", "qty": 1, "line": "L1"}, at)
	if err := live.migrateRuntimeState(ctx, j); err != nil {
		t.Fatal(err)
	}
	decide(t, live, "ana", "stock", "stock.item.edit", "stock.item", "I1", map[string]any{"qty": 2}, at.Add(time.Second))
	position := j.Position(id)
	journalBefore, err := j.Entries(ctx, id, 0)
	if err != nil || len(journalBefore) != 2 {
		t.Fatalf("original journal: %d entries, %v", len(journalBefore), err)
	}
	imageBefore, err := j.StateRecord(ctx, id, "stock.item", "I1")
	if err != nil {
		t.Fatal(err)
	}
	historyBefore, err := j.StateHistory(ctx, id, "stock.item", "I1", 0, 2)
	if err != nil || len(historyBefore) != 2 {
		t.Fatalf("original SQL history: %d entries, %v", len(historyBefore), err)
	}
	state, seq, err := live.Snapshot(func() int64 { return position })
	if err != nil {
		t.Fatal(err)
	}
	var legacy tenantState
	if err := json.Unmarshal(state, &legacy); err != nil {
		t.Fatal(err)
	}
	if len(legacy.Records["stock.item"]) != 1 || len(legacy.Records["stock.item"][0].History) != 2 {
		t.Fatal("SQL-backed snapshot lost its history before the old defect was reproduced")
	}
	// Older SQL attachment code retained a row's count but accidentally read
	// len(nil) while attaching its loader. The resulting snapshot held a sound
	// current image and ledger, but no history or chain declaration.
	legacy.Records["stock.item"][0].History = nil
	legacy.Records["stock.item"][0].HistoryHead = ""
	damaged, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	code := CodeOf(live)
	key, err := live.snapshotKey(code)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.SaveSnapshot(ctx, id, seq, key, damaged); err != nil {
		t.Fatal(err)
	}
	deployment := Deployment{SnapshotEvery: 1, Rebuild: func(string) (*Tenant, error) { return compose(), nil }}
	old := compose()
	restored := map[string]int64{}
	if err := deployment.restoreTenants(ctx, j, code, []*Tenant{old}, restored, map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	if !old.quarantined() || restored[id] != position || !strings.Contains(old.fault.Load().Reason, "state differs for stock.item/I1") {
		t.Fatalf("incomplete snapshot bypassed strict SQL comparison: restored=%d fault=%+v", restored[id], old.fault.Load())
	}
	assertOriginal := func() {
		t.Helper()
		entries, err := j.Entries(ctx, id, 0)
		if err != nil || j.Position(id) != position || !reflect.DeepEqual(entries, journalBefore) {
			t.Fatalf("recovery changed the accepted journal: %v", err)
		}
		image, err := j.StateRecord(ctx, id, "stock.item", "I1")
		if err != nil || !reflect.DeepEqual(image, imageBefore) {
			t.Fatalf("recovery changed the SQL business image: %v", err)
		}
		history, err := j.StateHistory(ctx, id, "stock.item", "I1", 0, 2)
		if err != nil || !reflect.DeepEqual(history, historyBefore) {
			t.Fatalf("recovery changed the SQL business history: %v", err)
		}
	}
	assertOriginal()
	registry := newTenantRegistry([]*Tenant{old})
	if err := deployment.retryTenant(ctx, j, registry, code, id); err != nil {
		t.Fatalf("retained full journal did not repair the tenant: %v", err)
	}
	recovered := registry.current(id)
	if recovered == old || recovered.quarantined() || !old.quarantined() || recovered.records.types["stock.item"].rows.get("I1").historyLength() != 2 {
		t.Fatal("recovery exposed the incomplete generation or lost original history")
	}
	if again, err := recovered.Submit(memberOf(t, recovered, "ana"), first.GetSubmission(), at.Add(time.Hour)); err != nil || again.GetChangeId() != first.GetChangeId() {
		t.Fatalf("recovery lost the original accepted answer: %v", err)
	}
	assertOriginal()
	// retryTenant repairs the snapshot through the existing path. A subsequent
	// ordinary startup must now pass the same strict comparison with SQL.
	restarted := compose()
	if err := deployment.restoreTenants(ctx, j, code, []*Tenant{restarted}, map[string]int64{}, map[string]bool{}); err != nil || restarted.quarantined() || restarted.records.types["stock.item"].rows.get("I1").historyLength() != 2 {
		t.Fatalf("repaired snapshot did not survive another startup: %v, fault=%+v", err, restarted.fault.Load())
	}
	assertOriginal()
}
