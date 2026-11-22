package security

import (
	"slices"
	"strings"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	controlowner "platformserver/services/security/control"
)

type RolePermissions struct {
	Role    string             `json:"role"`
	Holders int                `json:"holders"`
	Actions []PermissionAction `json:"actions"`
}

type PermissionAction struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Scope string `json:"scope"`
}

type AppPermissions struct {
	App      string            `json:"app"`
	Title    string            `json:"title"`
	Reads    []string          `json:"reads"`
	Everyone []string          `json:"everyone"`
	Roles    []RolePermissions `json:"roles"`
}

type Explanation struct {
	Member     string                 `json:"member"`
	Permission string                 `json:"permission"`
	App        string                 `json:"app"`
	Roles      []string               `json:"roles"`
	Allowed    []string               `json:"allowed"`
	Verdict    platform.AuthzDecision `json:"verdict"`
}

type PermissionsReport struct {
	Apps    func() []platform.Service
	Holders func(app, role string) int
}

func (p PermissionsReport) Permissions() []AppPermissions {
	out := []AppPermissions{}
	seen := map[string]bool{}
	spaceRoles := map[string][]string{}
	for _, a := range p.Apps() {
		m := a.Manifest()
		seen[m.ID] = true
		ap := AppPermissions{App: m.ID, Title: m.Title, Reads: append([]string{}, m.Reads...), Everyone: append([]string{}, m.Everyone...), Roles: []RolePermissions{}}
		if ap.Title == "" {
			ap.Title = m.ID
		}
		for _, role := range m.AllRoles() {
			rp := RolePermissions{Role: role, Holders: p.Holders(m.ID, role), Actions: []PermissionAction{}}
			for _, act := range m.Actions.ForRoles([]string{role}) {
				rp.Actions = append(rp.Actions, PermissionAction{ID: act.Schema, Title: act.Title, Scope: act.Target})
			}
			ap.Roles = append(ap.Roles, rp)
		}
		out = append(out, ap)
		for _, entity := range m.Entities {
			space, _, ok := strings.Cut(entity.Type, ".")
			if !ok || space == "" || space == m.ID {
				continue
			}
			for role := range entity.Scope.Levels {
				if role != "builder" && !slices.Contains(spaceRoles[space], role) {
					spaceRoles[space] = append(spaceRoles[space], role)
				}
			}
		}
	}
	for space, roles := range spaceRoles {
		if seen[space] || len(roles) == 0 {
			continue
		}
		slices.Sort(roles)
		ap := AppPermissions{App: space, Title: space, Reads: []string{}, Everyone: []string{}, Roles: make([]RolePermissions, 0, len(roles))}
		for _, role := range roles {
			ap.Roles = append(ap.Roles, RolePermissions{Role: role, Holders: p.Holders(space, role), Actions: []PermissionAction{}})
		}
		out = append(out, ap)
	}
	key := func(a AppPermissions) string {
		if a.App == controlowner.PlatformApp {
			return " "
		}
		return a.App
	}
	slices.SortFunc(out, func(a, b AppPermissions) int { return strings.Compare(key(a), key(b)) })
	return out
}

type PermissionExplainer struct {
	Member func(string) (platform.Member, bool)
	App    func(string) platform.Service
	Apps   func() []platform.Service
	Entry  func(m platform.Member, app, name string) platform.AuthzDecision
	Caller func(m platform.Member, app string) platform.Caller
}

func (e PermissionExplainer) Explain(member, permission string) (Explanation, *platform.Error) {
	m, ok := e.Member(member)
	if !ok {
		return Explanation{}, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	out := Explanation{Member: member, Permission: permission, Roles: []string{}, Allowed: []string{}}
	if app, name, isRead := strings.Cut(permission, ":read:"); isRead {
		a := e.App(app)
		if a == nil {
			return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "No app {app}", app)
		}
		mf := a.Manifest()
		out.App, out.Roles = app, nonNil(m.RolesIn(app))
		switch {
		case slices.Contains(mf.Everyone, name):
			out.Allowed = []string{platform.AnyMember}
		case slices.Contains(mf.Reads, name):
			out.Allowed = mf.AllRoles()
		default:
			return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "No read {name} in {app}", name, app)
		}
		out.Verdict = e.Entry(m, app, name)
		return out, nil
	}
	for _, a := range e.Apps() {
		mf := a.Manifest()
		if action, own := mf.Actions.Action(permission); own {
			out.App, out.Roles, out.Allowed = mf.ID, nonNil(m.RolesFor(mf.ID, permission)), nonNil(action.Roles)
			out.Verdict = mf.Actions.Decide(e.Caller(m, mf.ID), permission)
			return out, nil
		}
	}
	return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "No action {action}", permission)
}

func nonNil(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}
