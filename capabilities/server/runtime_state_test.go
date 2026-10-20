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
	row := live.records.types["stock.item"].rows.Get("I1")
	if row.History != nil || row.LoadHistory == nil || len(row.RecordHistory()) != 5 {
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
	if len(row.RecordHistory()) != 5 {
		t.Fatal("committed future history leaked into the old working set")
	}
	if len(live.committed.answer("stock/"+next.GetSubmission().GetIdempotencyKey())) != 0 {
		t.Fatal("unapplied SQL answer bypassed the original result application")
	}
	if got, refusal := live.Submit(memberOf(t, live, "ana"), next.GetSubmission(), at.Add(time.Minute)); refusal != nil || !proto.Equal(got, next) {
		t.Fatalf("unknown-outcome retry did not apply its original result: %v", refusal)
	}
	if qty := live.records.types["stock.item"].rows.Get("I1").Value.Interface().(Item).Qty; qty != 6 || j.Position(id) != position+1 {
		t.Fatalf("unknown-outcome retry returned without applying or appended twice: %d", qty)
	}
	restored := compose()
	deployment := Deployment{SnapshotEvery: 1}
	if err = deployment.restoreTenants(ctx, j, "build-b", []*Tenant{restored}, map[string]int64{}, map[string]bool{}); err != nil || restored.quarantined() {
		t.Fatalf("restore failed: %v %v", err, restored.fault.Load())
	}
	current := restored.records.types["stock.item"].rows.Get("I1")
	if current.History != nil || len(current.RecordHistory()) != 6 || current.Value.Interface().(Item).Qty != 6 {
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

// A predecessor writer can have persisted accepted results after SQL's last
// copied position. Recovery must verify the prefix before importing any suffix.
func TestPostgresRuntimeStateRetainedSuffix(t *testing.T) {
	dsn := os.Getenv("PLATFORM_TEST_DATABASE")
	if dsn == "" {
		t.Skip("PLATFORM_TEST_DATABASE not set")
	}
	for _, scenario := range []string{"recover", "damaged-prefix", "bad-delta", "legacy-input"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			id := fmt.Sprintf("runtime-suffix-%s-%d", scenario, time.Now().UnixNano())
			j, err := OpenJournal(ctx, dsn)
			if err != nil {
				t.Fatal(err)
			}
			defer forgetJournal(t, j, id)
			compose := func() *Tenant {
				return composeTenant(t, id, []Seat{seatOf("ana", "stock:clerk")}, &formattedStock{stock: newStock(id), format: "stock.ledger.v1"})
			}
			if _, err = j.Entries(ctx, id, 0); err != nil {
				t.Fatal(err)
			}
			live := compose()
			live.attachJournal(ctx, j)
			at := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
			decide(t, live, "ana", "stock", "stock.item.create", "stock.item", "I1", map[string]any{"name": "one", "qty": 1}, at)
			if err = live.migrateRuntimeState(ctx, j); err != nil {
				t.Fatal(err)
			}
			prefixPosition := j.Position(id)
			state, _, err := live.Snapshot(func() int64 { return prefixPosition })
			if err != nil {
				t.Fatal(err)
			}
			legacy := compose()
			if err = legacy.Restore(state); err != nil {
				t.Fatal(err)
			}
			var suffix []Entry
			legacy.Record = func(entry Entry) {
				if err := j.Append(ctx, id, entry); err != nil {
					t.Fatal(err)
				}
				suffix = append(suffix, entry)
			}
			decide(t, legacy, "ana", "stock", "stock.item.edit", "stock.item", "I1", map[string]any{"qty": 2}, at.Add(time.Second))
			last := decide(t, legacy, "ana", "stock", "stock.item.edit", "stock.item", "I1", map[string]any{"qty": 3}, at.Add(2*time.Second))
			// A newer snapshot must not be used as SQL's older prefix.
			head := j.Position(id)
			headState, _, err := legacy.Snapshot(func() int64 { return head })
			if err != nil {
				t.Fatal(err)
			}
			key, err := legacy.snapshotKey("suffix-build")
			if err != nil {
				t.Fatal(err)
			}
			if err = j.SaveSnapshot(ctx, id, head, key, headState); err != nil {
				t.Fatal(err)
			}
			if scenario == "damaged-prefix" {
				if _, err = j.Pool().Exec(ctx, `update runtime_records set query=jsonb_set(query,'{value,qty}','99') where tenant=$1 and type='stock.item' and id='I1'`, id); err != nil {
					t.Fatal(err)
				}
			}
			originalDelta := j.Delta
			if scenario == "bad-delta" {
				calls := 0
				j.Delta = func(raw []byte) (journal.StateDelta, error) {
					delta, err := originalDelta(raw)
					calls++
					if calls == 2 && len(delta.Records) > 0 {
						delta.Records[0].HistoryBase = 999
					}
					return delta, err
				}
			}
			if scenario == "legacy-input" {
				if err = j.Append(ctx, id, Entry{App: "stock", Kind: "submission", Principal: json.RawMessage(`{}`), Body: json.RawMessage(`{}`), At: at}); err != nil {
					t.Fatal(err)
				}
			}
			restored := compose()
			deployment := Deployment{SnapshotEvery: 1}
			if err = deployment.restoreTenants(ctx, j, "suffix-build", []*Tenant{restored}, map[string]int64{}, map[string]bool{}); err != nil {
				t.Fatal(err)
			}
			position, active, err := j.StatePosition(ctx, id)
			if err != nil || !active {
				t.Fatalf("read state position: %v", err)
			}
			var qty, results int
			if err = j.Pool().QueryRow(ctx, `select (query#>>'{value,qty}')::int from runtime_records where tenant=$1 and type='stock.item' and id='I1'`, id).Scan(&qty); err != nil {
				t.Fatal(err)
			}
			if err = j.Pool().QueryRow(ctx, `select count(*) from runtime_results where tenant=$1`, id).Scan(&results); err != nil {
				t.Fatal(err)
			}
			if scenario != "recover" {
				expected := 1
				if scenario == "damaged-prefix" {
					expected = 99
				}
				if !restored.quarantined() || position != prefixPosition || qty != expected || results != 1 {
					t.Fatalf("failed recovery changed SQL or served tenant: quarantine=%v position=%d qty=%d results=%d", restored.quarantined(), position, qty, results)
				}
				return
			}
			if restored.quarantined() || position != head || qty != 3 || results != 3 {
				t.Fatalf("suffix not restored: %v position=%d qty=%d results=%d", restored.fault.Load(), position, qty, results)
			}
			originalEntries, err := j.Entries(ctx, id, prefixPosition)
			if err != nil || len(originalEntries) != len(suffix) {
				t.Fatalf("journal changed: %v", err)
			}
			for i, entry := range suffix {
				if !sameStoredJSON(originalEntries[i].Body, entry.Body) {
					t.Fatal("retained journal result changed")
				}
				identity, err := acceptedIdentity(entry.Body)
				if err != nil {
					t.Fatal(err)
				}
				raw, err := j.AppendAccepted(ctx, id, entry, identity.Key, identity.Hash)
				if err != nil || !sameStoredJSON(raw, originalEntries[i].Body) || j.Position(id) != head {
					t.Fatalf("recovered answer changed: %v", err)
				}
			}
			got, refusal := restored.Submit(memberOf(t, restored, "ana"), last.GetSubmission(), at.Add(time.Hour))
			if refusal != nil || !proto.Equal(got, last) {
				t.Fatalf("original retry changed: %v", refusal)
			}
			again := compose()
			if err = deployment.restoreTenants(ctx, j, "suffix-build", []*Tenant{again}, map[string]int64{}, map[string]bool{}); err != nil || again.quarantined() {
				t.Fatalf("repeat recovery failed: %v %v", err, again.fault.Load())
			}
			var historyCount int
			if err = j.Pool().QueryRow(ctx, `select history_count from runtime_records where tenant=$1 and type='stock.item' and id='I1'`, id).Scan(&historyCount); err != nil || historyCount != 3 {
				t.Fatalf("repeat recovery appended history: %d %v", historyCount, err)
			}
		})
	}
}

func TestPostgresRuntimeStateRetainedWorkSuffix(t *testing.T) {
	dsn := os.Getenv("PLATFORM_TEST_DATABASE")
	if dsn == "" {
		t.Skip("PLATFORM_TEST_DATABASE not set")
	}
	ctx := context.Background()
	id := fmt.Sprintf("runtime-work-suffix-%d", time.Now().UnixNano())
	j, err := OpenJournal(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer forgetJournal(t, j, id)
	if _, err = j.Entries(ctx, id, 0); err != nil {
		t.Fatal(err)
	}
	live := resultWorkTenant(t, id)
	live.attachJournal(ctx, j)
	if err = live.migrateRuntimeState(ctx, j); err != nil {
		t.Fatal(err)
	}
	state, _, err := live.Snapshot(func() int64 { return j.Position(id) })
	if err != nil {
		t.Fatal(err)
	}
	legacy := resultWorkTenant(t, id)
	if err = legacy.Restore(state); err != nil {
		t.Fatal(err)
	}
	legacy.Record = func(entry Entry) {
		if err := j.Append(ctx, id, entry); err != nil {
			t.Fatal(err)
		}
	}
	at := time.Date(2026, 10, 20, 11, 0, 0, 0, time.UTC)
	runJob(t, legacy, "worker", "pulse", at)
	runJob(t, legacy, "worker", "pulse", at.Add(time.Hour))
	restored := resultWorkTenant(t, id)
	deployment := Deployment{}
	if err = deployment.restoreTenants(ctx, j, "work-suffix", []*Tenant{restored}, map[string]int64{}, map[string]bool{}); err != nil || restored.quarantined() {
		t.Fatalf("work suffix recovery: %v %v", err, restored.fault.Load())
	}
	if calls := restored.app("worker").(*resultWorker).calls; calls != 0 {
		t.Fatalf("recovery reran %d business jobs", calls)
	}
	position, _, err := j.StatePosition(ctx, id)
	if err != nil || position != 2 {
		t.Fatalf("work position %d: %v", position, err)
	}
	if len(restored.app("worker").AcceptedLedger().RecordsFor(id)) != 2 {
		t.Fatal("work decision receipts were lost")
	}
	if err = restored.migrateRuntimeState(ctx, j); err != nil {
		t.Fatalf("work state differs after repeated comparison: %v", err)
	}
}
