package authorization

import (
	"encoding/json"
	"maps"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/platform/authz"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"slices"
	"sync"
	"time"
)

type Policies struct {
	mu       sync.Mutex
	host     platform.AppCatalog
	roles    map[string]*platform.CustomRole
	policies map[string]*platform.PolicyRecord
	teams    map[string]*platform.Team
	projects map[string]*platform.BuildProject
}

func NewPolicies() *Policies {
	return &Policies{roles: map[string]*platform.CustomRole{}, policies: map[string]*platform.PolicyRecord{}, teams: map[string]*platform.Team{}, projects: map[string]*platform.BuildProject{}}
}
func (d *Policies) Attach(host platform.AppCatalog) { d.host = host }
func (d *Policies) SnapshotState() platform.AuthorizationState {
	d.mu.Lock()
	defer d.mu.Unlock()
	return platform.AuthorizationState{Roles: maps.Clone(d.roles), Policies: maps.Clone(d.policies), Teams: maps.Clone(d.teams), Projects: maps.Clone(d.projects)}
}
func (d *Policies) Restore(state platform.AuthorizationState) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.host != nil {
		for id, role := range d.roles {
			next := state.Roles[id]
			if next == nil || next.App != role.App {
				if app := d.host.App(role.App); app != nil {
					app.Manifest().Actions.DefineRole(id, nil)
				}
			}
		}
	}
	d.roles, d.policies, d.teams, d.projects = state.Roles, state.Policies, state.Teams, state.Projects
	if d.roles == nil {
		d.roles = map[string]*platform.CustomRole{}
	}
	if d.policies == nil {
		d.policies = map[string]*platform.PolicyRecord{}
	}
	if d.teams == nil {
		d.teams = map[string]*platform.Team{}
	}
	if d.projects == nil {
		d.projects = map[string]*platform.BuildProject{}
	}
	d.applyAccess()
}
func (d *Policies) PrepareAccess(c platform.Caller, s *pb.Submission, members []string) (func(*pb.ChangeRecord), *kernel.Error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	apply, err := d.decideAccess(c, s, members)
	return d.accepted(apply), err
}
func (d *Policies) PrepareProject(c platform.Caller, s *pb.Submission, members []string) (func(*pb.ChangeRecord), *kernel.Error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	apply, err := d.decideProject(c, s, members)
	return d.accepted(apply), err
}
func (d *Policies) accepted(apply func(*pb.ChangeRecord)) func(*pb.ChangeRecord) {
	if apply == nil {
		return nil
	}
	return func(r *pb.ChangeRecord) { d.mu.Lock(); defer d.mu.Unlock(); apply(r) }
}

func (d *Policies) decideAccess(c platform.Caller, s *pb.Submission, members []string) (func(*pb.ChangeRecord), *kernel.Error) {
	invalid := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	id := s.GetTarget().GetId()
	if id == "" || d.host == nil {
		return nil, invalid
	}
	day := func(v string) bool { _, err := time.Parse(time.DateOnly, v); return v == "" || err == nil }
	switch s.GetSchema().GetName() {
	case platform.SchemaRoleSave:
		var p platform.CustomRole
		if json.Unmarshal(s.GetPayload(), &p) != nil || len(p.Actions) == 0 {
			return nil, invalid
		}
		app := d.host.App(p.App)
		if app == nil || !c.Replaying && slices.Contains(app.Manifest().Actions.Roles(), id) && !app.Manifest().Actions.Custom(id) {
			return nil, invalid
		}
		for _, schema := range p.Actions {
			if _, own := app.Manifest().Actions.Action(schema); !own {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "No action {action}", schema)
			}
		}
		p.ID = id
		return func(*pb.ChangeRecord) {
			d.roles[id] = &p
			if !c.Staging() {
				app.Manifest().Actions.DefineRole(id, p.Actions)
			}
		}, nil
	case platform.SchemaRoleRemove:
		r := d.roles[id]
		if r == nil {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
		return func(*pb.ChangeRecord) {
			delete(d.roles, id)
			if app := d.host.App(r.App); app != nil && !c.Staging() {
				app.Manifest().Actions.DefineRole(id, nil)
			}
		}, nil
	case platform.SchemaPolicySave:
		var p platform.PolicyRecord
		if json.Unmarshal(s.GetPayload(), &p) != nil || p.Effect != "deny" && p.Effect != "allow" || p.Permission == "" || !day(p.From) || !day(p.Until) {
			return nil, invalid
		}
		for k := range p.Where {
			if !slices.Contains([]string{"member", "app", "agent", "target"}, k) {
				return nil, invalid
			}
		}
		p.ID = id
		return func(*pb.ChangeRecord) { d.policies[id] = &p }, nil
	case platform.SchemaPolicyDrop:
		if d.policies[id] == nil {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
		return func(*pb.ChangeRecord) { delete(d.policies, id) }, nil
	case platform.SchemaTeamSave:
		var p platform.Team
		if json.Unmarshal(s.GetPayload(), &p) != nil || p.Name == "" {
			return nil, invalid
		}
		for _, m := range p.Members {
			if !slices.Contains(members, m) {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "No member {member}", m)
			}
		}
		for i, g := range p.Grants {
			app := d.host.App(g.App)
			if app == nil || !slices.Contains(app.Manifest().AllRoles(), g.Role) || !day(g.From) || !day(g.Until) {
				return nil, invalid
			}
			p.Grants[i].By, p.Grants[i].Reason = "team:"+id, p.Name
		}
		p.ID = id
		if p.Members == nil {
			p.Members = []string{}
		}
		if p.Grants == nil {
			p.Grants = []platform.Grant{}
		}
		return func(*pb.ChangeRecord) { d.teams[id] = &p }, nil
	case platform.SchemaTeamRemove:
		if d.teams[id] == nil {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
		return func(*pb.ChangeRecord) { delete(d.teams, id) }, nil
	}
	return nil, invalid
}

func (d *Policies) TeamGrants(member string) []platform.Grant {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []platform.Grant
	for _, id := range slices.Sorted(maps.Keys(d.teams)) {
		if t := d.teams[id]; slices.Contains(t.Members, member) {
			out = append(out, t.Grants...)
		}
	}
	return out
}

func (d *Policies) activePolicies() []authz.Policy {
	d.mu.Lock()
	defer d.mu.Unlock()
	today := time.Now().UTC().Format(time.DateOnly)
	var out []authz.Policy
	for _, id := range slices.Sorted(maps.Keys(d.policies)) {
		p := d.policies[id]
		if (p.From != "" && today < p.From) || (p.Until != "" && today >= p.Until) {
			continue
		}
		where := maps.Clone(p.Where)
		out = append(out, authz.Policy{ID: p.ID, Effect: p.Effect, Permission: p.Permission, When: func(r authz.Request) bool {
			for k, v := range where {
				if r.Attributes[k] != v {
					return false
				}
			}
			return true
		}})
	}
	return out
}

func (d *Policies) applyAccess() {
	if d.host == nil {
		return
	}
	for _, a := range d.host.Apps() {
		a.Manifest().Actions.UsePolicies(d.activePolicies)
	}
	for _, r := range d.roles {
		if app := d.host.App(r.App); app != nil {
			app.Manifest().Actions.DefineRole(r.ID, r.Actions)
		}
	}
}

func (d *Policies) Access() platform.AccessConfig {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := platform.AccessConfig{Roles: []platform.CustomRole{}, Policies: []platform.PolicyRecord{}, Teams: []platform.Team{}}
	for _, id := range slices.Sorted(maps.Keys(d.roles)) {
		out.Roles = append(out.Roles, *d.roles[id])
	}
	for _, id := range slices.Sorted(maps.Keys(d.policies)) {
		out.Policies = append(out.Policies, *d.policies[id])
	}
	for _, id := range slices.Sorted(maps.Keys(d.teams)) {
		out.Teams = append(out.Teams, *d.teams[id])
	}
	return out
}

var projectRoles = []string{platform.ProjectOwnerRole, platform.ProjectEditorRole, platform.ProjectViewerRole}

func (d *Policies) decideProject(c platform.Caller, s *pb.Submission, members []string) (func(*pb.ChangeRecord), *kernel.Error) {
	invalid := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	id := s.GetTarget().GetId()
	var p struct {
		Title   string                   `json:"title"`
		Members []platform.ProjectMember `json:"members"`
	}
	if id == "" || json.Unmarshal(s.GetPayload(), &p) != nil || p.Title == "" || d.host == nil {
		return nil, invalid
	}
	seen := map[string]bool{}
	owners := 0
	for _, e := range p.Members {
		if e.Member == "" || seen[e.Member] || !slices.Contains(projectRoles, e.Role) || !slices.Contains(members, e.Member) {
			return nil, invalid
		}
		seen[e.Member] = true
		if e.Role == platform.ProjectOwnerRole {
			owners++
		}
	}
	if !platform.ProjectManager(c.Member, d.ProjectRole(c.ID, id)) {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Only the project's owners, builders and administrators manage its access")
	}
	if owners == 0 {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A project keeps at least one owner")
	}
	return func(*pb.ChangeRecord) {
		next := &platform.BuildProject{Title: p.Title, Members: p.Members}
		next.ID, next.Changed = id, platform.Stamp{At: time.Now().UTC()}
		if prior := d.projects[id]; prior != nil {
			next.Created, next.Revision = prior.Created, prior.Revision+1
		}
		d.projects[id] = next
	}, nil
}

func (d *Policies) ProjectViews() []platform.BuildProject {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := []platform.BuildProject{}
	for _, p := range d.projects {
		out = append(out, *p)
	}
	return out
}

func (d *Policies) ProjectRole(member, project string) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	p := d.projects[project]
	if p == nil || p.Archived {
		return ""
	}
	for _, e := range p.Members {
		if e.Member == member {
			return e.Role
		}
	}
	return ""
}

func (d *Policies) ProjectGrants(member string, agent bool) []platform.Grant {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []platform.Grant
	if agent {
		return out
	}
	for _, id := range slices.Sorted(maps.Keys(d.projects)) {
		p := d.projects[id]
		if p.Archived {
			continue
		}
		for _, e := range p.Members {
			if e.Member == member && slices.Contains(projectRoles, e.Role) {
				out = append(out, platform.Grant{App: "build", Role: "builder", By: "project:" + id, Reason: p.Title})
				break
			}
		}
	}
	return out
}

func (d *Policies) ApplyAccess() { d.mu.Lock(); defer d.mu.Unlock(); d.applyAccess() }
