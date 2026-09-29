package hospitality

import (
	"testing"

	"crm"
	"platformserver/apps/build"
)

// A builder's object refers to a CRM account and names the relation seen from
// there (ADR-0040 21b D1). The account's page lists the related records under
// that name, only for a member who may read both ends; another member of CRM
// alone does not learn the builder's records exist.
func TestNamedRelationFromAnotherApp(t *testing.T) {
	w := newWorld(t, hotelProvider)
	w.setup()
	visit := build.TypeOf("visit")
	w.expect(w.submit("manager", build.ID, build.ObjectType+".create", build.ObjectType, "O-VISIT", "rel-1",
		map[string]any{"name": "visit", "title": "Visit", "plural": "Visits", "fields": []map[string]any{
			{"name": "guest", "title": "Guest", "type": "text"},
			{"name": "account", "title": "Account", "type": "reference", "ref": crm.AccountType, "inverse": "visits"}}}), "ok")
	w.expect(w.submit("manager", build.ID, build.SchemaPublish, build.ObjectType, "O-VISIT", "rel-2", map[string]any{}), "ok")
	w.expect(w.submit("manager", build.ID, visit+".create", visit, "V-1", "rel-3",
		map[string]any{"guest": "Ada", "account": "ACME"}), "ok")

	related := func(who string) (string, int) {
		t.Helper()
		view, err := w.tenant.RecordOf(w.members[who], crm.AccountType, "ACME", t0)
		if err != nil {
			t.Fatalf("account as %s: %v", who, err)
		}
		for _, r := range view.Related {
			if r.Type == visit {
				return r.Relation, r.Total
			}
		}
		return "", 0
	}
	if name, total := related("manager"); name != "visits" || total != 1 {
		t.Fatalf("manager sees relation %q with %d records", name, total)
	}
	if name, total := related("sales"); name != "" || total != 0 {
		t.Fatalf("a CRM-only member learned of builder records: %q %d", name, total)
	}

	// A relation name must be a name, and only a reference has one.
	w.expect(w.submit("manager", build.ID, build.ObjectType+".create", build.ObjectType, "O-BAD", "rel-4",
		map[string]any{"name": "badrel", "title": "Bad", "fields": []map[string]any{
			{"name": "guest", "title": "Guest", "type": "text", "inverse": "visits"}}}),
		"ERROR_CODE_INVALID_ARGUMENT")
}
