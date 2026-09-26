package hospitality

import (
	"encoding/json"
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

// ADR-0028 11e: a manager on holiday delegates her approvals; the delegate
// approves a leave for her and the request says so. An export honours field security.
func TestDelegationAndExport(t *testing.T) {
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
	n := 0
	do := func(who, app, schema, typ, id string, payload any) string {
		n++
		raw, _ := json.Marshal(payload)
		_, err := tn.Submit(member(who), &pb.Submission{TenantId: "hotel-a", PrincipalId: who, Authority: app, IdempotencyKey: schema + id + string(rune('a'+n)),
			Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, now)
		if err != nil {
			return err.Code.String()
		}
		return "ok"
	}
	w := &world{t: t}
	w.expect(do("max", work.ID, work.SchemaDelegate, work.DelegationType, "D-1", map[string]string{"to": "kim", "start": "2026-10-10", "end": "2026-10-20"}), "ok")
	w.expect(do("ana", hcm.ID, hcm.SchemaCreate, hcm.LeaveType, "L-1", map[string]string{"kind": "vacation", "from": "2026-11-02", "until": "2026-11-03"}), "ok")
	w.expect(do("ana", hcm.ID, hcm.SchemaSubmit, hcm.LeaveType, "L-1", struct{}{}), "ok")
	out, _ := tn.Read(member("kim"), "inbox")
	if tasks := out.([]work.WorkTask); len(tasks) != 1 || !strings.Contains(tasks[0].Title, "hcm.leave/L-1") {
		t.Fatalf("the delegate's inbox %+v", tasks)
	}
	requests, _ := tn.Read(member("ana"), "requests")
	id := requests.([]work.ApprovalRequest)[0].ID
	w.expect(do("kim", work.ID, "work.approval.approve", work.ApprovalType, id, struct{}{}), "ok")
	requests, _ = tn.Read(member("ana"), "requests")
	r := requests.([]work.ApprovalRequest)[0]
	if r.State != "approved" || r.Levels[0].Approved[0] != "max" || r.Levels[0].DecidedBy["max"] != "kim" {
		t.Fatalf("request %+v", r)
	}

	// The margin never leaves in the salesperson's export, and does in the manager's.
	w.expect(do("ana", crm.ID, crm.SchemaAccount, crm.AccountType, "ACME", map[string]string{"name": "Acme", "kind": "company"}), "ok")
	w.expect(do("ana", crm.ID, crm.SchemaOpen, crm.OpportunityType, "O-1", map[string]string{"account": "ACME", "title": "Offsite"}), "ok")
	w.expect(do("max", crm.ID, "crm.opportunity.edit", crm.OpportunityType, "O-1", map[string]any{"margin": 31.5}), "ok")
	sales, _ := tn.Export(member("ana"), crm.OpportunityType, platform.Query{}, now)
	manager, _ := tn.Export(member("max"), crm.OpportunityType, platform.Query{}, now)
	if strings.Contains(string(sales), "margin") || strings.Contains(string(sales), "31.5") || !strings.Contains(string(manager), "31.5") {
		t.Fatalf("exports:\n%s\n%s", sales, manager)
	}
	platformserver.CheckReplay(t, tn, journal, build)
}
