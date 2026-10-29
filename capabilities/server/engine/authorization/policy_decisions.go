package authorization

import (
	"encoding/json"
	"maps"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/platform/authz"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"slices"
	"strings"
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

func (d *Policies) decideProject(c platform.Caller, s *pb.Submission, members []string) (func(*pb.ChangeRecord), *kernel.Error) {
	invalid := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	id := s.GetTarget().GetId()
	if s.GetSchema().GetName() == platform.SchemaProjectArchive {
		p := d.projects[id]
		if p == nil {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
		return func(*pb.ChangeRecord) { p.Archived = true }, nil
	}
	var p struct {
		Name, Title string
		Members     []platform.ProjectMember `json:"members"`
		Assets      []platform.ProjectAsset  `json:"assets"`
	}
	if id == "" || json.Unmarshal(s.GetPayload(), &p) != nil || p.Name == "" || p.Title == "" {
		return nil, invalid
	}
	if p.Name != strings.ToLower(p.Name) {
		return nil, invalid
	}
	for _, e := range p.Members {
		if e.Member == "" || e.Role == "" || e.Role != platform.ProjectEditorRole {
			return nil, invalid
		}
	}
	for _, a := range p.Assets {
		if a.Kind == "" || a.Name == "" {
			return nil, invalid
		}
	}
	if d.host == nil {
		return nil, invalid
	}
	for _, e := range p.Members {
		if !slices.Contains(members, e.Member) {
			return nil, invalid
		}
	}
	return func(*pb.ChangeRecord) {
		p := &platform.BuildProject{Name: p.Name, Title: p.Title, Members: p.Members, Assets: p.Assets}
		p.ID, p.Changed = id, platform.Stamp{At: time.Now().UTC()}
		if prior := d.projects[id]; prior != nil {
			p.Created, p.Revision = prior.Created, prior.Revision+1
		}
		d.projects[id] = p
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

func (d *Policies) EditsProjectAsset(member string, ref platform.ProjectAsset) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, p := range d.projects {
		if p.Archived || !covers(*p, ref) {
			continue
		}
		for _, e := range p.Members {
			if e.Member == member && e.Role == platform.ProjectEditorRole {
				return true
			}
		}
	}
	return false
}

func covers(p platform.BuildProject, ref platform.ProjectAsset) bool {
	for _, a := range p.Assets {
		if a.Kind == ref.Kind && a.Name == ref.Name && (a.App == "" || ref.App == "" || a.App == ref.App) {
			return true
		}
	}
	return false
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
			if e.Member == member && e.Role == platform.ProjectEditorRole {
				out = append(out, platform.Grant{App: "build", Role: "builder", By: "project:" + id, Reason: p.Title})
				break
			}
		}
	}
	return out
}

func (d *Policies) ApplyAccess() { d.mu.Lock(); defer d.mu.Unlock(); d.applyAccess() }
