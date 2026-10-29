package platformserver

import (
	"bytes"
	"context"
	"os"
	"platformserver/engine/resources"
	acceptedformat "platformserver/kernel/acceptedformat"
	"platformserver/platform"
	"strings"
	"testing"
	"time"
)

// These two phases are called by the isolated deployment rehearsal around the
// real cross-instance handover. Neither selects a daily database by default.
func TestPostgresStorageHandoverPrepare(t *testing.T) {
	address := os.Getenv("PLATFORM_HANDOVER_PREPARE_DATABASE")
	if address == "" {
		t.Skip("PLATFORM_HANDOVER_PREPARE_DATABASE not set")
	}
	ctx := context.Background()
	store, err := OpenJournal(ctx, address)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	tenant := composeTenant(t, "storage-handover", []platform.Seat{seatOf("operator", "stock:clerk")}, newStock("storage-handover"))
	if _, err := store.Entries(ctx, tenant.ID, 0); err != nil {
		t.Fatal(err)
	}
	tenant.attachJournal(ctx, store)
	at := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	decide(t, tenant, "operator", "stock", "stock.bin.create", "stock.bin", "retained", map[string]any{"code": "original"}, at)
	decide(t, tenant, "operator", "stock", "stock.bin.create", "stock.bin", "current", map[string]any{"code": "current"}, at.Add(time.Second))
	if err := store.SaveSnapshot(ctx, tenant.ID, store.Position(tenant.ID), "handover-fixture", []byte(`{"owner":"retained"}`)); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresStorageHandoverVerify(t *testing.T) {
	address := os.Getenv("PLATFORM_HANDOVER_TARGET_DATABASE")
	source := os.Getenv("PLATFORM_HANDOVER_SOURCE_DATABASE")
	if address == "" || source == "" {
		t.Skip("handover source and target databases not set")
	}
	ctx := context.Background()
	if stale, err := OpenJournal(ctx, source); err == nil {
		stale.Close()
		t.Fatal("stale source reopened")
	} else if !strings.Contains(err.Error(), "fenced") {
		t.Fatal(err)
	}
	store, err := OpenJournal(ctx, address)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	tenant := composeTenant(t, "storage-handover", []platform.Seat{seatOf("operator", "stock:clerk")}, newStock("storage-handover"))
	entries, err := store.Entries(ctx, tenant.ID, 0)
	if err != nil || len(entries) != 2 {
		t.Fatal(len(entries), err)
	}
	if err := tenant.Replay(entries); err != nil {
		t.Fatal(err)
	}
	tenant.attachJournal(ctx, store)
	row, refusal := tenant.RecordOf(memberOf(t, tenant, "operator"), "stock.bin", "current", time.Now())
	if refusal != nil || row.Record.(Bin).Code != "current" {
		t.Fatal(row, refusal)
	}
	before := store.Position(tenant.ID)
	// Retry the original command identity; it must return the saved answer.
	_, receipt, err := acceptedformat.DecodeRecord(entries[0].Body)
	if err != nil {
		t.Fatal(err)
	}
	// Same owner factory and accepted images also retain business history.
	if _, state, found, err := store.Snapshot(ctx, tenant.ID, "handover-fixture"); err != nil || !found || !bytes.Equal(state, []byte(`{"owner":"retained"}`)) {
		t.Fatal(found, string(state), err)
	}
	at := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	if again, err := tenant.Submit(memberOf(t, tenant, "operator"), receipt.Submission, at); err != nil || again.GetChangeId() != receipt.ChangeId {
		t.Fatal(again, err)
	}
	if store.Position(tenant.ID) != before {
		t.Fatal("same command was appended after handover")
	}
	request := submission(tenant, memberOf(t, tenant, "operator"), "stock", "stock.bin.create", "stock.bin", "suffix", map[string]any{"code": "afterhandover"})
	request.IdempotencyKey = "handover-new-suffix"
	if _, refusal := tenant.Submit(memberOf(t, tenant, "operator"), request, at.Add(2*time.Second)); refusal != nil {
		t.Fatal(refusal)
	}
	if store.Position(tenant.ID) != before+1 {
		t.Fatal("target did not accept a new suffix")
	}
	if stale, err := OpenJournal(ctx, source); err == nil {
		stale.Close()
		t.Fatal("stale source reopened after target append")
	}
	files := &resources.MemoryFiles{}
	if err := files.Put(ctx, "retained/receipt", platform.Raw(entries), "application/json"); err != nil {
		t.Fatal(err)
	}
	if manifest, err := resources.FileStoreManifest(ctx, files); err != nil || len(manifest) != 1 {
		t.Fatal(manifest, err)
	}
}
