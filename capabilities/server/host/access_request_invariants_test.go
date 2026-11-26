package host

import (
	"platformserver/kernel/protocol"
	"platformserver/kernel/transport"
	"platformserver/services/actions"
	"platformserver/services/ontology"
	"slices"
	"testing"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	build "platformserver/services/resources/authoring"
	"platformserver/services/security"
	controlowner "platformserver/services/security/control"
	enterprisemodel "platformserver/services/security/organization"
	"platformserver/services/work"
)

func TestInvariantAccessRequestAppliesGrantOnApproval(t *testing.T) {
	owner := protocol.Seat{Subjects: []string{"owner"}, Member: platform.Member{ID: "owner"}}
	requester := protocol.Seat{Subjects: []string{"requester"}, Member: platform.Member{ID: "requester"}}
	administrator := protocol.Seat{Subjects: []string{"request-admin"}, Member: platform.Member{ID: "request-admin", Roles: map[string]string{"platform": "admin"}}}
	tn := newInvariantTenantWithApps(t, "access-request", []platform.Service{work.New("access-request", actions.NewAuthority)}, owner, requester, administrator)
	if action, ok := tn.App("platform").Manifest().Actions.Action(controlowner.SchemaAccessRequest); !ok || action.Target != controlowner.AccessRequestType {
		t.Fatal("the access request action is not declared for its target")
	}
	tn.accept(t, protocol.AppType+".create", protocol.AppType, "PROJ", map[string]any{"name": "accessproject", "title": "Access project"})
	save := tn.submission(protocol.SchemaProjectSave, protocol.ProjectType, "PROJ", map[string]any{
		"title": "Access project", "approvals": 1,
		"members": []platform.ProjectMember{{Member: "owner", Role: protocol.ProjectOwnerRole}},
	})
	save.Authority = "platform"
	if _, refusal := tn.Submit(tn.member(t), save, invariantClock); refusal != nil {
		t.Fatalf("project save: %s: %s", refusal.Code, refusal.Message)
	}
	submit := func(memberID, requestID, schema string, payload any) *pb.ChangeRecord {
		t.Helper()
		member, ok := tn.Member(memberID)
		if !ok {
			t.Fatalf("member %s is not seated", memberID)
		}
		sub := tn.submission(schema, controlowner.AccessRequestType, requestID, payload)
		sub.Authority, sub.PrincipalId = "platform", memberID
		record, refusal := tn.Submit(member, sub, invariantClock)
		if refusal != nil {
			t.Fatalf("%s %s: %s: %s", memberID, schema, refusal.Code, refusal.Message)
		}
		return record
	}
	submit("requester", "REQ1", controlowner.SchemaAccessRequest, map[string]any{
		"scope": "project", "project": "PROJ", "role": protocol.ProjectViewerRole, "reason": "Inspect the line",
	})
	requesterMember, _ := tn.Member("requester")
	requests, refusal := tn.read(requesterMember, "access-requests")
	if refusal != nil || len(requests.([]controlowner.AccessRequest)) != 1 || requests.([]controlowner.AccessRequest)[0].State != "pending" {
		t.Fatalf("pending request read: %+v: %v", requests, refusal)
	}
	if requests.([]controlowner.AccessRequest)[0].CanDecide {
		t.Fatal("the requester was told they may decide their own request")
	}
	ownerMember, _ := tn.Member("owner")
	inbox, refusal := tn.read(ownerMember, "inbox")
	if refusal != nil || !slices.ContainsFunc(inbox.([]protocol.WorkTask), func(task protocol.WorkTask) bool {
		return task.Key == "access:REQ1" && task.Title != "" && !task.Withheld
	}) {
		t.Fatalf("approver inbox has no access task: %+v: %v", inbox, refusal)
	}
	approverRequests, refusal := tn.read(ownerMember, "access-requests")
	if refusal != nil || len(approverRequests.([]controlowner.AccessRequest)) != 1 || !approverRequests.([]controlowner.AccessRequest)[0].CanDecide {
		t.Fatalf("approver request read: %+v: %v", approverRequests, refusal)
	}
	self := tn.submission(controlowner.SchemaAccessApprove, controlowner.AccessRequestType, "REQ1", map[string]any{})
	self.Authority, self.PrincipalId = "platform", "requester"
	if _, refusal := tn.Submit(requesterMember, self, invariantClock); refusal == nil {
		t.Fatal("the requester approved their own request")
	}
	submit("owner", "REQ1", controlowner.SchemaAccessApprove, map[string]any{"comment": "Approved for the shift"})
	inbox, refusal = tn.read(ownerMember, "inbox")
	if refusal != nil || slices.ContainsFunc(inbox.([]protocol.WorkTask), func(task protocol.WorkTask) bool {
		return task.Key == "access:REQ1"
	}) {
		t.Fatalf("approved access task stayed open: %+v: %v", inbox, refusal)
	}
	current, ok := tn.Member("requester")
	if !ok || tn.App("platform").(protocol.ProjectAccess).ProjectRole("requester", "PROJ") != protocol.ProjectViewerRole || !current.Holds(build.ID, protocol.ProjectViewerRole) {
		t.Fatalf("approval did not grant project viewer access: member=%+v role=%q", current, tn.App("platform").(protocol.ProjectAccess).ProjectRole("requester", "PROJ"))
	}
	requests, _ = tn.read(requesterMember, "access-requests")
	if got := requests.([]controlowner.AccessRequest)[0]; got.State != "approved" || got.DecidedBy != "owner" || got.Granted == "" {
		t.Fatalf("approved request was not retained: %+v", got)
	}
	again := tn.submission(controlowner.SchemaAccessRequest, controlowner.AccessRequestType, "REQ1b", map[string]any{
		"scope": "project", "project": "PROJ", "role": protocol.ProjectViewerRole, "reason": "Request again",
	})
	again.Authority, again.PrincipalId = "platform", "requester"
	if _, refusal := tn.Submit(requesterMember, again, invariantClock); refusal == nil {
		t.Fatal("a granted project role could be requested again")
	}

	submit("requester", "REQ2", controlowner.SchemaAccessRequest, map[string]any{
		"scope": "role", "app": "platform", "role": "auditor", "reason": "Audit the tenant",
	})
	submit("request-admin", "REQ2", controlowner.SchemaAccessApprove, map[string]any{})
	current, _ = tn.Member("requester")
	if !current.Holds("platform", "auditor") {
		t.Fatalf("role request did not grant the business role: %+v", current.Roles)
	}
	requests, _ = tn.read(requesterMember, "access-requests")
	if len(requests.([]controlowner.AccessRequest)) != 2 || !slices.ContainsFunc(requests.([]controlowner.AccessRequest), func(item controlowner.AccessRequest) bool {
		return item.ID == "REQ2" && item.State == "approved" && item.DecidedBy == "request-admin"
	}) {
		t.Fatalf("role request was not retained: %+v", requests)
	}
}

func TestInvariantRebacUnitScopedRequestUntilOverrideAndLifecycle(t *testing.T) {
	seed := platform.OrgSeed{
		Structures: []platform.Structure{{ID: "reporting", Name: "Reporting", Kind: "reporting"}},
		Units:      []platform.Unit{{ID: "WEST", Name: "West Region", Kind: "department"}},
		Edges:      []platform.Edge{{Structure: "reporting", Unit: "WEST"}},
	}
	requester := protocol.Seat{Subjects: []string{"rep-1"}, Member: platform.Member{ID: "rep-1"}}
	colleague := protocol.Seat{Subjects: []string{"rep-2"}, Member: platform.Member{ID: "rep-2"}}
	adminSeat := protocol.Seat{Subjects: []string{"admin-1"}, Member: platform.Member{ID: "admin-1", Roles: map[string]string{"platform": "admin"}}}
	tn := newInvariantTenantWithApps(
		t,
		"rebac-lifecycle",
		[]platform.Service{
			work.New("rebac-lifecycle", actions.NewAuthority),
			enterprisemodel.New("rebac-lifecycle", actions.NewAuthority, seed),
		},
		requester, colleague, adminSeat,
	)

	tn.accept(t, protocol.AppType+".create", protocol.AppType, "CRM-PROJ", map[string]any{"name": "crm", "title": "CRM"})
	tn.accept(t, "build.object.create", build.ObjectType, "DEAL", map[string]any{
		"project": "CRM-PROJ",
		"name":    "deal",
		"title":   "Deal",
		"scope":   map[string]any{"owner": "owner", "unit": "unit", "structure": "reporting"},
		"fields": []ontology.Field{
			{Name: "title", Title: "Title", Type: "text", Required: true},
			{Name: "owner", Title: "Owner", Type: "member"},
			{Name: "unit", Title: "Unit", Type: "reference", Ref: enterprisemodel.ElementType},
			{Name: "margin", Title: "Margin", Type: "decimal", Read: []string{"salesmanager"}, Write: []string{"salesmanager"}},
		},
		"access": []ontology.Access{
			{Role: "salesrep", Read: "own", Create: true, Edit: true},
			{Role: "salesmanager", Read: "below", Create: true, Edit: true, Archive: true},
		},
	})
	tn.accept(t, "build.object.publish", build.ObjectType, "DEAL", nil)

	infos := tn.Apps()
	if !slices.ContainsFunc(infos, func(info transport.AppInfo) bool {
		return info.ID == "crm" && slices.Contains(info.Roles, "salesmanager") && slices.Contains(info.Roles, "salesrep")
	}) {
		t.Fatalf("declarative space crm was not surfaced in AppInfos: %+v", infos)
	}

	adminMember, _ := tn.Member("admin-1")
	customSub := tn.submission(protocol.SchemaRoleSave, protocol.RoleType, "deal-creator", map[string]any{
		"app":     "crm",
		"title":   "Deal creator",
		"actions": []string{"crm.deal.create"},
	})
	customSub.Authority, customSub.PrincipalId = "platform", "admin-1"
	if _, refusal := tn.Submit(adminMember, customSub, invariantClock); refusal != nil {
		t.Fatalf("save declarative custom role: %s: %s", refusal.Code, refusal.Message)
	}

	rawPerms, refusal := tn.read(adminMember, "permissions")
	if refusal != nil {
		t.Fatalf("read permissions: %v", refusal)
	}
	perms := rawPerms.([]security.AppPermissions)
	crmPermsIdx := slices.IndexFunc(perms, func(entry security.AppPermissions) bool { return entry.App == "crm" })
	if crmPermsIdx < 0 {
		t.Fatalf("crm missing from permissions report: %+v", perms)
	}
	crmPerms := perms[crmPermsIdx]
	if !slices.ContainsFunc(crmPerms.Roles, func(r security.RolePermissions) bool { return r.Role == "deal-creator" }) {
		t.Fatalf("custom role deal-creator missing from crm permissions: %+v", crmPerms.Roles)
	}
	if len(crmPerms.Objects) != 1 || crmPerms.Objects[0].RoleScopes["salesmanager"] != "below" || len(crmPerms.Objects[0].Fields) == 0 {
		t.Fatalf("crm object ReBAC scope matrix incomplete: %+v", crmPerms.Objects)
	}

	rep1, _ := tn.Member("rep-1")
	reqSub := tn.submission(controlowner.SchemaAccessRequest, controlowner.AccessRequestType, "REQ-UNIT-1", map[string]any{
		"scope":     "role",
		"app":       "crm",
		"role":      "salesmanager",
		"unit":      "WEST",
		"structure": "reporting",
		"until":     "2027-06-01",
		"reason":    "Cover West pipeline review",
	})
	reqSub.Authority, reqSub.PrincipalId = "platform", "rep-1"
	if _, refusal := tn.Submit(rep1, reqSub, invariantClock); refusal != nil {
		t.Fatalf("unit-scoped access request refused: %s: %s", refusal.Code, refusal.Message)
	}

	approveSub := tn.submission(controlowner.SchemaAccessApprove, controlowner.AccessRequestType, "REQ-UNIT-1", map[string]any{
		"comment": "Shortened to end of quarter",
		"until":   "2027-05-01",
	})
	approveSub.Authority, approveSub.PrincipalId = "platform", "admin-1"
	if _, refusal := tn.Submit(adminMember, approveSub, invariantClock); refusal != nil {
		t.Fatalf("approve with until override refused: %s: %s", refusal.Code, refusal.Message)
	}

	rep1After, _ := tn.Member("rep-1")
	if !slices.ContainsFunc(rep1After.Grants, func(g platform.Grant) bool {
		return g.App == "crm" && g.Role == "salesmanager" && g.Unit == "WEST" && g.Structure == "reporting" && g.Until == "2027-05-01"
	}) {
		t.Fatalf("approved unit-scoped grant did not preserve unit and overridden until: %+v", rep1After.Grants)
	}

	delSub := tn.submission(protocol.SchemaDelegate, controlowner.MemberType, "rep-2", map[string]any{
		"app":    "crm",
		"until":  "2027-04-15",
		"reason": "Vacation handover",
	})
	delSub.Authority, delSub.PrincipalId = "platform", "rep-1"
	if _, refusal := tn.Submit(rep1After, delSub, invariantClock); refusal != nil {
		t.Fatalf("delegate declarative role refused: %s: %s", refusal.Code, refusal.Message)
	}
	rep2After, _ := tn.Member("rep-2")
	if !rep2After.Holds("crm", "salesmanager") {
		t.Fatalf("colleague did not receive delegated crm role: %+v", rep2After.Grants)
	}

	undelSub := tn.submission(protocol.SchemaUndelegate, controlowner.MemberType, "rep-2", map[string]any{"app": "crm"})
	undelSub.Authority, undelSub.PrincipalId = "platform", "rep-1"
	if _, refusal := tn.Submit(rep1After, undelSub, invariantClock); refusal != nil {
		t.Fatalf("undelegate refused: %s: %s", refusal.Code, refusal.Message)
	}
	rep2Ended, _ := tn.Member("rep-2")
	if rep2Ended.Holds("crm", "salesmanager") {
		t.Fatalf("undelegate did not end delegated role: %+v", rep2Ended.Grants)
	}

	schedSub := tn.submission(controlowner.SchemaGrant, controlowner.MemberType, "rep-2", map[string]any{
		"app":    "crm",
		"role":   "deal-creator",
		"from":   "2027-07-01",
		"until":  "2027-08-01",
		"reason": "Summer launch",
	})
	schedSub.Authority, schedSub.PrincipalId = "platform", "admin-1"
	if _, refusal := tn.Submit(adminMember, schedSub, invariantClock); refusal != nil {
		t.Fatalf("scheduled grant refused: %s: %s", refusal.Code, refusal.Message)
	}
	rawMembers, refusal := tn.read(adminMember, "members")
	if refusal != nil {
		t.Fatalf("read members: %v", refusal)
	}
	memberViews := rawMembers.([]protocol.MemberView)
	rep2ViewIdx := slices.IndexFunc(memberViews, func(v protocol.MemberView) bool { return v.ID == "rep-2" })
	if rep2ViewIdx < 0 || len(memberViews[rep2ViewIdx].ScheduledGrants) != 1 || memberViews[rep2ViewIdx].ScheduledGrants[0].Role != "deal-creator" {
		t.Fatalf("scheduled grant missing from MemberView: %+v", memberViews[rep2ViewIdx])
	}
}
