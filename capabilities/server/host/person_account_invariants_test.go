package host

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"platformserver/kernel/protocol"
	"platformserver/platform"
	"platformserver/services/actions"
	"platformserver/services/ontology"
	build "platformserver/services/resources/authoring"
	enterprisemodel "platformserver/services/security/organization"
	"platformserver/services/work"
)

func TestInvariantK37AnAccountSignsInAsOnePersonAndActsThroughTheirOrganisation(t *testing.T) {
	const tenant, typ = "invariant-k37", "invariantproject.request"
	seed := platform.OrgSeed{
		Units:       []platform.Unit{{ID: "TEAM", Name: "Team", Kind: "team", Calendar: "office"}},
		Calendars:   []platform.Calendar{{ID: "office", Name: "Office", Workdays: []int{1, 2, 3, 4, 5}}},
		Memberships: []platform.Membership{{Party: "member:emp-1", Unit: "TEAM", Role: "member"}},
	}
	people := map[string]platform.Member{}
	var seats []protocol.Seat
	for id, roles := range map[string]map[string]string{
		"emp-1": {build.ID: ontology.UserRole}, "lead-1": {build.ID: ontology.UserRole}, "lead-2": {build.ID: ontology.UserRole},
		"org-admin": {enterprisemodel.ID: enterprisemodel.Admin},
	} {
		people[id] = platform.Member{ID: id, Tenant: tenant, Roles: roles}
		seats = append(seats, protocol.Seat{Subjects: []string{id}, Member: people[id]})
	}
	slices.SortFunc(seats, func(a, b protocol.Seat) int { return strings.Compare(a.Member.ID, b.Member.ID) })
	apps := func() []platform.Service {
		return []platform.Service{work.New(tenant, actions.NewAuthority), enterprisemodel.New(tenant, actions.NewAuthority, seed)}
	}
	live := newInvariantTenantWithApps(t, tenant, apps(), seats...)
	org := func(schema, target, id string, payload map[string]any) *platform.Error {
		t.Helper()
		sub := live.submission(schema, target, id, payload)
		sub.PrincipalId, sub.Authority = "org-admin", enterprisemodel.ID
		_, refusal := live.Submit(people["org-admin"], sub, invariantClock)
		return refusal
	}
	must := func(refusal *platform.Error) {
		t.Helper()
		if refusal != nil {
			t.Fatal(refusal.Message)
		}
	}
	must(org(enterprisemodel.SchemaElementAdd, enterprisemodel.ElementType, "P-LI", map[string]any{"stereotype": enterprisemodel.Person, "name": "Li Na"}))
	must(org(enterprisemodel.SchemaElementAdd, enterprisemodel.ElementType, "P-WU", map[string]any{"stereotype": enterprisemodel.Person, "name": "Wu Lei"}))
	relate := func(id, stereotype, source, target, role string) *platform.Error {
		return org(enterprisemodel.SchemaRelationshipAdd, enterprisemodel.RelationshipType, id, map[string]any{"stereotype": stereotype, "source": source, "target": target, "role": role})
	}
	must(relate("R-MGR", enterprisemodel.Membership, "P-LI", "TEAM", platform.ManagerRole))
	must(relate("R-SIGN", enterprisemodel.SignsInAs, "member:lead-1", "P-LI", ""))
	for name, refusal := range map[string]*platform.Error{
		"a second person for one account": relate("R-X1", enterprisemodel.SignsInAs, "member:lead-1", "P-WU", ""),
		"a second account for one person": relate("R-X2", enterprisemodel.SignsInAs, "member:lead-2", "P-LI", ""),
		"a person as the source":          relate("R-X3", enterprisemodel.SignsInAs, "P-WU", "P-LI", ""),
		"an account signing in as a team": relate("R-X4", enterprisemodel.SignsInAs, "member:lead-2", "TEAM", ""),
	} {
		if refusal == nil {
			t.Fatalf("%s must be refused", name)
		}
	}

	submitted := &platform.Expr{Kind: "choice", Lit: &platform.ExprLiteral{Value: platform.Raw("approved")}}
	live.accept(t, "build.object.create", build.ObjectType, "REQUEST", map[string]any{"name": "request", "title": "Request",
		"fields": []ontology.Field{{Name: "title", Title: "Title", Type: "text"}},
		"states": []ontology.State{{Name: "draft", Title: "Draft"}, {Name: "pending", Title: "Pending"}, {Name: "approved", Title: "Approved"}, {Name: "rejected", Title: "Rejected"}}})
	submit := ontology.Action{Name: "submit", Title: "Submit", From: []string{"draft"},
		Approval: &ontology.ActionApproval{Pending: "pending", Rejected: "rejected", Levels: []ontology.ApproverLevel{{Title: "Manager", Manager: true}}},
		Transaction: &platform.BusinessTxn{Subject: typ, Steps: []platform.TxnStep{{
			Name: "approve", Kind: "update", Target: platform.TxnRef{Type: typ, ID: "$subject"}, Changes: []platform.TxnSet{{Field: "state", Expr: submitted}},
		}}}}
	live.accept(t, "build.actiontype.create", build.ActionTypeType, "SUBMIT", map[string]any{"name": typ + ".submit", "object": typ, "definition": submit})
	live.accept(t, "build.actiontype.publish", build.ActionTypeType, "SUBMIT", map[string]any{})
	live.accept(t, "build.object.publish", build.ObjectType, "REQUEST", map[string]any{})

	as := func(who, schema, authority, target, id string, payload any) (string, *platform.Error) {
		t.Helper()
		sub := live.submission(schema, target, id, payload)
		sub.PrincipalId, sub.Authority = who, authority
		_, refusal := live.Submit(people[who], sub, invariantClock)
		return build.ID + "." + sub.GetIdempotencyKey(), refusal
	}
	if _, refusal := as("emp-1", typ+".create", build.ID, typ, "Q1", map[string]any{"title": "Leave"}); refusal != nil {
		t.Fatal(refusal.Message)
	}
	approval, refusal := as("emp-1", typ+".submit", build.ID, typ, "Q1", map[string]any{})
	if refusal != nil {
		t.Fatal(refusal.Message)
	}
	view, refusal := live.RecordOf(t.Context(), people["emp-1"], work.ApprovalType, approval, invariantClock)
	if refusal != nil {
		t.Fatal(refusal.Message)
	}
	var request work.ApprovalRequest
	_ = json.Unmarshal(platform.Raw(view.Record), &request)
	if len(request.Levels) != 1 || !slices.Equal(request.Levels[0].Approvers, []string{"lead-1"}) {
		t.Fatalf("the manager is reached through the person the account signs in as: %+v", request.Levels)
	}
	if _, refusal := as("lead-2", work.ApprovalType+".approve", work.ID, work.ApprovalType, approval, map[string]any{}); refusal == nil {
		t.Fatal("an account without the person must not approve")
	}
	if _, refusal := as("lead-1", work.ApprovalType+".approve", work.ID, work.ApprovalType, approval, map[string]any{}); refusal != nil {
		t.Fatalf("the account signed in as the manager approves: %s", refusal.Message)
	}
	before := live.recordImage(t)
	stored := make([]Entry, len(live.entries))
	raw, _ := json.Marshal(live.entries)
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	fresh := newInvariantTenantReplayWithApps(t, tenant, apps(), seats...)
	if err := fresh.recoverEntries(stored); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if after := fresh.recordImage(t); !reflect.DeepEqual(before, after) {
		t.Fatal("replay must rebuild the same organisation and approval")
	}
	if got := fresh.App(enterprisemodel.ID).(*enterprisemodel.Enterprise).Holders("", "TEAM", platform.ManagerRole, "2026-11-23"); !slices.Equal(got, []string{"lead-1"}) {
		t.Fatalf("after replay the team manager is still reached through the person, got %v", got)
	}
}
