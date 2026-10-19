package platformserver

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestProjectionChangedIDsAndHistorySuffix(t *testing.T) {
	tenant := stockTenant(t)
	at := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	for _, id := range []string{"I1", "I2"} {
		decide(t, tenant, "ana", "stock", "stock.item.create", "stock.item", id, map[string]any{"name": id, "qty": 1}, at)
	}
	projection := &Projection{tenant: tenant}
	rows, changes, ends := projection.rows("stock.item", nil)
	if len(rows) != 2 || len(changes) != 2 {
		t.Fatalf("initial projection: %d rows, %d changes", len(rows), len(changes))
	}
	projection.recordHistory(ends)
	decide(t, tenant, "ana", "stock", "stock.item.edit", "stock.item", "I2", map[string]any{"qty": 2}, at.Add(time.Second))
	ids := map[string]bool{"I2": true, "absent": true}
	rows, changes, ends = projection.rows("stock.item", ids)
	if len(rows) != 1 || rows[0][0] != "I2" || len(changes) != 1 || changes[0][0] != "I2" || changes[0][1] != 1 {
		t.Fatalf("changed projection sent unchanged rows or old history: %v %v", rows, changes)
	}
	// A failed database batch does not advance history; its retry sends the same
	// suffix. Only the successful batch records the confirmed prefix.
	_, retried, _ := projection.rows("stock.item", ids)
	if len(retried) != 1 || retried[0][1] != 1 {
		t.Fatal("reading a batch consumed history before a successful write")
	}
	projection.recordHistory(ends)
	_, changes, _ = projection.rows("stock.item", ids)
	if len(changes) != 0 {
		t.Fatal("successful history was sent again")
	}
}

func TestPostgresProjectionRetainsTablesAndAppendsHistory(t *testing.T) {
	dsn := os.Getenv("PLATFORM_TEST_DATABASE")
	if dsn == "" {
		t.Skip("PLATFORM_TEST_DATABASE not set")
	}
	ctx := context.Background()
	id := fmt.Sprintf("projection-%d", time.Now().UnixNano())
	j, err := OpenJournal(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer forgetJournal(t, j, id)
	schema := ProjectionSchema(id)
	defer func() {
		j.Pool().Exec(ctx, "drop schema if exists "+q(schema)+" cascade")
		j.Pool().Exec(ctx, "drop role if exists "+q(ReaderRole(id)))
	}()
	tenant := composeTenant(t, id, []Seat{seatOf("ana", "stock:clerk")}, newStock(id))
	if _, err := j.Entries(ctx, id, 0); err != nil {
		t.Fatal(err)
	}
	tenant.attachJournal(ctx, j)
	at := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	decide(t, tenant, "ana", "stock", "stock.item.create", "stock.item", "I1", map[string]any{"name": "one", "qty": 1}, at)
	if err := tenant.migrateRuntimeState(ctx, j); err != nil {
		t.Fatal(err)
	}
	projection, err := Project(ctx, j.Pool(), tenant)
	if err != nil {
		t.Fatal(err)
	}
	table, history := q(schema, tableOf("stock.item")), q(schema, tableOf("stock.item")+"_changes")
	var original uint32
	if err := j.Pool().QueryRow(ctx, "select $1::regclass::oid", table).Scan(&original); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Pool().Exec(ctx, "alter table "+table+" add column obsolete text"); err != nil {
		t.Fatal(err)
	}
	decide(t, tenant, "ana", "stock", "stock.item.edit", "stock.item", "I1", map[string]any{"qty": 2}, at.Add(time.Second))
	if err := projection.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	projection, err = Project(ctx, j.Pool(), tenant)
	if err != nil {
		t.Fatal(err)
	}
	var current uint32
	var n, qty int
	var obsolete bool
	if err := j.Pool().QueryRow(ctx, "select $1::regclass::oid", table).Scan(&current); err != nil || current != original {
		t.Fatalf("restart replaced the table: %d/%d %v", current, original, err)
	}
	if err := j.Pool().QueryRow(ctx, "select count(*) from "+history).Scan(&n); err != nil || n != 2 {
		t.Fatalf("history was duplicated or lost: %d %v", n, err)
	}
	if err := j.Pool().QueryRow(ctx, "select qty from "+table+" where id='I1'").Scan(&qty); err != nil || qty != 2 {
		t.Fatalf("current row differs: %d %v", qty, err)
	}
	if err := j.Pool().QueryRow(ctx, `select exists(select 1 from pg_attribute where attrelid=$1::regclass and attname='obsolete' and not attisdropped)`, table).Scan(&obsolete); err != nil || obsolete {
		t.Fatalf("removed field remains readable: %v %v", obsolete, err)
	}
	// The SQL-backed row can fetch precisely the new suffix, without loading
	// older record history again during each projection flush.
	decide(t, tenant, "ana", "stock", "stock.item.edit", "stock.item", "I1", map[string]any{"qty": 3}, at.Add(2*time.Second))
	_, changes, _ := projection.rows("stock.item", map[string]bool{"I1": true})
	if len(changes) != 1 || changes[0][1] != 2 {
		t.Fatalf("SQL-backed projection did not read its suffix: %v", changes)
	}
	if err := projection.Flush(ctx); err != nil {
		t.Fatal(err)
	}
}
