package hospitality

import (
	"strings"
	"testing"

	"crm"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
)

// A composed page over accounts lists opportunities through the named
// relation "opportunities" (ADR-0040 21b D3); a relation the related object
// does not declare, or on a widget that cannot follow one, is refused.
func TestComposedPageFollowsNamedRelation(t *testing.T) {
	w := newWorld(t, hotelProvider)
	w.setup()
	page := func(id, key, relation, widget string) string {
		return w.submit("manager", build.ID, build.PageType+".create", build.PageType, id, key,
			map[string]any{"name": strings.ToLower(id), "title": "Accounts", "object": crm.AccountType,
				"sections": []map[string]any{
					{"widget": "table", "fields": []string{"name"}},
					{"widget": widget, "object": crm.OpportunityType, "relation": relation, "fields": []string{"title"}},
				}})
	}
	publish := func(id, key string) string {
		_, err := w.tenant.Submit(w.members["manager"], &pb.Submission{TenantId: "hotel-a", PrincipalId: w.members["manager"].ID,
			Authority: build.ID, Target: &pb.EntityRef{Type: build.PageType, Id: id},
			Schema: &pb.SchemaRef{Name: build.SchemaRelease, Version: 1}, IdempotencyKey: key, Payload: []byte(`{}`)}, t0)
		if err != nil {
			return err.Code.String() + ": " + err.Message
		}
		return "ok"
	}
	w.expect(page("PGOOD", "rp-1", "opportunities", "table"), "ok")
	w.expect(publish("PGOOD", "rp-2"), "ok")

	w.expect(page("PBAD", "rp-3", "deals", "table"), "ok")
	if got := publish("PBAD", "rp-4"); !strings.Contains(got, `declares no relation "deals"`) {
		t.Fatalf("undeclared relation published: %s", got)
	}
	w.expect(page("PDETAIL", "rp-5", "opportunities", "detail"), "ok")
	if got := publish("PDETAIL", "rp-6"); !strings.Contains(got, "only a table, chart or metric follows a relation") {
		t.Fatalf("detail widget followed a relation: %s", got)
	}
}
