package platformserver

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"platformserver/platform"
)

// ADR-0019 D3: a type's table has the record's own columns, then its fields
// typed for SQL; the values are what a BI tool reads.
func TestProjectionColumns(t *testing.T) {
	info, err := platform.Describe("stock", platform.Entity{Type: "stock.item", Title: "Item", Model: Item{}}, func(reflect.Type) string { return "stock.bin" })
	if err != nil {
		t.Fatal(err)
	}
	var defs []string
	for _, c := range columns(info) {
		defs = append(defs, c.name+" "+c.sqlType)
	}
	want := "id text primary key, revision bigint, created_at timestamptz, created_by text, changed_at timestamptz, changed_by text, archived boolean, " +
		"name text, qty bigint, price_amount bigint, price_currency text, line text, owner text, kind text, bin text, tags text[], due date"
	if got := strings.Join(defs, ", "); got != want {
		t.Fatalf("columns\n%s\nwant\n%s", got, want)
	}
	at := time.Date(2026, 9, 25, 9, 0, 0, 0, time.UTC)
	item := Item{Record: platform.Record{ID: "I1", Revision: 2, Created: platform.Stamp{By: "ana", At: at}}, Name: "Bolt", Qty: 5,
		Price: platform.Money{Amount: 150, Currency: "EUR"}, Bin: "B1", Tags: []string{"m6"}, Due: "2026-10-01"}
	var values []string
	for _, c := range columns(info) {
		values = append(values, fmt.Sprint(c.value(reflect.ValueOf(&item).Elem())))
	}
	if got := strings.Join(values, "|"); got != "I1|2|2026-09-25 09:00:00 +0000 UTC|ana|<nil>||false|Bolt|5|150|EUR||||B1|[m6]|2026-10-01 00:00:00 +0000 UTC" {
		t.Fatalf("values %s", got)
	}
	if ProjectionSchema("hotel-a") != "tenant_hotel_a" || ReaderRole("plant-sz") != "tenant_plant_sz_reader" || tableOf("hotel.room-type") != "hotel_room_type" {
		t.Fatal("identifiers")
	}
}
