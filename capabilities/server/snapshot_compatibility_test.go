package platformserver

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"platformserver/platform"
)

type formattedStock struct {
	*stock
	format  string
	version string
}

func (s *formattedStock) SnapshotFormat() string { return s.format }
func (s *formattedStock) Manifest() platform.Manifest {
	manifest := s.stock.Manifest()
	if s.version != "" {
		manifest.Version = s.version
	}
	return manifest
}

func TestSnapshotRestoreFormatsAndLegacyEligibility(t *testing.T) {
	for _, backend := range journalsUnderTest(t) {
		t.Run(backend.name, func(t *testing.T) {
			id := fmt.Sprintf("snapshot-format-%s-%d", backend.name, time.Now().UnixNano())
			t.Cleanup(func() { backend.cleanup(t, id) })
			journal := backend.open(t, id)
			defer journal.Close()
			compose := func(format, version string) *Tenant {
				return composeTenant(t, id, []Seat{seatOf("clerk", "stock:clerk")},
					&formattedStock{stock: newStock(id), format: format, version: version})
			}
			ctx := context.Background()
			deployment := Deployment{SnapshotEvery: 1}
			start := func(tenant *Tenant, build string) int64 {
				t.Helper()
				restored := map[string]int64{}
				if err := deployment.restoreTenants(ctx, journal, build, []*Tenant{tenant}, restored, map[string]bool{}); err != nil {
					t.Fatal(err)
				}
				if tenant.quarantined() {
					t.Fatal(tenant.fault.Load().Reason)
				}
				return restored[id]
			}
			live := compose("stock.ledger.v1", "1")
			start(live, "build-a")
			at := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
			decide(t, live, "clerk", "stock", "stock.bin.create", "stock.bin", "B1", map[string]any{"code": "one"}, at)
			state, seq, err := live.Snapshot(func() int64 { return journal.Position(id) })
			if err != nil {
				t.Fatal(err)
			}
			key, err := live.snapshotKey("build-a")
			if err != nil || key == "build-a" {
				t.Fatalf("explicit formats did not replace build eligibility: %q %v", key, err)
			}
			if err := journal.SaveSnapshot(ctx, id, seq, key, state); err != nil {
				t.Fatal(err)
			}
			decide(t, live, "clerk", "stock", "stock.bin.create", "stock.bin", "B2", map[string]any{"code": "two"}, at.Add(time.Second))
			restored := compose("stock.ledger.v1", "1")
			if after := start(restored, "build-b"); after != seq || restored.records.types["stock.bin"].rows.Len() != 2 {
				t.Fatalf("compatible build did not restore checkpoint plus suffix: %d", after)
			}
			for _, incompatible := range []*Tenant{compose("stock.ledger.v2", "1"), compose("stock.ledger.v1", "2")} {
				if err := incompatible.Restore(state); err == nil || incompatible.records.types["stock.bin"].rows.Len() != 0 {
					t.Fatal("incompatible owner or composition changed the fresh state")
				}
				if after := start(incompatible, "build-b"); after != 0 || incompatible.records.types["stock.bin"].rows.Len() != 2 {
					t.Fatalf("incompatible format did not replay the retained journal: %d", after)
				}
			}
			if err := restored.checkSnapshotCheckpoint(state, seq+1); err == nil {
				t.Fatal("checkpoint position was not bound to its stored sequence")
			}
			var image tenantState
			if err := json.Unmarshal(state, &image); err != nil {
				t.Fatal(err)
			}
			image.Checkpoint.Build = "another-build"
			changedBuild, _ := json.Marshal(image)
			if err := compose("stock.ledger.v1", "1").Restore(changedBuild); err != nil {
				t.Fatalf("diagnostic build digest became a restore requirement: %v", err)
			}

			// Old checkpoints retain their bytes and their exact-build namespace.
			legacyID := id + "-legacy"
			t.Cleanup(func() { backend.cleanup(t, legacyID) })
			legacy := composeTenant(t, legacyID, []Seat{seatOf("clerk", "stock:clerk")}, newStock(legacyID))
			legacyKey, _ := legacy.snapshotKey("legacy-build")
			if legacyKey != "legacy-build" {
				t.Fatal("an undeclared owner gained cross-build eligibility")
			}
			if _, err := journal.Entries(ctx, legacyID, 0); err != nil {
				t.Fatal(err)
			}
			legacy.attachJournal(ctx, journal)
			decide(t, legacy, "clerk", "stock", "stock.bin.create", "stock.bin", "B1", map[string]any{"code": "old"}, at)
			oldState, oldSeq, err := legacy.Snapshot(func() int64 { return journal.Position(legacyID) })
			if err != nil {
				t.Fatal(err)
			}
			if err := journal.SaveSnapshot(ctx, legacyID, oldSeq, legacyKey, oldState); err != nil {
				t.Fatal(err)
			}
			newOwner := composeTenant(t, legacyID, []Seat{seatOf("clerk", "stock:clerk")}, &formattedStock{stock: newStock(legacyID), format: "stock.ledger.v1"})
			positions := map[string]int64{}
			if err := deployment.restoreTenants(ctx, journal, "legacy-build", []*Tenant{newOwner}, positions, map[string]bool{}); err != nil || newOwner.quarantined() || positions[legacyID] != oldSeq {
				t.Fatalf("exact-build legacy checkpoint was not restored: %v", err)
			}
		})
	}
}
