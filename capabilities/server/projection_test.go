package platformserver

import (
	"context"
	"fmt"
	"os"
	"platformserver/platform"
	"testing"
	"time"

	"platformserver/studio/build"
)

func TestProjectionChangedIDsAndHistorySuffix(t *testing.T) {
	tenant := stockTenant(t)
	at := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	for _, id := range []string{"I1", "I2"} {
		decide(t, tenant, "ana", "stock", "stock.item.create", "stock.item", id, map[string]any{"name": id, "qty": 1}, at)
	}
	projection := &Projection{tenant: tenant}
	rows, changes, ends, err := projection.rows(context.Background(), "stock.item", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || len(changes) != 2 {
		t.Fatalf("initial projection: %d rows, %d changes", len(rows), len(changes))
	}
	projection.recordHistory(ends)
	decide(t, tenant, "ana", "stock", "stock.item.edit", "stock.item", "I2", map[string]any{"qty": 2}, at.Add(time.Second))
	ids := map[string]bool{"I2": true, "absent": true}
	rows, changes, ends, err = projection.rows(context.Background(), "stock.item", ids)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0][0] != "I2" || len(changes) != 1 || changes[0][0] != "I2" || changes[0][1] != 1 {
		t.Fatalf("changed projection sent unchanged rows or old history: %v %v", rows, changes)
	}
	// A failed database batch does not advance history; its retry sends the same
	// suffix. Only the successful batch records the confirmed prefix.
	_, retried, _, err := projection.rows(context.Background(), "stock.item", ids)
	if err != nil {
		t.Fatal(err)
	}
	if len(retried) != 1 || retried[0][1] != 1 {
		t.Fatal("reading a batch consumed history before a successful write")
	}
	projection.recordHistory(ends)
	_, changes, _, err = projection.rows(context.Background(), "stock.item", ids)
	if err != nil {
		t.Fatal(err)
	}
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
	tenant := composeTenant(t, id, []platform.Seat{seatOf("ana", "stock:clerk", "build:builder")}, newStock(id), build.New(id))
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
	// A common host starts before builders install business objects. Their
	// reader tables must appear without restarting or replacing old tables.
	typ := publishObject(t, tenant, "ana", "REPORT-O", map[string]any{"name": "reportprobe", "title": "Report probe", "fields": []map[string]any{
		{"name": "note", "title": "Note", "type": "text"},
		{"name": "flag", "title": "Flag", "type": "boolean"},
		{"name": "count", "title": "Count", "type": "integer"},
		{"name": "ratio", "title": "Ratio", "type": "decimal"},
		{"name": "moment", "title": "Moment", "type": "datetime"},
		{"name": "cost", "title": "Cost", "type": "money"},
	}}, at)
	decide(t, tenant, "ana", "build", typ+".create", typ, "R1", map[string]any{"note": "published live"}, at)
	decide(t, tenant, "ana", "build", typ+".create", typ, "R2", map[string]any{"note": "explicit values", "flag": false, "count": 0, "ratio": 0, "moment": at.Format(time.RFC3339), "cost": map[string]any{"amount": 0, "currency": "EUR"}}, at)
	if err := projection.Flush(ctx); err != nil {
		t.Fatal("live object projection", err)
	}
	var note string
	if err := j.Pool().QueryRow(ctx, "select note from "+q(schema, tableOf(typ))+" where id='R1'").Scan(&note); err != nil || note != "published live" {
		t.Fatalf("new business table: %q %v", note, err)
	}
	for _, rowID := range []string{"R1", "R2"} {
		var flag *bool
		var count, amount *int64
		var ratio *float64
		var moment *time.Time
		var currency *string
		if err := j.Pool().QueryRow(ctx, "select flag,count,ratio,moment,cost_amount,cost_currency from "+q(schema, tableOf(typ))+" where id=$1", rowID).Scan(&flag, &count, &ratio, &moment, &amount, &currency); err != nil {
			t.Fatal(err)
		}
		if rowID == "R1" {
			if flag != nil || count != nil || ratio != nil || moment != nil || amount != nil || currency != nil {
				t.Fatal("absent scalar values must remain SQL NULL")
			}
		} else if flag == nil || *flag || count == nil || *count != 0 || ratio == nil || *ratio != 0 || moment == nil || !moment.Equal(at) || amount == nil || *amount != 0 || currency == nil || *currency != "EUR" {
			t.Fatal("explicit false, zero, timestamp or currency was lost")
		}
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
	_, changes, _, err := projection.rows(ctx, "stock.item", map[string]bool{"I1": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0][1] != 2 {
		t.Fatalf("SQL-backed projection did not read its suffix: %v", changes)
	}
	if err := projection.Flush(ctx); err != nil {
		t.Fatal(err)
	}
}
