package host

import (
	"encoding/json"
	"testing"

	"platformserver/kernel/protocol"
	"platformserver/platform"
	"platformserver/services/actions"
	"platformserver/services/ontology"
	build "platformserver/services/resources/authoring"
	enterprisemodel "platformserver/services/security/organization"
	"platformserver/services/work"
)

func TestInvariantK31ManagerApprovalGoesToTheRequestersOwnUnitManager(t *testing.T) {
	const tenant, typ = "invariant-k31", "invariantproject.request"
	seed := platform.OrgSeed{
		Structures: []platform.Structure{{ID: "line", Name: "Line", Kind: "line"}},
		Units:      []platform.Unit{{ID: "DEPT", Name: "Department", Kind: "department"}, {ID: "TEAM", Name: "Team", Kind: "team"}},
		Edges:      []platform.Edge{{Structure: "line", Unit: "TEAM", Parent: "DEPT"}},
		Memberships: []platform.Membership{
			{Party: "member:emp-1", Unit: "TEAM", Role: "member"}, {Party: "member:lead-1", Unit: "TEAM", Role: platform.ManagerRole},
			{Party: "member:emp-2", Unit: "DEPT", Role: "member"}, {Party: "member:head-1", Unit: "DEPT", Role: platform.ManagerRole},
		},
	}
	var seats []protocol.Seat
	people := map[string]platform.Member{}
	for _, id := range []string{"emp-1", "lead-1", "emp-2", "head-1", "hr-1"} {
		role := ontology.UserRole
		if id == "hr-1" {
			role = ontology.BuilderRole
		}
		m := platform.Member{ID: id, Tenant: tenant, Roles: map[string]string{build.ID: role}}
		people[id] = m
		seats = append(seats, protocol.Seat{Subjects: []string{id}, Member: m})
	}
	live := newInvariantTenantWithApps(t, tenant, []platform.Service{work.New(tenant, actions.NewAuthority), enterprisemodel.New(tenant, actions.NewAuthority, seed)}, seats...)
	submitted := &platform.Expr{Kind: "choice", Lit: &platform.ExprLiteral{Value: platform.Raw("approved")}}
	live.accept(t, "build.object.create", build.ObjectType, "REQUEST", map[string]any{"name": "request", "title": "Request",
		"fields": []ontology.Field{{Name: "title", Title: "Title", Type: "text"}},
		"states": []ontology.State{{Name: "draft", Title: "Draft"}, {Name: "pending", Title: "Pending"}, {Name: "approved", Title: "Approved"}, {Name: "rejected", Title: "Rejected"}}})
	submit := ontology.Action{Name: "submit", Title: "Submit", From: []string{"draft"},
		Approval: &ontology.ActionApproval{Pending: "pending", Rejected: "rejected", Levels: []ontology.ApproverLevel{{Title: "Manager", Manager: true, Role: ontology.BuilderRole}}},
		Transaction: &platform.BusinessTxn{Subject: typ, Steps: []platform.TxnStep{{
			Name: "approve", Kind: "update", Target: platform.TxnRef{Type: typ, ID: "$subject"}, Changes: []platform.TxnSet{{Field: "state", Expr: submitted}},
		}}}}
	live.accept(t, "build.actiontype.create", build.ActionTypeType, "SUBMIT", map[string]any{"name": typ + ".submit", "object": typ, "definition": submit})
	live.accept(t, "build.actiontype.publish", build.ActionTypeType, "SUBMIT", map[string]any{})
	live.accept(t, "build.object.publish", build.ObjectType, "REQUEST", map[string]any{})

	keys := map[string]string{}
	as := func(who, schema, authority, target, id string, payload any) error {
		t.Helper()
		sub := live.submission(schema, target, id, payload)
		sub.PrincipalId, sub.Authority = who, authority
		keys[id] = build.ID + "." + sub.GetIdempotencyKey()
		if _, refusal := live.Submit(people[who], sub, invariantClock); refusal != nil {
			return refusal
		}
		return nil
	}
	requesters := map[string]string{}
	approvals := map[string]string{}
	approvers := func(id string) []string {
		t.Helper()
		view, refusal := live.RecordOf(t.Context(), people[requesters[id]], work.ApprovalType, approvals[id], invariantClock)
		if refusal != nil {
			t.Fatalf("approval %s: %s", id, refusal.Message)
		}
		var request work.ApprovalRequest
		_ = json.Unmarshal(platform.Raw(view.Record), &request)
		if len(request.Levels) != 1 {
			t.Fatalf("approval %s levels: %+v", id, request.Levels)
		}
		return request.Levels[0].Approvers
	}
	same := func(got []string, want ...string) bool {
		if len(got) != len(want) {
			return false
		}
		for i := range got {
			if got[i] != want[i] {
				return false
			}
		}
		return true
	}
	state := func(id string) string {
		t.Helper()
		view, refusal := live.RecordOf(t.Context(), people["hr-1"], typ, id, invariantClock)
		if refusal != nil {
			t.Fatal(refusal.Message)
		}
		var rec struct{ State string }
		_ = json.Unmarshal(platform.Raw(view.Record), &rec)
		return rec.State
	}
	for who, id := range map[string]string{"emp-1": "R1", "emp-2": "R2", "lead-1": "R3"} {
		if err := as(who, typ+".create", build.ID, typ, id, map[string]any{"title": who}); err != nil {
			t.Fatalf("%s create: %v", who, err)
		}
		if err := as(who, typ+".submit", build.ID, typ, id, map[string]any{}); err != nil {
			t.Fatalf("%s submit: %v", who, err)
		}
		requesters[id], approvals[id] = who, keys[id]
	}
	if got := approvers("R1"); !same(got, "builder", "hr-1", "lead-1") {
		t.Fatalf("a team member's request goes to the team manager and the role, got %v", got)
	}
	if got := approvers("R2"); !same(got, "builder", "head-1", "hr-1") {
		t.Fatalf("a department member's request skips the manager of a unit below, got %v", got)
	}
	if got := approvers("R3"); !same(got, "builder", "hr-1") {
		t.Fatalf("a manager does not approve their own request, got %v", got)
	}
	if err := as("head-1", work.ApprovalType+".approve", work.ID, work.ApprovalType, approvals["R1"], map[string]any{}); err == nil {
		t.Fatal("a manager of another unit must not approve")
	}
	if err := as("lead-1", work.ApprovalType+".approve", work.ID, work.ApprovalType, approvals["R1"], map[string]any{}); err != nil {
		t.Fatalf("the team manager approves: %v", err)
	}
	live.Work(invariantClock)
	if got := state("R1"); got != "approved" {
		t.Fatalf("the approved request runs its action, state %q", got)
	}
}
