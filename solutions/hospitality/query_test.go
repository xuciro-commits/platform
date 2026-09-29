package hospitality

import (
	"strings"
	"testing"

	"crm"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/platform"
)

// CRM declares "open-opportunities" once (ADR-0040 21c). A member runs it for an
// account and sees only open ones they may read; a composed page lists it for
// the selected account; a query of another object or record is refused.
func TestNamedQuerySharedByPageAndMember(t *testing.T) {
	w := newWorld(t, hotelProvider)
	w.setup()
	w.expect(w.submit("sales", crm.ID, crm.SchemaOpen, crm.OpportunityType, "OPP-2", "q-1",
		map[string]string{"account": "ACME", "title": "Summer retreat"}), "ok")
	w.expect(w.submit("sales", crm.ID, crm.SchemaClose, crm.OpportunityType, "OPP-2", "q-2",
		map[string]string{"outcome": "lost"}), "ok")

	page, err := w.tenant.RunQuery(w.members["sales"], crm.ID, "open-opportunities", "ACME", t0)
	if err != nil || page.Total != 1 {
		t.Fatalf("open opportunities of ACME: %+v, %v", page, err)
	}
	if _, err := w.tenant.RunQuery(w.members["sales"], crm.ID, "open-opportunities", "", t0); err == nil {
		t.Fatal("a query that needs its record ran without one")
	}
	if _, err := w.tenant.RunQuery(w.members["desk"], crm.ID, "open-opportunities", "ACME", t0); err == nil {
		t.Fatal("a member without CRM read opportunities through a query")
	}
	found := false
	for _, d := range w.tenant.Definitions(w.members["sales"]) {
		found = found || d.Ref == platform.AssetRef{App: crm.ID, Kind: platform.AssetQuery, Name: "open-opportunities"}
	}
	if !found {
		t.Fatal("the query is not discoverable")
	}

	section := func(name, query string) map[string]any {
		return map[string]any{"name": name, "title": "Accounts", "object": crm.AccountType,
			"sections": []map[string]any{{"widget": "table", "fields": []string{"name"}},
				{"widget": "table", "object": crm.OpportunityType, "query": query, "fields": []string{"title"}}}}
	}
	w.expect(w.submit("manager", build.ID, build.PageType+".create", build.PageType, "PQ1", "q-3", section("acctqopen", "crm.open-opportunities")), "ok")
	if _, e := w.tenant.Submit(w.members["manager"], &pb.Submission{TenantId: "hotel-a", PrincipalId: w.members["manager"].ID, Authority: build.ID, Target: &pb.EntityRef{Type: build.PageType, Id: "PQ1"}, Schema: &pb.SchemaRef{Name: build.SchemaRelease, Version: 1}, IdempotencyKey: "q-4", Payload: []byte(`{}`)}, t0); e != nil {
		t.Fatalf("publish PQ1: %s", e.Message)
	}
	w.expect(w.submit("manager", build.ID, build.PageType+".create", build.PageType, "PQ2", "q-5", section("acctqmissing", "crm.missing")), "ok")
	_, refusal := w.tenant.Submit(w.members["manager"], &pb.Submission{TenantId: "hotel-a", PrincipalId: w.members["manager"].ID,
		Authority: build.ID, Target: &pb.EntityRef{Type: build.PageType, Id: "PQ2"},
		Schema: &pb.SchemaRef{Name: build.SchemaRelease, Version: 1}, IdempotencyKey: "q-6", Payload: []byte(`{}`)}, t0)
	if refusal == nil || !strings.Contains(refusal.Message, "no query") {
		t.Fatalf("an unknown query was published: %v", refusal)
	}
}
