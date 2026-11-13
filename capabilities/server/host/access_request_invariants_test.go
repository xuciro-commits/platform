package host

import (
	"slices"
	"testing"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	build "platformserver/services/resources/authoring"
	controlowner "platformserver/services/security/control"
	"platformserver/services/work"
)

func TestInvariantAccessRequestAppliesGrantOnApproval(t *testing.T) {
	owner := platform.Seat{Subjects: []string{"owner"}, Member: platform.Member{ID: "owner"}}
	requester := platform.Seat{Subjects: []string{"requester"}, Member: platform.Member{ID: "requester"}}
	administrator := platform.Seat{Subjects: []string{"request-admin"}, Member: platform.Member{ID: "request-admin", Roles: map[string]string{"platform": "admin"}}}
	tn := newInvariantTenantWithApps(t, "access-request", []platform.App{work.New("access-request")}, owner, requester, administrator)
	if action, ok := tn.App("platform").Manifest().Actions.Action(controlowner.SchemaAccessRequest); !ok || action.Target != controlowner.AccessRequestType {
		t.Fatal("the access request action is not declared for its target")
	}
	tn.accept(t, build.AppType+".create", build.AppType, "PROJ", map[string]any{"name": "accessproject", "title": "Access project"})
	save := tn.submission(platform.SchemaProjectSave, platform.ProjectType, "PROJ", map[string]any{
		"title": "Access project", "approvals": 1,
		"members": []platform.ProjectMember{{Member: "owner", Role: platform.ProjectOwnerRole}},
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
		"scope": "project", "project": "PROJ", "role": platform.ProjectViewerRole, "reason": "Inspect the line",
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
	if refusal != nil || !slices.ContainsFunc(inbox.([]platform.WorkTask), func(task platform.WorkTask) bool {
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
	if refusal != nil || slices.ContainsFunc(inbox.([]platform.WorkTask), func(task platform.WorkTask) bool {
		return task.Key == "access:REQ1"
	}) {
		t.Fatalf("approved access task stayed open: %+v: %v", inbox, refusal)
	}
	current, ok := tn.Member("requester")
	if !ok || tn.App("platform").(platform.ProjectAccess).ProjectRole("requester", "PROJ") != platform.ProjectViewerRole || !current.Holds(build.ID, platform.ProjectViewerRole) {
		t.Fatalf("approval did not grant project viewer access: member=%+v role=%q", current, tn.App("platform").(platform.ProjectAccess).ProjectRole("requester", "PROJ"))
	}
	requests, _ = tn.read(requesterMember, "access-requests")
	if got := requests.([]controlowner.AccessRequest)[0]; got.State != "approved" || got.DecidedBy != "owner" || got.Granted == "" {
		t.Fatalf("approved request was not retained: %+v", got)
	}
	again := tn.submission(controlowner.SchemaAccessRequest, controlowner.AccessRequestType, "REQ1b", map[string]any{
		"scope": "project", "project": "PROJ", "role": platform.ProjectViewerRole, "reason": "Request again",
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
