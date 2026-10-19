package platformserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/journal"
)

func TestPostgresRuntimeStateMigrationAcceptedHistoryAndRetry(t *testing.T) {
	dsn := os.Getenv("PLATFORM_TEST_DATABASE")
	if dsn == "" {
		t.Skip("PLATFORM_TEST_DATABASE not set")
	}
	ctx := context.Background()
	id := fmt.Sprintf("runtime-state-%d", time.Now().UnixNano())
	j, err := OpenJournal(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer forgetJournal(t, j, id)
	compose := func() *Tenant {
		return composeTenant(t, id, []Seat{seatOf("ana", "stock:clerk")}, &formattedStock{stock: newStock(id), format: "stock.ledger.v1"})
	}
	live := compose()
	if _, err = j.Entries(ctx, id, 0); err != nil {
		t.Fatal(err)
	}
	// Existing journals are copied at a paused position before SQL-backed reads
	// are installed. This first accepted record represents the predecessor path.
	live.attachJournal(ctx, j)
	at := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	first := decide(t, live, "ana", "stock", "stock.item.create", "stock.item", "I1", map[string]any{"name": "one", "qty": 1}, at)
	if err = live.migrateRuntimeState(ctx, j); err != nil {
		t.Fatal(err)
	}
	for n := 2; n <= 5; n++ {
		decide(t, live, "ana", "stock", "stock.item.edit", "stock.item", "I1", map[string]any{"qty": n}, at.Add(time.Duration(n)*time.Second))
	}
	row := live.records.types["stock.item"].rows.get("I1")
	if row.history != nil || row.loadHistory == nil || len(row.recordHistory()) != 5 {
		t.Fatal("complete record history remained resident or was lost")
	}
	if len(live.committed.allAnswers()) == 0 && len(live.app("stock").AcceptedLedger().RecordsFor(id)) != 5 {
		t.Fatal("receipt history was lost")
	}
	position := j.Position(id)
	repeated, refusal := live.Submit(memberOf(t, live, "ana"), proto.Clone(first.GetSubmission()).(*pb.Submission), at.Add(time.Hour))
	if refusal != nil || !proto.Equal(repeated, first) || j.Position(id) != position {
		t.Fatalf("archived retry changed its original answer: %v", refusal)
	}
	if len(live.committed.answers) > 1 || len(live.committed.schemaVersions) > 1 {
		t.Fatal("historical answers accumulated in memory")
	}
	var storedPosition int64
	if err = j.Pool().QueryRow(ctx, `select seq from runtime_tenants where tenant=$1`, id).Scan(&storedPosition); err != nil || storedPosition != position {
		t.Fatalf("accepted position did not share the state transaction: %d %v", storedPosition, err)
	}

	// An error while the authority provides a delta must roll back the journal,
	// current image, history, idempotent answer and position together.
	state, _, err := live.Snapshot(func() int64 { return position })
	if err != nil {
		t.Fatal(err)
	}
	scratch := compose()
	if err = scratch.Restore(state); err != nil {
		t.Fatal(err)
	}
	var entry Entry
	scratch.Record = func(e Entry) { entry = e }
	next := decide(t, scratch, "ana", "stock", "stock.item.edit", "stock.item", "I1", map[string]any{"qty": 6}, at.Add(10*time.Second))
	identity, err := acceptedIdentity(entry.Body)
	if err != nil {
		t.Fatal(err)
	}
	originalDelta := j.Delta
	j.Delta = func([]byte) (journal.StateDelta, error) {
		return journal.StateDelta{}, errors.New("storage owner failed")
	}
	if _, err = j.AppendAccepted(ctx, id, entry, identity.Key, identity.Hash); err == nil {
		t.Fatal("failed owner delta was accepted")
	}
	j.Delta = originalDelta
	if j.Position(id) != position {
		t.Fatal("failed transaction advanced the journal")
	}
	var n int
	if err = j.Pool().QueryRow(ctx, `select history_count from runtime_records where tenant=$1 and type='stock.item' and id='I1'`, id).Scan(&n); err != nil || n != 5 {
		t.Fatalf("failed transaction changed history: %d %v", n, err)
	}
	// Commit without applying to the old working set, then recover. The old row
	// retains its own prefix while the restarted tenant reads the new current row.
	raw, err := j.AppendAccepted(ctx, id, entry, identity.Key, identity.Hash)
	if err != nil {
		t.Fatal(err)
	}
	if len(row.recordHistory()) != 5 {
		t.Fatal("committed future history leaked into the old working set")
	}
	restored := compose()
	deployment := Deployment{SnapshotEvery: 1}
	if err = deployment.restoreTenants(ctx, j, "build-b", []*Tenant{restored}, map[string]int64{}, map[string]bool{}); err != nil || restored.quarantined() {
		t.Fatalf("restore failed: %v %v", err, restored.fault.Load())
	}
	current := restored.records.types["stock.item"].rows.get("I1")
	if current.history != nil || len(current.recordHistory()) != 6 || current.value.Interface().(Item).Qty != 6 {
		t.Fatal("restart did not load committed current/history")
	}
	if saved, err := j.AppendAccepted(ctx, id, entry, identity.Key, identity.Hash); err != nil || !sameStoredJSON(saved, raw) || j.Position(id) != position+1 {
		t.Fatal("unknown-outcome retry appended again")
	}
	if got, refusal := restored.Submit(memberOf(t, restored, "ana"), next.GetSubmission(), at.Add(time.Hour)); refusal != nil || !proto.Equal(got, next) {
		t.Fatalf("recovered idempotency changed: %v", refusal)
	}
}

func TestPostgresRuntimeStateRefusesMigrationDifference(t *testing.T) {
	dsn := os.Getenv("PLATFORM_TEST_DATABASE")
	if dsn == "" {
		t.Skip("PLATFORM_TEST_DATABASE not set")
	}
	ctx := context.Background()
	id := fmt.Sprintf("runtime-compare-%d", time.Now().UnixNano())
	j, err := OpenJournal(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer forgetJournal(t, j, id)
	if _, err = j.Entries(ctx, id, 0); err != nil {
		t.Fatal(err)
	}
	live := composeTenant(t, id, []Seat{seatOf("ana", "stock:clerk")}, newStock(id))
	live.attachJournal(ctx, j)
	decide(t, live, "ana", "stock", "stock.item.create", "stock.item", "I1", map[string]any{"name": "one", "qty": 1}, time.Now().UTC())
	if err = live.migrateRuntimeState(ctx, j); err != nil {
		t.Fatal(err)
	}
	if _, err = j.Pool().Exec(ctx, `update runtime_records set query=jsonb_set(query,'{value,qty}','99') where tenant=$1 and type='stock.item' and id='I1'`, id); err != nil {
		t.Fatal(err)
	}
	if err = live.migrateRuntimeState(ctx, j); err == nil {
		t.Fatal("migration silently replaced differing authoritative SQL state")
	}
	var value json.RawMessage
	if err = j.Pool().QueryRow(ctx, `select query#>'{value,qty}' from runtime_records where tenant=$1 and type='stock.item' and id='I1'`, id).Scan(&value); err != nil || string(value) != "99" {
		t.Fatal("failed compare changed SQL state")
	}
}
