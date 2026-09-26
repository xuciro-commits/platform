package hospitality

import (
	"encoding/json"
	"slices"
	"testing"

	"crm"
	"hcm"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver"
	"platformserver/platform"
)

// ADR-0028 11b: a field's reach is its declaration, on every path out of the
// host. A sick leave's medical reason is HR's alone and its reads are audited;
// an opportunity's margin is the sales managers' alone.
func TestFieldSecurity(t *testing.T) {
	w := newWorld(t, hotelProvider)
	member := func(id string, roles map[string]string) platform.Member {
		return platform.Member{ID: id, Tenant: "hotel-a", Roles: roles}
	}
	ana := member("ana", map[string]string{"hcm": hcm.Employee, "crm": string(crm.Sales)})
	hr := member("hana", map[string]string{"hcm": hcm.HR, "crm": string(crm.Manager)})
	n := 0
	submit := func(m platform.Member, app, schema, typ, id string, payload any) string {
		n++
		raw, _ := json.Marshal(payload)
		_, err := w.tenant.Submit(m, &pb.Submission{TenantId: "hotel-a", PrincipalId: m.ID, Authority: app, IdempotencyKey: "s" + string(rune('a'+n)),
			Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, t0)
		if err != nil {
			return err.Code.String()
		}
		return "ok"
	}
	w.expect(submit(ana, hcm.ID, hcm.SchemaCreate, hcm.LeaveType, "L-1", map[string]string{"kind": "sick", "from": "2026-10-01", "until": "2026-10-02", "health": "migraine"}), "ok")
	leave := func(m platform.Member) hcm.Leave {
		v, err := w.tenant.RecordOf(m, hcm.LeaveType, "L-1", t0)
		if err != nil {
			t.Fatalf("%s: %v", m.ID, err)
		}
		return v.Record.(hcm.Leave)
	}
	// The employee sees her leave without its medical reason; HR sees it.
	if got := leave(ana).Health; got != "" {
		t.Fatalf("the employee read the medical reason %q", got)
	}
	if got := leave(hr).Health; got != "migraine" {
		t.Fatalf("HR reads %q", got)
	}
	// Nor through its history, search, filters, grouping or the type's fields.
	v, _ := w.tenant.RecordOf(ana, hcm.LeaveType, "L-1", t0)
	for _, c := range v.History {
		if slices.ContainsFunc(c.Fields, func(f platformserver.FieldChange) bool { return f.Field == "health" }) {
			t.Fatal("the history shows the medical reason")
		}
	}
	if page, _ := w.tenant.Records(ana, hcm.LeaveType, platform.Query{Search: "migraine"}, t0); page.Total != 0 {
		t.Fatal("search found the leave by its medical reason")
	}
	if page, _ := w.tenant.Records(hr, hcm.LeaveType, platform.Query{Domain: json.RawMessage(`[["health","=","migraine"]]`)}, t0); page.Total != 1 {
		t.Fatal("HR's filter misses it")
	}
	if _, err := w.tenant.Records(ana, hcm.LeaveType, platform.Query{Domain: json.RawMessage(`[["health","=","migraine"]]`)}, t0); err == nil {
		t.Fatal("a filter on the medical reason was accepted")
	}
	if _, err := w.tenant.Aggregate(ana, hcm.LeaveType, platformserver.AggregateQuery{Groups: []string{"health"}}, t0); err == nil {
		t.Fatal("grouping by the medical reason was accepted")
	}
	for _, e := range w.tenant.Entities(ana) {
		if _, ok := e.Field("health"); e.Type == hcm.LeaveType && ok {
			t.Fatal("the employee's forms would offer the medical reason")
		}
	}
	// HR's read is audited; the employee's, which showed no personal field, is not.
	reads := w.tenant.PersonalReads()
	if len(reads) == 0 || reads[0].Member != "hana" || reads[0].Type != hcm.LeaveType || !slices.Equal(reads[0].Fields, []string{"health"}) {
		t.Fatalf("personal reads %+v", reads)
	}
	if slices.ContainsFunc(reads, func(r platformserver.PersonalRead) bool { return r.Member == "ana" }) {
		t.Fatal("a read without personal fields was audited")
	}

	// The margin: a manager sets it; the salesperson owning the opportunity neither sees nor sets it.
	w.expect(submit(ana, crm.ID, crm.SchemaAccount, crm.AccountType, "ACME", map[string]string{"name": "Acme", "kind": "company"}), "ok")
	w.expect(submit(ana, crm.ID, crm.SchemaOpen, crm.OpportunityType, "O-1", map[string]string{"account": "ACME", "title": "Offsite"}), "ok")
	w.expect(submit(hr, crm.ID, "crm.opportunity.edit", crm.OpportunityType, "O-1", map[string]any{"margin": 31.5}), "ok")
	opp := func(m platform.Member) crm.Opportunity {
		v, _ := w.tenant.RecordOf(m, crm.OpportunityType, "O-1", t0)
		return v.Record.(crm.Opportunity)
	}
	if opp(ana).Margin != 0 || opp(hr).Margin != 31.5 {
		t.Fatalf("margins: salesperson %v, manager %v", opp(ana).Margin, opp(hr).Margin)
	}
	w.expect(submit(ana, crm.ID, "crm.opportunity.edit", crm.OpportunityType, "O-1", map[string]any{"margin": 50}), "ERROR_CODE_POLICY_DENIED")
	platformserver.CheckReplay(t, w.tenant, w.journal, func() *platformserver.Tenant { return newWorld(t, hotelProvider).tenant })
}
