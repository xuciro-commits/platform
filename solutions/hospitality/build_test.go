package hospitality

import (
	"encoding/json"
	"slices"
	"testing"

	"platformserver/apps/build"
	"platformserver/platform"
)

// ADR-0034 in a solution that is already running: the hotel's manager defines an
// object the platform never heard of, publishes it, and the front desk keeps its
// records — in the same tenant as the CRM, the PMS and their flows, with nothing
// installed or restarted.
func TestTheHotelDefinesItsOwnObject(t *testing.T) {
	w := newWorld(t, hotelProvider)
	w.setup()
	fields := []map[string]any{
		{"name": "guest", "title": "Guest", "type": "text", "required": true, "search": true},
		{"name": "room", "title": "Room", "type": "text"},
		{"name": "left", "title": "Left behind", "type": "choice", "choices": "umbrella,charger,book"},
		{"name": "returned", "title": "Returned on", "type": "date"},
	}
	w.expect(w.submit("manager", build.ID, build.ObjectType+".create", build.ObjectType, "O-LOST", "lf-1",
		map[string]any{"name": "lostitem", "title": "Lost item", "plural": "Lost items", "description": "Something a guest left in a room.", "fields": fields}), "ok")
	// The front desk cannot define or publish; the manager publishes.
	if got := w.submit("desk", build.SchemaPublish, build.ObjectType, build.ObjectType, "O-LOST", "lf-2", map[string]any{}); got == "ok" {
		t.Error("the front desk published an object")
	}
	w.expect(w.submit("manager", build.ID, build.SchemaPublish, build.ObjectType, "O-LOST", "lf-3", map[string]any{}), "ok")

	lost := build.TypeOf("lostitem")
	w.expect(w.submit("desk", build.ID, lost+".create", lost, "L-1", "lf-4",
		map[string]any{"guest": "Ada Lovelace", "room": "101", "left": "umbrella"}), "ok")
	page, err := w.tenant.Records(w.members["desk"], lost, platform.Query{Search: "Ada"}, t0)
	if err != nil || len(page.Records) != 1 {
		t.Fatalf("the desk's own records: %+v, %v", page, err)
	}
	if record, _ := json.Marshal(page.Records[0]); !slices.Contains([]bool{true}, json.Valid(record)) {
		t.Errorf("the record does not encode: %s", record)
	}

	// It is one of the member's pages, like a page the platform's code declares.
	defs := w.tenant.Definitions(w.members["desk"])
	if !slices.ContainsFunc(defs, func(d platform.Definition) bool {
		return d.Ref.Kind == platform.AssetPage && d.Ref.App == build.ID && d.Ref.Name == "lostitem" && d.Source == "tenant"
	}) {
		t.Errorf("the defined object has no page for the desk: %+v", defs)
	}
	// The hotel's own apps are untouched: the CRM still works beside it.
	if n := len(w.reservations("manager")); n != 0 {
		t.Errorf("the tenant's other apps changed: %d reservations", n)
	}
}
