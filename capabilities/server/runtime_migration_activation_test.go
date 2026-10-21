package platformserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"platformserver/internal/construction"
	"platformserver/platform"
)

func TestPostgresMigrationResumesAndActivatesWithCurrentRows(t *testing.T) {
	dsn := os.Getenv("PLATFORM_TEST_DATABASE")
	if dsn == "" {
		t.Skip("PLATFORM_TEST_DATABASE not set")
	}
	ctx := context.Background()
	id := fmt.Sprintf("migration-current-activation-%d", time.Now().UnixNano())
	j, err := OpenJournal(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer forgetJournal(t, j, id)
	if _, err := j.Entries(ctx, id, 0); err != nil {
		t.Fatal(err)
	}
	live, plan, _ := migrationFixture(t, id, []string{"One", "Two", "Three"}, j)
	live.attachJournal(ctx, j)
	if err := live.migrateRuntimeState(ctx, j); err != nil {
		t.Fatal(err)
	}
	builder := memberOf(t, live, "builder")
	review, err := live.ReviewMigrationPlan(builder, plan, txnAt)
	if err != nil {
		t.Fatal(err)
	}
	decide(t, live, "builder", construction.ID, "build.migration.start", platform.MigrationRunType, "M", map[string]any{"plan": plan, "review": review.ID, "reviewedAt": review.At}, txnAt)
	first := decide(t, live, "builder", construction.ID, "build.migration.advance", platform.MigrationRunType, "M", map[string]any{}, txnAt)
	if _, err := live.checkpointRuntime(ctx, j, "migration-current-activation"); err != nil {
		t.Fatal(err)
	}
	restored := builderTenant(t, id)
	deployment := Deployment{SnapshotEvery: 1}
	if err := deployment.restoreTenants(ctx, j, "migration-current-activation", []*Tenant{restored}, map[string]int64{}, map[string]bool{}); err != nil || restored.quarantined() {
		t.Fatalf("migration startup: %v, fault=%+v", err, restored.fault.Load())
	}
	position := j.Position(id)
	builder = memberOf(t, restored, "builder")
	again, refusal := restored.Submit(builder, first.GetSubmission(), txnAt.Add(time.Minute))
	if refusal != nil || again.GetChangeId() != first.GetChangeId() || j.Position(id) != position {
		t.Fatal("migration restart repeated the accepted batch", refusal)
	}
	decide(t, restored, "builder", construction.ID, "build.migration.advance", platform.MigrationRunType, "M", map[string]any{}, txnAt.Add(time.Minute))
	run, refusal := restored.RecordOf(builder, platform.MigrationRunType, "M", txnAt)
	if refusal != nil || run.Record.(platform.MigrationRun).State != "complete" {
		t.Fatal("migration did not finish", refusal)
	}
	histories := map[string][]json.RawMessage{}
	readHistory := func(t *testing.T, rowID string) []json.RawMessage {
		t.Helper()
		var count int
		if err := j.Pool().QueryRow(ctx, `select history_count from runtime_records where tenant=$1 and type='build.asset' and id=$2`, id, rowID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		history, err := j.StateHistory(ctx, id, "build.asset", rowID, 0, count)
		if err != nil || len(history) == 0 {
			t.Fatal("original record history is unavailable", rowID, err)
		}
		return history
	}
	for _, rowID := range []string{"A", "B", "C"} {
		histories[rowID] = readHistory(t, rowID)
	}
	if _, err := restored.ActivateRelease(builder, plan.To, "target-after-restart", txnAt.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	activatedPosition := j.Position(id)
	assertTarget := func(t *testing.T, tenant *Tenant) {
		t.Helper()
		if tenant.releases.active != plan.To || tenant.records.readView != nil {
			t.Fatal("activation lost the target or retained its SQL read scope")
		}
		member := memberOf(t, tenant, "builder")
		for index, rowID := range []string{"A", "B", "C"} {
			row, refusal := tenant.RecordOf(member, "build.asset", rowID, txnAt)
			if refusal != nil || row.Record == nil {
				t.Fatal("activated migrated record is unreadable", rowID, refusal)
			}
			var value struct {
				Name    string `json:"name"`
				Enabled bool   `json:"enabled"`
			}
			if err := json.Unmarshal(platform.Raw(row.Record), &value); err != nil || !value.Enabled || value.Name != []string{"One", "Two", "Three"}[index] {
				t.Fatalf("activation changed migrated target values: %s: %+v, %v", rowID, value, err)
			}
			if !reflect.DeepEqual(readHistory(t, rowID), histories[rowID]) {
				t.Fatal("activation or recovery rewrote original record history", rowID)
			}
		}
		for typ, entity := range tenant.records.types {
			if entity.rows.Stored() != 0 {
				t.Fatal("activation or recovery retained SQL rows in the root store", typ, entity.rows.Stored())
			}
		}
		again, err := tenant.ActivateRelease(member, plan.To, "target-after-restart", txnAt.Add(time.Hour))
		if err != nil || again != plan.To || j.Position(id) != activatedPosition {
			t.Fatal("same-key target activation was repeated", again, err)
		}
	}
	assertTarget(t, restored)
	if _, err := restored.checkpointRuntime(ctx, j, "migration-current-activation"); err != nil {
		t.Fatal(err)
	}
	for _, every := range []int64{1, 0} {
		t.Run(fmt.Sprintf("restore-snapshot-every-%d", every), func(t *testing.T) {
			restarted := builderTenant(t, id)
			recovery := Deployment{SnapshotEvery: every}
			if err := recovery.restoreTenants(ctx, j, "migration-current-activation", []*Tenant{restarted}, map[string]int64{}, map[string]bool{}); err != nil || restarted.quarantined() {
				t.Fatalf("activated target restoration: %v, fault=%+v", err, restarted.fault.Load())
			}
			assertTarget(t, restarted)
		})
	}
}
