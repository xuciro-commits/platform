package hospitality

import (
	"encoding/json"
	"slices"
	"testing"

	"crm"

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

	// A page the hotel composes over the CRM's own object, with the CRM's own
	// action on it (ADR-0034, #132): the platform's page, the CRM's execution.
	w.expect(w.submit("manager", build.ID, build.PageType+".create", build.PageType, "P-OFF", "lf-5",
		map[string]any{"name": "offsites", "title": "Group offsites", "object": crm.OpportunityType,
			"list": []string{"title", "stage"}, "detail": []string{"title", "account", "stage"}, "actions": []string{crm.SchemaClose}}), "ok")
	w.expect(w.submit("manager", build.ID, build.SchemaRelease, build.PageType, "P-OFF", "lf-6", map[string]any{}), "ok")
	composed := func(who string) *platform.Page {
		for _, d := range w.tenant.Definitions(w.members[who]) {
			if d.Ref.Kind == platform.AssetPage && d.Ref.Name == "offsites" {
				return d.Page
			}
		}
		return nil
	}
	if page := composed("manager"); page == nil || page.Object.Name != crm.OpportunityType || len(page.Actions) != 1 {
		t.Fatalf("the composed page: %+v", page)
	}
	// The desk holds no role in the CRM, so the page's object is not theirs to
	// read and the page is not offered to them.
	if page := composed("desk"); page != nil {
		t.Errorf("a page over an object the member may not read was offered: %+v", page)
	}
}
