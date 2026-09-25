package hr

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver"
	"platformserver/platform"
)

// ADR-0017 D7: leave requests approved along the organisation. Alice works in
// sales; Bob manages sales; Carol heads the company above it; Dave works in
// HR; a bot is an AI agent with Alice's rights in HR.
func TestLeaveApprovals(t *testing.T) {
	var journal []platformserver.Entry
	build := func() *platformserver.Tenant {
		seat := func(id string, roles map[string]string, agent bool) platformserver.Seat {
			return platformserver.Seat{Subjects: []string{id}, Member: platform.Member{ID: id, Roles: roles, Agent: agent}}
		}
		org := platformserver.NewOrganization("t", platform.OrgSeed{Structures: []platform.Structure{{ID: Structure, Name: "Management", Kind: "management"}},
			Units: []platform.Unit{{ID: "acme", Name: "Acme", Kind: "company"}, {ID: "sales", Name: "Sales", Kind: "department"}},
			Edges: []platform.Edge{{Structure: Structure, Unit: "sales", Parent: "acme"}},
			Memberships: []platform.Membership{{Party: "member:alice", Unit: "sales", Role: "employee"}, {Party: "member:bob", Unit: "sales", Role: "manager"},
				{Party: "member:carol", Unit: "acme", Role: "head"}, {Party: "member:bot", Unit: "sales", Role: "manager"}}})
		tn, err := platformserver.NewTenant("t", platformserver.NewConsole("t",
			seat("alice", map[string]string{"hr": Employee}, false), seat("bob", map[string]string{"hr": Employee}, false),
			seat("carol", map[string]string{"hr": Employee}, false), seat("dave", map[string]string{"hr": HR}, false),
			seat("bot", map[string]string{"hr": Employee}, true)), org, platformserver.NewWork("t"), New("t"))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := build()
	tn.Record = func(e platformserver.Entry) { journal = append(journal, e) }
	member := func(id string) platform.Member {
		m := map[string]platform.Member{}
		for _, x := range []struct {
			id, role string
			agent    bool
		}{{"alice", Employee, false}, {"bob", Employee, false}, {"carol", Employee, false}, {"dave", HR, false}, {"bot", Employee, true}} {
			m[x.id] = platform.Member{ID: x.id, Tenant: "t", Roles: map[string]string{"hr": x.role}, Agent: x.agent}
		}
		return m[id]
	}
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	keys := 0
	do := func(who, schema, typ, id string, payload any) string {
		keys++
		raw, _ := json.Marshal(payload)
		authority := "hr"
		if typ != LeaveType {
			authority = platformserver.WorkApp
		}
		r, err := tn.Submit(member(who), &pb.Submission{TenantId: "t", PrincipalId: who, Authority: authority, IdempotencyKey: fmt.Sprint("k", keys),
			Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, now)
		if err != nil {
			return err.Error()
		}
		return r.GetSubmission().GetSchema().GetName()
	}
	leave := func(id string) Leave {
		page, _ := tn.Records(member("dave"), LeaveType, platform.Query{Domain: json.RawMessage(`[["id","=","` + id + `"]]`)}, now)
		return page.Records[0].(Leave)
	}
	request := func(leave string) platformserver.ApprovalRequest {
		out, _ := tn.Read(member("alice"), "requests")
		for _, r := range out.([]platformserver.ApprovalRequest) {
			if r.Target == LeaveType+"/"+leave {
				return r
			}
		}
		return platformserver.ApprovalRequest{}
	}
	inbox := func(who string) []string {
		out, _ := tn.Read(member(who), "inbox")
		var titles []string
		for _, task := range out.([]platformserver.WorkTask) {
			titles = append(titles, task.Title)
		}
		return titles
	}
	draft := func(id, from, until string) {
		t.Helper()
		if got := do("alice", SchemaCreate, LeaveType, id, map[string]string{"kind": "vacation", "from": from, "until": until}); got != SchemaCreate {
			t.Fatalf("draft %s: %s", id, got)
		}
	}
	expect := func(what, got, want string) {
		t.Helper()
		if got != want {
			t.Fatalf("%s: %s, want %s", what, got, want)
		}
	}

	// Three days: the manager approves, and the leave is approved by that decision.
	draft("L1", "2026-10-12", "2026-10-14")
	expect("submit", do("alice", SchemaSubmit, LeaveType, "L1", struct{}{}), platformserver.SchemaRequest) // held, not applied
	expect("while pending", leave("L1").State, "draft")
	a1 := request("L1")
	expect("levels", fmt.Sprint(len(a1.Levels), a1.Levels[0].Approvers), "1 [bob bot]")
	expect("bob's inbox", fmt.Sprint(inbox("bob")), "[Approve: Submit for approval hr.leave/L1]")
	expect("the agent approves", do("bot", "work.approval.approve", platformserver.ApprovalType, a1.ID, struct{}{}), "ERROR_CODE_POLICY_DENIED")
	expect("carol approves", do("carol", "work.approval.approve", platformserver.ApprovalType, a1.ID, struct{}{}), "ERROR_CODE_POLICY_DENIED")
	expect("bob approves", do("bob", "work.approval.approve", platformserver.ApprovalType, a1.ID, struct{}{}), "work.approval.approve")
	expect("after approval", leave("L1").State+" "+request("L1").State, "approved approved")
	expect("bob's inbox after", fmt.Sprint(inbox("bob")), "[]")

	// Seven days: the manager, then the department head.
	draft("L2", "2026-11-02", "2026-11-08")
	do("alice", SchemaSubmit, LeaveType, "L2", struct{}{})
	a2 := request("L2")
	expect("two levels", fmt.Sprint(len(a2.Levels), a2.Levels[1].Approvers), "2 [carol]")
	do("bob", "work.approval.approve", platformserver.ApprovalType, a2.ID, struct{}{})
	expect("level two", fmt.Sprintf("%d %s %v", request("L2").Level, leave("L2").State, inbox("carol")), "1 draft [Approve: Submit for approval hr.leave/L2]")
	do("carol", "work.approval.approve", platformserver.ApprovalType, a2.ID, struct{}{})
	expect("after both", leave("L2").State, "approved")

	// The rules decide when the last approver agrees (D3): a leave canceled
	// meanwhile is refused, and the request says why.
	draft("L3", "2026-12-01", "2026-12-02")
	do("alice", SchemaSubmit, LeaveType, "L3", struct{}{})
	do("alice", SchemaCancel, LeaveType, "L3", struct{}{})
	do("bob", "work.approval.approve", platformserver.ApprovalType, request("L3").ID, struct{}{})
	expect("stale", request("L3").State+" "+request("L3").Outcome+" "+leave("L3").State, "refused ERROR_CODE_CONFLICT canceled")

	// Rejected and withdrawn requests leave the leave a draft.
	draft("L4", "2026-12-10", "2026-12-10")
	do("alice", SchemaSubmit, LeaveType, "L4", struct{}{})
	do("bob", "work.approval.reject", platformserver.ApprovalType, request("L4").ID, map[string]string{"note": "busy week"})
	expect("rejected", request("L4").State+" "+leave("L4").State, "rejected draft")
	draft("L5", "2026-12-20", "2026-12-20")
	do("alice", SchemaSubmit, LeaveType, "L5", struct{}{})
	expect("bob withdraws", do("bob", "work.approval.withdraw", platformserver.ApprovalType, request("L5").ID, struct{}{}), "ERROR_CODE_POLICY_DENIED")
	do("alice", "work.approval.withdraw", platformserver.ApprovalType, request("L5").ID, struct{}{})
	expect("withdrawn", request("L5").State+" "+fmt.Sprint(inbox("bob")), "withdrawn []")

	// A request is checked when made: HR cannot submit Alice's leave for her.
	draft("L6", "2027-01-04", "2027-01-05")
	expect("probe", do("dave", SchemaSubmit, LeaveType, "L6", struct{}{}), "ERROR_CODE_POLICY_DENIED")

	// An overdue task tells its approvers once.
	do("alice", SchemaSubmit, LeaveType, "L6", struct{}{})
	tn.Work(now.Add(72 * time.Hour))
	notes, _ := tn.Read(member("bob"), "notifications")
	overdue := slices.ContainsFunc(notes.([]platform.Notification), func(n platform.Notification) bool { return n.Title == "Overdue: Approve: Submit for approval hr.leave/L6" })
	expect("overdue", fmt.Sprint(overdue), "true")

	platformserver.CheckReplay(t, tn, journal, build)
}
