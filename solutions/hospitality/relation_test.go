package hospitality

import (
	"encoding/json"
	"strings"
	"testing"

	"crm"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver"
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

	related := func(who string, typ string) (string, int) {
		t.Helper()
		view, err := w.tenant.RecordOf(w.members[who], crm.AccountType, "ACME", t0)
		if err != nil {
			t.Fatalf("account as %s: %v", who, err)
		}
		for _, r := range view.Related {
			if r.Type == typ {
				return r.Relation, r.Total
			}
		}
		return "", 0
	}
	if name, total := related("manager", visit); name != "visits" || total != 1 {
		t.Fatalf("manager sees relation %q with %d records", name, total)
	}
	if name, total := related("sales", visit); name != "" || total != 0 {
		t.Fatalf("a CRM-only member learned of builder records: %q %d", name, total)
	}

	// An account's page lists CRM opportunities with Relation "opportunities"
	if name, total := related("sales", crm.OpportunityType); name != "opportunities" || total != 1 {
		t.Fatalf("sales sees opportunities relation %q with %d records", name, total)
	}

	// A relation name must be a name, and only a reference has one.
	w.expect(w.submit("manager", build.ID, build.ObjectType+".create", build.ObjectType, "O-BAD", "rel-4",
		map[string]any{"name": "badrel", "title": "Bad", "fields": []map[string]any{
			{"name": "guest", "title": "Guest", "type": "text", "inverse": "visits"}}}),
		"ERROR_CODE_INVALID_ARGUMENT")

	// An inverse with upper-case characters is refused with the "not lower-case letters and digits" message.
	rawUpper, _ := json.Marshal(map[string]any{"name": "upperrel", "title": "Upper", "fields": []map[string]any{
		{"name": "account", "title": "Account", "type": "reference", "ref": crm.AccountType, "inverse": "Visits"}}})
	_, kerrUpper := w.tenant.Submit(w.members["manager"], &pb.Submission{TenantId: "hotel-a", PrincipalId: w.members["manager"].ID,
		Authority: build.ID, Target: &pb.EntityRef{Type: build.ObjectType, Id: "O-UPPER"},
		Schema: &pb.SchemaRef{Name: build.ObjectType + ".create", Version: 1}, IdempotencyKey: "rel-5", Payload: rawUpper}, t0)
	if kerrUpper == nil || !strings.Contains(kerrUpper.Message, "not lower-case letters and digits") {
		t.Fatalf("expected refusal with 'not lower-case letters and digits', got: %+v", kerrUpper)
	}

	// CheckReplay-style check: replay w.journal into a fresh world and assert the same related total.
	platformserver.CheckReplay(t, w.tenant, w.journal, func() *platformserver.Tenant { return newWorld(t, hotelProvider).tenant })

	replayed := newWorld(t, hotelProvider)
	if err := replayed.tenant.Replay(w.journal); err != nil {
		t.Fatalf("replay: %v", err)
	}
	view, kerr := replayed.tenant.RecordOf(replayed.members["manager"], crm.AccountType, "ACME", t0)
	if kerr != nil {
		t.Fatalf("replayed account view: %v", kerr)
	}
	foundVisit, foundOpp := false, false
	for _, r := range view.Related {
		if r.Type == visit && r.Relation == "visits" && r.Total == 1 {
			foundVisit = true
		}
		if r.Type == crm.OpportunityType && r.Relation == "opportunities" && r.Total == 1 {
			foundOpp = true
		}
	}
	if !foundVisit || !foundOpp {
		t.Fatalf("replayed related records mismatch: %+v", view.Related)
	}
}
