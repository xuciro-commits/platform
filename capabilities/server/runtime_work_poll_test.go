package platformserver

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestPostgresIdleWorkKeepsAcceptedPositionAndResumesRealWork(t *testing.T) {
	dsn := os.Getenv("PLATFORM_TEST_DATABASE")
	if dsn == "" {
		t.Skip("PLATFORM_TEST_DATABASE not set")
	}
	ctx := context.Background()
	id := fmt.Sprintf("sql-idle-work-%d", time.Now().UnixNano())
	j, err := OpenJournal(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer forgetJournal(t, j, id)
	compose := func() *Tenant { return resultWorkTenant(t, id) }
	live := compose()
	worker := live.app("worker").(*resultWorker)
	worker.idle = true
	if _, err = j.Entries(ctx, id, 0); err != nil {
		t.Fatal(err)
	}
	live.attachJournal(ctx, j)
	if err = live.migrateRuntimeState(ctx, j); err != nil {
		t.Fatal(err)
	}
	before, position := snapshot(live), j.Position(id)
	at := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	started := time.Now()
	for i := range 10 {
		live.Work(at.Add(time.Duration(i) * time.Hour))
	}
	t.Logf("10 SQL-backed idle rounds took %s", time.Since(started))
	if live.quarantined() || j.Position(id) != position || snapshot(live) != before || len(live.works.All()) != 0 {
		t.Fatalf("idle polling changed accepted SQL/owner state: position %d/%d fault=%+v", j.Position(id), position, live.fault.Load())
	}
	if worker.polls != 10 {
		t.Fatalf("idle polls did not respect their declared interval: %d", worker.polls)
	}
	if _, err = live.checkpointRuntime(ctx, j, "sql-idle-work"); err != nil {
		t.Fatal(err)
	}
	worker.idle = false
	live.Work(at.Add(10 * time.Hour))
	if live.quarantined() || live.work.Jobs[0].Attempts != 1 || j.Position(id) <= position {
		t.Fatalf("real work after idle polling did not commit: %+v fault=%+v", live.work.Jobs[0], live.fault.Load())
	}
	if raw, err := j.StateRecord(ctx, id, "worker.document", "job-"+at.Add(10*time.Hour).Format(time.RFC3339Nano)); err != nil || len(raw) == 0 {
		t.Fatalf("accepted work row is unavailable from SQL: %v", err)
	}
	for typ, et := range live.records.types {
		if et.rows.Stored() != 0 {
			t.Fatalf("real work left resident SQL rows in %s", typ)
		}
	}
	restored := compose()
	deployment := Deployment{SnapshotEvery: 1}
	if err = deployment.restoreTenants(ctx, j, "sql-idle-work", []*Tenant{restored}, map[string]int64{}, map[string]bool{}); err != nil || restored.quarantined() {
		t.Fatalf("accepted suffix after idle checkpoint did not recover: %v fault=%+v", err, restored.fault.Load())
	}
	if snapshot(restored) != snapshot(live) || restored.app("worker").(*resultWorker).calls != 0 {
		t.Fatal("recovery changed accepted state or reran the worker")
	}
}
