package control

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

var requestableProjectRoles = []string{platform.ProjectViewerRole, platform.ProjectEditorRole, platform.ProjectDiscovererRole}

const (
	AccessRequestType    = "platform.access-request"
	SchemaAccessRequest  = "platform.access.request"
	SchemaAccessApprove  = "platform.access.approve"
	SchemaAccessReject   = "platform.access.reject"
	SchemaAccessWithdraw = "platform.access.withdraw"
)

type AccessRequest struct {
	platform.Record
	Requester string    `json:"requester"`
	Scope     string    `json:"scope"`
	Project   string    `json:"project,omitempty"`
	App       string    `json:"app,omitempty"`
	Role      string    `json:"role,omitempty"`
	Reason    string    `json:"reason"`
	Until     string    `json:"until,omitempty"`
	State     string    `json:"state"`
	Approvers []string  `json:"approvers"`
	DecidedBy string    `json:"decidedBy,omitempty"`
	DecidedAt time.Time `json:"decidedAt,omitzero"`
	Outcome   string    `json:"outcome,omitempty"`
	Granted   string    `json:"granted,omitempty"`
	CanDecide bool      `json:"canDecide,omitempty"`
}

func AccessRequestActions() []platform.Action {
	return []platform.Action{
		{Schema: SchemaAccessRequest, Target: AccessRequestType, Capability: "members", Title: "Request access",
			Description: "Ask the project's owners or the platform administrators for project or business-role access. Approval applies the grant immediately.",
			Payload: []platform.Field{
				{Name: "scope", Type: "string", Required: true, Choices: []string{"project", "role"}, Description: "project or role"},
				{Name: "project", Type: "string", Description: "The project for a project request"},
				{Name: "app", Type: "string", Description: "The app for a business-role request"},
				{Name: "role", Type: "string", Description: "The requested project or business role"},
				{Name: "reason", Type: "string", Required: true, Description: "Why you need this access"},
				{Name: "until", Type: "string", Description: "Access ends before this day (YYYY-MM-DD); project requests default to 30 days"},
			}, Roles: []string{platform.AnyMember}},
		{Schema: SchemaAccessApprove, Target: AccessRequestType, Capability: "members", Title: "Approve access request",
			Description: "Approve a pending access request and apply its grant in the same decision. Current project owners or platform administrators may approve; requesters cannot approve their own request.",
			Payload:     []platform.Field{{Name: "comment", Type: "string"}}, Roles: []string{platform.AnyMember}},
		{Schema: SchemaAccessReject, Target: AccessRequestType, Capability: "members", Title: "Reject access request",
			Description: "Reject a pending access request with an optional reason.",
			Payload:     []platform.Field{{Name: "comment", Type: "string"}}, Roles: []string{platform.AnyMember}},
		{Schema: SchemaAccessWithdraw, Target: AccessRequestType, Capability: "members", Title: "Withdraw access request",
			Description: "Withdraw your own pending access request.",
			Payload:     []platform.Field{}, Roles: []string{platform.AnyMember}},
	}
}

func (d *Console) AccessRequests(c platform.Caller) []AccessRequest {
	d.mu.Lock()
	defer d.mu.Unlock()
	all := c.Holds(PlatformApp, Admin) || c.Holds(PlatformApp, platform.Auditor)
	out := []AccessRequest{}
	for _, request := range d.accessRequests {
		canDecide := request.State == "pending" && request.Requester != c.ID && slices.Contains(d.accessApprovers(request), c.ID)
		if !all && request.Requester != c.ID && !canDecide {
			continue
		}
		copy := *request
		copy.Approvers = slices.Clone(request.Approvers)
		copy.CanDecide = canDecide
		out = append(out, copy)
	}
	slices.SortFunc(out, func(a, b AccessRequest) int {
		if !a.Created.At.Equal(b.Created.At) {
			return b.Created.At.Compare(a.Created.At)
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out
}

func (d *Console) decideAccessRequest(c platform.Caller, s *pb.Submission, now time.Time) (func(*pb.ChangeRecord), func(*pb.ChangeRecord) *kernel.Error, *kernel.Error) {
	invalid := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	id := s.GetTarget().GetId()
	if id == "" || d.host == nil || c.Agent || strings.HasPrefix(c.ID, "app:") || !c.Active() {
		return nil, nil, invalid
	}
	switch s.GetSchema().GetName() {
	case SchemaAccessRequest:
		var p struct {
			Scope, Project, App, Role, Reason, Until string
		}
		if json.Unmarshal(s.GetPayload(), &p) != nil {
			return nil, nil, invalid
		}
		if d.accessRequests[id] != nil {
			return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Access request {request} already exists", id)
		}
		reason := strings.TrimSpace(p.Reason)
		if reason == "" || len(reason) > 2000 {
			return nil, nil, invalid
		}
		today := now.UTC().Format(time.DateOnly)
		if p.Until != "" {
			if _, err := time.Parse(time.DateOnly, p.Until); err != nil || p.Until <= today {
				return nil, nil, invalid
			}
		}
		request := AccessRequest{Record: platform.Record{ID: id}, Requester: c.ID, Scope: p.Scope, Project: p.Project, App: p.App, Role: p.Role, Reason: reason, Until: p.Until, State: "pending"}
		switch p.Scope {
		case "project":
			project, found := d.project(p.Project)
			if !found || !slices.Contains(requestableProjectRoles, p.Role) {
				return nil, nil, invalid
			}
			if current := d.ConsolePolicies.ProjectRole(c.ID, p.Project); projectRoleRank(current) >= projectRoleRank(p.Role) {
				return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "You already have {role} access to {project}", current, project.Title)
			}
			if request.Until == "" {
				request.Until = now.UTC().AddDate(0, 0, 30).Format(time.DateOnly)
			}
			request.Approvers = d.projectApprovers(p.Project, c.ID)
			if len(request.Approvers) == 0 {
				return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "No active project owner can approve this request")
			}
		case "role":
			if p.App == "" || p.Role == "" || p.App == PlatformApp && p.Role == Admin {
				return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Platform administration is not requested through business access")
			}
			if p.Project != "" {
				return nil, nil, invalid
			}
			app := d.host.App(p.App)
			if app == nil || !slices.Contains(app.Manifest().AllRoles(), p.Role) {
				return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Application {app} does not declare role {role}", p.App, p.Role)
			}
			if c.Holds(p.App, p.Role) {
				return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "You already hold {role} in {app}", p.Role, p.App)
			}
			request.Approvers = d.platformApprovers(c.ID)
			if len(request.Approvers) == 0 {
				return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "No active platform administrator can approve this request")
			}
		default:
			return nil, nil, invalid
		}
		for _, existing := range d.accessRequests {
			if existing.State == "pending" && existing.Requester == request.Requester && existing.Scope == request.Scope &&
				existing.Project == request.Project && existing.App == request.App && existing.Role == request.Role {
				return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Access request {request} is already pending", existing.ID)
			}
		}
		request.Created, request.Changed = platform.Stamp{By: c.ID, At: now}, platform.Stamp{By: c.ID, At: now}
		apply := func(*pb.ChangeRecord) { d.accessRequests[id] = &request }
		after := func(r *pb.ChangeRecord) *kernel.Error {
			return c.Assign(r, platform.Assignment{
				Title: "Access request: " + accessRequestTarget(request),
				Body:  fmt.Sprintf("%s requests %s. Reason: %s. Until: %s.", request.Requester, accessRequestTarget(request), request.Reason, request.Until),
				Ref:   "read:access-requests",
				To:    approversAsRecipients(request.Approvers),
				Key:   "access:" + id,
			})
		}
		return apply, after, nil
	case SchemaAccessApprove, SchemaAccessReject, SchemaAccessWithdraw:
		request := d.accessRequests[id]
		if request == nil {
			return nil, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
		if request.State != "pending" {
			return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Access request {request} is {state}", id, request.State)
		}
		var p struct{ Comment string }
		_ = json.Unmarshal(s.GetPayload(), &p)
		next := *request
		next.Approvers = slices.Clone(request.Approvers)
		if s.GetSchema().GetName() == SchemaAccessWithdraw {
			if request.Requester != c.ID {
				return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Only the requester may withdraw this request")
			}
			next.State, next.DecidedBy, next.DecidedAt, next.Outcome = "withdrawn", c.ID, now, strings.TrimSpace(p.Comment)
			apply := func(*pb.ChangeRecord) { d.accessRequests[id] = &next }
			return apply, d.accessRequestClosed(c, next, now, request.Approvers), nil
		}
		if request.Requester == c.ID || !slices.Contains(d.accessApprovers(request), c.ID) {
			return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Only a current eligible approver may decide this request")
		}
		if request.Until != "" && request.Until <= now.UTC().Format(time.DateOnly) {
			return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Access request {request} has expired", id)
		}
		if s.GetSchema().GetName() == SchemaAccessReject {
			next.State, next.DecidedBy, next.DecidedAt, next.Outcome = "rejected", c.ID, now, strings.TrimSpace(p.Comment)
			apply := func(*pb.ChangeRecord) { d.accessRequests[id] = &next }
			return apply, d.accessRequestClosed(c, next, now, []string{request.Requester}), nil
		}
		grant, refusal := d.prepareAccessGrant(c, request)
		if refusal != nil {
			return nil, nil, refusal
		}
		next.State, next.DecidedBy, next.DecidedAt, next.Outcome = "approved", c.ID, now, strings.TrimSpace(p.Comment)
		next.Granted = accessRequestTarget(next)
		apply := func(r *pb.ChangeRecord) {
			if grant != nil {
				grant(r)
			}
			d.accessRequests[id] = &next
		}
		return apply, d.accessRequestClosed(c, next, now, []string{request.Requester}), nil
	}
	return nil, nil, invalid
}

func (d *Console) prepareAccessGrant(c platform.Caller, request *AccessRequest) (func(*pb.ChangeRecord), *kernel.Error) {
	switch request.Scope {
	case "project":
		project, found := d.project(request.Project)
		if !found {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The project is no longer available")
		}
		if projectRoleRank(d.ConsolePolicies.ProjectRole(request.Requester, request.Project)) >= projectRoleRank(request.Role) {
			return nil, nil
		}
		members := slices.Clone(project.Members)
		found = false
		for i := range members {
			if members[i].Member == request.Requester {
				members[i].Role, members[i].Until = request.Role, request.Until
				found = true
				break
			}
		}
		if !found {
			members = append(members, platform.ProjectMember{Member: request.Requester, Role: request.Role, Until: request.Until})
		}
		payload, err := json.Marshal(map[string]any{"title": project.Title, "approvals": max(1, project.Approvals), "members": members})
		if err != nil {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
		}
		ids := make([]string, 0, len(d.members))
		for member := range d.members {
			ids = append(ids, member)
		}
		s := &pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: PlatformApp, IdempotencyKey: "access-grant:" + request.ID,
			Target: &pb.EntityRef{Type: platform.ProjectType, Id: request.Project}, Schema: &pb.SchemaRef{Name: platform.SchemaProjectSave, Version: 1}, Payload: payload}
		return d.ConsolePolicies.PrepareProject(c, s, ids)
	case "role":
		if request.Requester == "" || d.members[request.Requester] == nil || !d.members[request.Requester].Active() {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The requester is no longer active")
		}
		if member := d.members[request.Requester]; member != nil && member.Holds(request.App, request.Role) {
			return nil, nil
		}
		app := d.host.App(request.App)
		if app == nil || !slices.Contains(app.Manifest().AllRoles(), request.Role) {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Application {app} no longer declares role {role}", request.App, request.Role)
		}
		payload, err := json.Marshal(map[string]string{"app": request.App, "role": request.Role, "until": request.Until, "reason": request.Reason})
		if err != nil {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
		}
		s := &pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: PlatformApp, IdempotencyKey: "access-grant:" + request.ID,
			Target: &pb.EntityRef{Type: MemberType, Id: request.Requester}, Schema: &pb.SchemaRef{Name: SchemaGrant, Version: 1}, Payload: payload}
		return d.decideMember(c, s)
	}
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
}

func (d *Console) accessRequestClosed(c platform.Caller, request AccessRequest, now time.Time, notify []string) func(*pb.ChangeRecord) *kernel.Error {
	return func(r *pb.ChangeRecord) *kernel.Error {
		if err := c.Assign(r, platform.Assignment{Close: true, Key: "access:" + request.ID}); err != nil {
			return err
		}
		to := make([]platform.Recipient, 0, len(notify))
		for _, member := range notify {
			if member != "" {
				to = append(to, platform.Recipient{Member: member})
			}
		}
		if len(to) > 0 {
			c.Notify(platform.Notification{Title: "Access request " + request.State, Body: accessRequestTarget(request),
				Ref: "read:access-requests", Key: "access-decision:" + request.ID}, now, to...)
		}
		return nil
	}
}

func (d *Console) projectApprovers(project, requester string) []string {
	out := []string{}
	for _, item := range d.ConsolePolicies.ProjectViews() {
		if item.ID != project || item.Archived {
			continue
		}
		for _, member := range item.Members {
			if member.Role == platform.ProjectOwnerRole && member.Member != requester && d.activeMember(member.Member) {
				out = append(out, member.Member)
			}
		}
	}
	if len(out) == 0 {
		return d.platformApprovers(requester)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func (d *Console) platformApprovers(requester string) []string {
	out := []string{}
	for _, member := range d.members {
		if member.ID != requester && member.Active() && member.Holds(PlatformApp, Admin) {
			out = append(out, member.ID)
		}
	}
	slices.Sort(out)
	return out
}

func (d *Console) accessApprovers(request *AccessRequest) []string {
	if request.Scope == "project" {
		return d.projectApprovers(request.Project, request.Requester)
	}
	return d.platformApprovers(request.Requester)
}

func (d *Console) activeMember(id string) bool {
	member := d.members[id]
	return member != nil && member.Active()
}

func (d *Console) project(id string) (platform.BuildProject, bool) {
	for _, project := range d.ConsolePolicies.ProjectViews() {
		if project.ID == id && !project.Archived {
			return project, true
		}
	}
	return platform.BuildProject{}, false
}

func approversAsRecipients(approvers []string) []platform.Recipient {
	out := make([]platform.Recipient, 0, len(approvers))
	for _, member := range approvers {
		out = append(out, platform.Recipient{Member: member})
	}
	return out
}

func accessRequestTarget(request AccessRequest) string {
	if request.Scope == "project" {
		return "project " + request.Project + " as " + request.Role
	}
	return request.App + " role " + request.Role
}

func projectRoleRank(role string) int {
	switch role {
	case platform.ProjectOwnerRole:
		return 4
	case platform.ProjectEditorRole:
		return 3
	case platform.ProjectViewerRole:
		return 2
	case platform.ProjectDiscovererRole:
		return 1
	default:
		return 0
	}
}
