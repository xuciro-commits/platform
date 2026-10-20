package platformserver

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestPostgresHistoryChainBootstrapChecksOriginalHistory(t *testing.T) {
	dsn := os.Getenv("PLATFORM_TEST_DATABASE")
	if dsn == "" {
		t.Skip("PLATFORM_TEST_DATABASE not set")
	}
	ctx := context.Background()
	id := fmt.Sprintf("history-chain-%d", time.Now().UnixNano())
	j, err := OpenJournal(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer forgetJournal(t, j, id)
	live := composeTenant(t, id, []Seat{seatOf("ana", "stock:clerk")}, &formattedStock{stock: newStock(id), format: "stock.ledger.v1"})
	if _, err := j.Entries(ctx, id, 0); err != nil {
		t.Fatal(err)
	}
	live.attachJournal(ctx, j)
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	decide(t, live, "ana", "stock", "stock.item.create", "stock.item", "I1", map[string]any{"name": "one", "qty": 1}, at)
	if err := live.migrateRuntimeState(ctx, j); err != nil {
		t.Fatal(err)
	}
	original, _, err := live.Snapshot(func() int64 { return j.Position(id) })
	if err != nil {
		t.Fatal(err)
	}
	var journalBefore string
	if err := j.Pool().QueryRow(ctx, `select body::text from journal where tenant=$1`, id).Scan(&journalBefore); err != nil {
		t.Fatal(err)
	}
	// Simulate a database whose original rows precede the derived chain column.
	if _, err := j.Pool().Exec(ctx, `update runtime_records set history_head='' where tenant=$1`, id); err != nil {
		t.Fatal(err)
	}
	if err := live.migrateRuntimeState(ctx, j); err != nil {
		t.Fatal(err)
	}
	var head, journalAfter string
	if err := j.Pool().QueryRow(ctx, `select history_head from runtime_records where tenant=$1`, id).Scan(&head); err != nil || head == "" {
		t.Fatalf("missing derived head: %q %v", head, err)
	}
	if err := j.Pool().QueryRow(ctx, `select body::text from journal where tenant=$1`, id).Scan(&journalAfter); err != nil || journalBefore != journalAfter {
		t.Fatal("derived bootstrap changed accepted history", err)
	}
	// An established head cannot be overwritten just because a new expected head exists.
	if _, err := j.Pool().Exec(ctx, `update runtime_records set history_head='damaged' where tenant=$1`, id); err != nil {
		t.Fatal(err)
	}
	if err := live.migrateRuntimeState(ctx, j); err == nil {
		t.Fatal("damaged established head was accepted")
	}
	// Bootstrap must compare the original history before storing derived metadata.
	if _, err := j.Pool().Exec(ctx, `update runtime_records set history_head='' where tenant=$1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Pool().Exec(ctx, `update runtime_history set query=jsonb_set(query,'{by}','"someone-else"') where tenant=$1`, id); err != nil {
		t.Fatal(err)
	}
	fresh := composeTenant(t, id, []Seat{seatOf("ana", "stock:clerk")}, &formattedStock{stock: newStock(id), format: "stock.ledger.v1"})
	if err := fresh.Restore(original); err != nil {
		t.Fatal(err)
	}
	if err := fresh.migrateRuntimeState(ctx, j); err == nil {
		t.Fatal("changed historical prefix was accepted during bootstrap")
	}
	if err := j.Pool().QueryRow(ctx, `select history_head from runtime_records where tenant=$1`, id).Scan(&head); err != nil || head != "" {
		t.Fatalf("failed bootstrap changed head: %q %v", head, err)
	}
}
