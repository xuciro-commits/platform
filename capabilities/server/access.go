package platformserver

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/platform/authz"
)

// Access configuration a tenant adds over what the apps declare (ADR-0078
// §3.3–3.4): custom roles, policies and teams, each a record the console
// decides, kept in the directory's state, applied to the apps' catalogs.
const (
	RoleType         = "platform.role"
	SchemaRoleSave   = "platform.role.save"
	SchemaRoleRemove = "platform.role.remove"
	PolicyType       = "platform.policy"
	SchemaPolicySave = "platform.policy.save"
	SchemaPolicyDrop = "platform.policy.remove"
	TeamType         = "platform.team"
	SchemaTeamSave   = "platform.team.save"
	SchemaTeamRemove = "platform.team.remove"
	SchemaDelegate   = "platform.member.delegate"
)

// CustomRole is a role the tenant defines in an app: the actions it may call.
type CustomRole struct {
	ID      string   `json:"id"`
	App     string   `json:"app"`
	Title   string   `json:"title"`
	Actions []string `json:"actions"`
}

// PolicyRecord is a tenant rule: deny or allow a permission (exact, or a
// prefix ending in *) where the request's attributes match, within days.
type PolicyRecord struct {
	ID         string            `json:"id"`
	Title      string            `json:"title"`
	Effect     string            `json:"effect"` // deny, allow
	Permission string            `json:"permission"`
	Where      map[string]string `json:"where,omitempty"` // member, app, agent ("true"), target (entity type)
	From       string            `json:"from,omitempty"`
	Until      string            `json:"until,omitempty"`
}

// Team is a named set of members who hold grants together.
type Team struct {
	ID      string           `json:"id"`
	Name    string           `json:"name"`
	Members []string         `json:"members"`
	Grants  []platform.Grant `json:"grants"`
}

// AccessConfig is what GET /v1/access answers.
type AccessConfig struct {
	Roles    []CustomRole   `json:"roles"`
	Policies []PolicyRecord `json:"policies"`
	Teams    []Team         `json:"teams"`
}

func accessActions() []platform.Action {
	admin := []string{Admin}
	return []platform.Action{
		{Schema: SchemaRoleSave, Target: RoleType, Capability: "members", Title: "Define role",
			Description: "Define or change a role of this tenant in an app: the actions it may call. Members hold it like any role.",
			Payload: []platform.Field{{Name: "app", Type: "string", Required: true, Description: "App ID"}, {Name: "title", Type: "string"},
				{Name: "actions", Type: "json", Required: true, Description: "Action schemas of the app"}}, Roles: admin},
		{Schema: SchemaRoleRemove, Target: RoleType, Capability: "members", Title: "Remove role",
			Description: "Remove a role this tenant defined; grants of it no longer allow anything.", Payload: []platform.Field{}, Roles: admin},
		{Schema: SchemaPolicySave, Target: PolicyType, Capability: "members", Title: "Set policy",
			Description: "Deny or allow a permission under conditions; a deny wins over every role.",
			Payload: []platform.Field{{Name: "title", Type: "string"}, {Name: "effect", Type: "string", Required: true, Choices: []string{"deny", "allow"}},
				{Name: "permission", Type: "string", Required: true, Description: "An action schema, or a prefix ending in *"},
				{Name: "where", Type: "json", Description: "Conditions: member, app, agent, target"},
				{Name: "from", Type: "string", Description: "In force from this day (YYYY-MM-DD)"}, {Name: "until", Type: "string", Description: "Until this day, exclusive"}}, Roles: admin},
		{Schema: SchemaPolicyDrop, Target: PolicyType, Capability: "members", Title: "Remove policy", Description: "Remove a policy.", Payload: []platform.Field{}, Roles: admin},
		{Schema: SchemaTeamSave, Target: TeamType, Capability: "members", Title: "Save team",
			Description: "A team: members who hold the team's grants together, for as long as they belong to it.",
			Payload: []platform.Field{{Name: "name", Type: "string", Required: true}, {Name: "members", Type: "json", Required: true, Description: "Member IDs"},
				{Name: "grants", Type: "json", Description: "Grants every member holds: app, role, unit, structure, until"}}, Roles: admin},
		{Schema: SchemaTeamRemove, Target: TeamType, Capability: "members", Title: "Remove team", Description: "Remove a team; its grants end.", Payload: []platform.Field{}, Roles: admin},
		{Schema: SchemaDelegate, Target: MemberType, Capability: "members", Title: "Delegate my roles",
			Description: "Give another member the roles you hold in an app, until a day: for an absence or a handover. Only what you hold yourself.",
			Payload: []platform.Field{{Name: "app", Type: "string", Required: true, Description: "App ID"}, {Name: "until", Type: "string", Required: true, Description: "Until this day, exclusive (YYYY-MM-DD)"},
				{Name: "reason", Type: "string"}}, Roles: []string{platform.AnyMember}},
	}
}

// decideAccess decides roles, policies and teams. The console's lock is held.
func (d *Console) decideAccess(c platform.Caller, s *pb.Submission) (func(*pb.ChangeRecord), *kernel.Error) {
	invalid := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	id := s.GetTarget().GetId()
	if id == "" || d.t == nil {
		return nil, invalid
	}
	day := func(v string) bool { _, err := time.Parse(time.DateOnly, v); return v == "" || err == nil }
	switch s.GetSchema().GetName() {
	case SchemaRoleSave:
		var p CustomRole
		if json.Unmarshal(s.GetPayload(), &p) != nil || len(p.Actions) == 0 {
			return nil, invalid
		}
		app := d.t.app(p.App)
		if app == nil || !c.Replaying && slices.Contains(app.Manifest().Actions.Roles(), id) && !app.Manifest().Actions.Custom(id) {
			return nil, invalid // not over a role the app itself declares
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
	case SchemaRoleRemove:
		r := d.roles[id]
		if r == nil {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
		return func(*pb.ChangeRecord) {
			delete(d.roles, id)
			if app := d.t.app(r.App); app != nil && !c.Staging() {
				app.Manifest().Actions.DefineRole(id, nil)
			}
		}, nil
	case SchemaPolicySave:
		var p PolicyRecord
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
	case SchemaPolicyDrop:
		if d.policies[id] == nil {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
		return func(*pb.ChangeRecord) { delete(d.policies, id) }, nil
	case SchemaTeamSave:
		var p Team
		if json.Unmarshal(s.GetPayload(), &p) != nil || p.Name == "" {
			return nil, invalid
		}
		for _, m := range p.Members {
			if d.members[m] == nil {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "No member {member}", m)
			}
		}
		for i, g := range p.Grants {
			app := d.t.app(g.App)
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
	case SchemaTeamRemove:
		if d.teams[id] == nil {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
		return func(*pb.ChangeRecord) { delete(d.teams, id) }, nil
	}
	return nil, invalid
}

// decideDelegate gives the target member the caller's roles in an app until a
// day (ADR-0078 §3.3): a grant by the caller, bounded, revocable like any.
func (d *Console) decideDelegate(c platform.Caller, s *pb.Submission) (func(*pb.ChangeRecord), *kernel.Error) {
	invalid := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	var p struct{ App, Until, Reason string }
	to := d.members[s.GetTarget().GetId()]
	if json.Unmarshal(s.GetPayload(), &p) != nil || to == nil || to.ID == c.ID || d.t == nil || d.t.app(p.App) == nil {
		return nil, invalid
	}
	if _, err := time.Parse(time.DateOnly, p.Until); err != nil {
		return nil, invalid
	}
	roles := c.RolesIn(p.App)
	if len(roles) == 0 {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "{member} holds no role in {app}, so may not {action}", c.ID, p.App, "delegate")
	}
	return func(r *pb.ChangeRecord) {
		d.migrateGrants(to)
		for _, role := range roles {
			to.Grants = slices.DeleteFunc(to.Grants, func(g platform.Grant) bool { return g.App == p.App && g.Role == role && g.By == c.ID })
			to.Grants = append(to.Grants, platform.Grant{App: p.App, Role: role, Until: p.Until, By: c.ID, Reason: strings.TrimSpace("delegated " + p.Reason), At: r.GetRecordedTime().AsTime()})
		}
		d.deriveRoles(to, d.currentMember(to).Today(time.Now()))
	}, nil
}

// teamGrants are the grants a member holds through teams.
func (d *Console) teamGrants(member string) []platform.Grant {
	var out []platform.Grant
	for _, id := range slices.Sorted(maps.Keys(d.teams)) {
		if t := d.teams[id]; slices.Contains(t.Members, member) {
			out = append(out, t.Grants...)
		}
	}
	return out
}

// activePolicies are the tenant's policies as the engine reads them, today.
func (d *Console) activePolicies() []authz.Policy {
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

// applyAccess puts the custom roles on the apps' catalogs and the policies in
// their hands, after composition or a restore.
func (d *Console) applyAccess() {
	if d.t == nil {
		return
	}
	for _, a := range d.t.apps {
		a.Manifest().Actions.UsePolicies(d.activePolicies)
	}
	for _, r := range d.roles {
		if app := d.t.app(r.App); app != nil {
			app.Manifest().Actions.DefineRole(r.ID, r.Actions)
		}
	}
}

// Access is the tenant's configuration, sorted.
func (d *Console) Access() AccessConfig {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := AccessConfig{Roles: []CustomRole{}, Policies: []PolicyRecord{}, Teams: []Team{}}
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
