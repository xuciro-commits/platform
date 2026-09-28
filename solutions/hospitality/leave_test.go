package hospitality

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"crm"
	"hcm"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver"
	"platformserver/apps/work"
	"platformserver/platform"
)

// ADR-0028 D7: an approval due in two working days of the requester's
// calendar. The office closes for National Day week and weekends; the front
// office works every day.
func TestWorkingDays(t *testing.T) {
	seat := func(id string, roles map[string]string, units ...platform.Membership) platformserver.Seat {
		return platformserver.Seat{Subjects: []string{id}, Member: platform.Member{ID: id, Roles: roles}, Units: units}
	}
	seats := []platformserver.Seat{
		seat("ana", map[string]string{"hcm": hcm.Employee}, platform.Membership{Unit: "sales-team", Role: "account executive", Primary: true}),
		seat("dan", map[string]string{"hcm": hcm.Employee}, platform.Membership{Unit: "front-office", Role: "receptionist", Primary: true}),
		seat("max", map[string]string{"hcm": hcm.HR}, platform.Membership{Unit: "hotel-a", Role: "manager"}),
	}
	tn, err := Compose("hotel-a", nil, seats...)
	if err != nil {
		t.Fatal(err)
	}
	wednesday := time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC)
	due := func(who, leave string) string {
		m := platform.Member{ID: who, Tenant: "hotel-a", Roles: map[string]string{"hcm": hcm.Employee}}
		submit := func(schema string, payload any) {
			raw, _ := json.Marshal(payload)
			if _, err := tn.Submit(m, &pb.Submission{TenantId: "hotel-a", PrincipalId: who, Authority: hcm.ID, IdempotencyKey: schema + leave,
				Target: &pb.EntityRef{Type: hcm.LeaveType, Id: leave}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, wednesday); err != nil {
				t.Fatalf("%s %s: %v", who, schema, err)
			}
		}
		submit(hcm.SchemaCreate, map[string]string{"kind": "vacation", "from": "2026-11-02", "until": "2026-11-03"})
		submit(hcm.SchemaSubmit, struct{}{})
		out, _ := tn.Read(m, "requests")
		for _, r := range out.([]work.ApprovalRequest) {
			if r.Target == hcm.LeaveType+"/"+leave && len(r.Levels) > 0 {
				return r.Levels[0].Due.Format(time.DateOnly)
			}
		}
		t.Fatalf("no approval for %s", leave)
		return ""
	}
	// Wednesday before National Day: Thursday to Wednesday are closed, then Thursday and Friday count.
	if got := due("ana", "L-1"); got != "2026-10-09" {
		t.Fatalf("the office's due day %s", got)
	}
	if got := due("dan", "L-2"); got != "2026-10-02" {
		t.Fatalf("the front office's due day %s", got)
	}
}

// ADR-0028 11b, 11e: field security across views, search, history, aggregates and exports;
// personal data auditing for HR; delegation of approvals when a manager is on leave.
func TestFieldSecurity(t *testing.T) {
	seat := func(id string, roles map[string]string, units ...platform.Membership) platformserver.Seat {
		return platformserver.Seat{Subjects: []string{id}, Member: platform.Member{ID: id, Roles: roles}, Units: units}
	}
	seats := []platformserver.Seat{
		seat("ana", map[string]string{"hcm": hcm.Employee, "crm": string(crm.Sales)}, platform.Membership{Unit: "sales-team", Role: "account executive", Primary: true}),
		seat("max", map[string]string{"hcm": hcm.HR, "crm": string(crm.Manager)}, platform.Membership{Unit: "hotel-a", Role: "manager"}),
		seat("kim", map[string]string{"hcm": hcm.Employee}, platform.Membership{Unit: "front-office", Role: "supervisor"}),
	}
	var journal []platformserver.Entry
	build := func() *platformserver.Tenant {
		tn, err := Compose("hotel-a", nil, seats...)
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := build()
	tn.Record = func(e platformserver.Entry) { journal = append(journal, e) }
	now := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	member := func(id string) platform.Member { m, _ := tn.Member(id); return m }
	ana, max := member("ana"), member("max")

	n := 0
	submit := func(m platform.Member, app, schema, typ, id string, payload any) string {
		n++
		raw, _ := json.Marshal(payload)
		_, err := tn.Submit(m, &pb.Submission{TenantId: "hotel-a", PrincipalId: m.ID, Authority: app, IdempotencyKey: "s" + string(rune('a'+n)),
			Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, now)
		if err != nil {
			return err.Code.String()
		}
		return "ok"
	}
	expect := func(got, want string) {
		t.Helper()
		if got != want {
			t.Fatalf("got %s, want %s", got, want)
		}
	}

	// 1. Approval delegation: Max delegates to Kim; Kim approves Ana's leave.
	expect(submit(max, work.ID, work.SchemaDelegate, work.DelegationType, "D-1", map[string]string{"to": "kim", "start": "2026-10-10", "end": "2026-10-20"}), "ok")
	expect(submit(ana, hcm.ID, hcm.SchemaCreate, hcm.LeaveType, "L-VAC", map[string]string{"kind": "vacation", "from": "2026-11-02", "until": "2026-11-03"}), "ok")
	expect(submit(ana, hcm.ID, hcm.SchemaSubmit, hcm.LeaveType, "L-VAC", struct{}{}), "ok")
	out, _ := tn.Read(member("kim"), "inbox")
	if tasks := out.([]work.WorkTask); len(tasks) != 1 || !strings.Contains(tasks[0].Title, "hcm.leave/L-VAC") {
		t.Fatalf("the delegate's inbox %+v", tasks)
	}
	requests, _ := tn.Read(ana, "requests")
	reqID := requests.([]work.ApprovalRequest)[0].ID
	expect(submit(member("kim"), work.ID, "work.approval.approve", work.ApprovalType, reqID, struct{}{}), "ok")
	requests, _ = tn.Read(ana, "requests")
	r := requests.([]work.ApprovalRequest)[0]
	if r.State != "approved" || r.Levels[0].Approved[0] != "max" || r.Levels[0].DecidedBy["max"] != "kim" {
		t.Fatalf("delegated request %+v", r)
	}

	// 2. Personal data: A sick leave's medical reason is HR's alone; reads are audited.
	expect(submit(ana, hcm.ID, hcm.SchemaCreate, hcm.LeaveType, "L-1", map[string]string{"kind": "sick", "from": "2026-10-01", "until": "2026-10-02", "health": "migraine"}), "ok")
	leave := func(m platform.Member) hcm.Leave {
		v, err := tn.RecordOf(m, hcm.LeaveType, "L-1", now)
		if err != nil {
			t.Fatalf("%s: %v", m.ID, err)
		}
		return v.Record.(hcm.Leave)
	}
	if got := leave(ana).Health; got != "" {
		t.Fatalf("the employee read the medical reason %q", got)
	}
	if got := leave(max).Health; got != "migraine" {
		t.Fatalf("HR reads %q", got)
	}
	v, _ := tn.RecordOf(ana, hcm.LeaveType, "L-1", now)
	for _, c := range v.History {
		if slices.ContainsFunc(c.Fields, func(f platformserver.FieldChange) bool { return f.Field == "health" }) {
			t.Fatal("the history shows the medical reason")
		}
	}
	if page, _ := tn.Records(ana, hcm.LeaveType, platform.Query{Search: "migraine"}, now); page.Total != 0 {
		t.Fatal("search found the leave by its medical reason")
	}
	if page, _ := tn.Records(max, hcm.LeaveType, platform.Query{Domain: json.RawMessage(`[["health","=","migraine"]]`)}, now); page.Total != 1 {
		t.Fatal("HR's filter misses it")
	}
	if _, err := tn.Records(ana, hcm.LeaveType, platform.Query{Domain: json.RawMessage(`[["health","=","migraine"]]`)}, now); err == nil {
		t.Fatal("a filter on the medical reason was accepted")
	}
	if _, err := tn.Aggregate(ana, hcm.LeaveType, platformserver.AggregateQuery{Groups: []string{"health"}}, now); err == nil {
		t.Fatal("grouping by the medical reason was accepted")
	}
	for _, e := range tn.Entities(ana) {
		if _, ok := e.Field("health"); e.Type == hcm.LeaveType && ok {
			t.Fatal("the employee's forms would offer the medical reason")
		}
	}
	reads := tn.PersonalReads()
	if len(reads) == 0 || reads[0].Member != "max" || reads[0].Type != hcm.LeaveType || !slices.Equal(reads[0].Fields, []string{"health"}) {
		t.Fatalf("personal reads %+v", reads)
	}
	if slices.ContainsFunc(reads, func(r platformserver.PersonalRead) bool { return r.Member == "ana" }) {
		t.Fatal("a read without personal fields was audited")
	}

	// 3. CRM Opportunity margin: Manager reads/edits and exports; salesperson neither sees, nor sets, nor exports.
	expect(submit(ana, crm.ID, crm.SchemaAccount, crm.AccountType, "ACME", map[string]string{"name": "Acme", "kind": "company"}), "ok")
	expect(submit(ana, crm.ID, crm.SchemaOpen, crm.OpportunityType, "O-1", map[string]string{"account": "ACME", "title": "Offsite"}), "ok")
	expect(submit(max, crm.ID, "crm.opportunity.edit", crm.OpportunityType, "O-1", map[string]any{"margin": 31.5}), "ok")
	opp := func(m platform.Member) crm.Opportunity {
		v, _ := tn.RecordOf(m, crm.OpportunityType, "O-1", now)
		return v.Record.(crm.Opportunity)
	}
	if opp(ana).Margin != 0 || opp(max).Margin != 31.5 {
		t.Fatalf("margins: salesperson %v, manager %v", opp(ana).Margin, opp(max).Margin)
	}
	expect(submit(ana, crm.ID, "crm.opportunity.edit", crm.OpportunityType, "O-1", map[string]any{"margin": 50}), "ERROR_CODE_POLICY_DENIED")

	// Export honours field security
	salesExport, _ := tn.Export(ana, crm.OpportunityType, platform.Query{}, now)
	managerExport, _ := tn.Export(max, crm.OpportunityType, platform.Query{}, now)
	if strings.Contains(string(salesExport), "margin") || strings.Contains(string(salesExport), "31.5") || !strings.Contains(string(managerExport), "31.5") {
		t.Fatalf("exports:\nsales: %s\nmanager: %s", salesExport, managerExport)
	}

	platformserver.CheckReplay(t, tn, journal, build)
}
