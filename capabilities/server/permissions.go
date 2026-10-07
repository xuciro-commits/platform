package platformserver

import (
	"slices"
	"strings"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/platform/authz"
)

// Permission catalog (ADR-0078 §3.2): what each role in each app may do,
// derived from the manifests — the same catalog decisions use, so the page
// that shows it and the engine that enforces it cannot disagree.
type RolePermissions struct {
	Role    string             `json:"role"`
	Holders int                `json:"holders"`
	Actions []PermissionAction `json:"actions"`
}

type PermissionAction struct {
	ID    string `json:"id"` // the permission id: the action's schema
	Title string `json:"title"`
	Scope string `json:"scope"` // entity type the action targets
}

type AppPermissions struct {
	App      string            `json:"app"`
	Title    string            `json:"title"`
	Reads    []string          `json:"reads"`    // reads administrators (and app roles) may use
	Everyone []string          `json:"everyone"` // reads any member may use
	Roles    []RolePermissions `json:"roles"`
}

// Permissions is the tenant's catalog: every app it runs, every role, every action.
func (t *Tenant) Permissions() []AppPermissions {
	d, _ := t.app(PlatformApp).(*Console)
	out := []AppPermissions{}
	for _, a := range t.apps {
		m := a.Manifest()
		ap := AppPermissions{App: m.ID, Title: m.Title, Reads: append([]string{}, m.Reads...), Everyone: append([]string{}, m.Everyone...), Roles: []RolePermissions{}}
		if ap.Title == "" {
			ap.Title = m.ID
		}
		for _, role := range m.AllRoles() {
			rp := RolePermissions{Role: role, Actions: []PermissionAction{}}
			if d != nil {
				rp.Holders = len(d.holding(m.ID, role))
			}
			for _, act := range m.Actions.For(role) {
				rp.Actions = append(rp.Actions, PermissionAction{ID: act.Schema, Title: act.Title, Scope: act.Target})
			}
			ap.Roles = append(ap.Roles, rp)
		}
		out = append(out, ap)
	}
	key := func(a AppPermissions) string { // the platform first
		if a.App == PlatformApp {
			return " "
		}
		return a.App
	}
	slices.SortFunc(out, func(a, b AppPermissions) int { return strings.Compare(key(a), key(b)) })
	return out
}

// Explanation is the engine's answer to "may this member do this?", for the
// explain endpoint (ADR-0078 §3.4): the permission, who holds what, and the verdict.
type Explanation struct {
	Member     string        `json:"member"`
	Permission string        `json:"permission"`
	App        string        `json:"app"`
	Roles      []string      `json:"roles"`   // the member's roles in the app today
	Allowed    []string      `json:"allowed"` // the roles the permission names
	Verdict    authz.Verdict `json:"verdict"`
}

// Explain decides permission for member and says why: an action's schema
// ("<app>.<entity>.<verb>"), or a read ("<app>:read:<name>").
func (t *Tenant) Explain(member, permission string) (Explanation, *kernel.Error) {
	m, ok := t.member(member)
	if !ok {
		return Explanation{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	out := Explanation{Member: member, Permission: permission, Roles: []string{}, Allowed: []string{}}
	if app, name, isRead := strings.Cut(permission, ":read:"); isRead {
		a := t.app(app)
		if a == nil {
			return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "No app {app}", app)
		}
		mf := a.Manifest()
		out.App, out.Roles = app, m.RolesIn(app)
		switch {
		case slices.Contains(mf.Everyone, name):
			out.Allowed = []string{authz.AnyMember}
		case slices.Contains(mf.Reads, name):
			out.Allowed = mf.AllRoles()
		default:
			return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "No read {name} in {app}", name, app)
		}
		out.Verdict = m.May(app, permission, out.Allowed)
		return out, nil
	}
	for _, a := range t.apps {
		mf := a.Manifest()
		if action, own := mf.Actions.Action(permission); own {
			out.App, out.Roles, out.Allowed = mf.ID, m.RolesIn(mf.ID), action.Roles
			out.Verdict = mf.Actions.Decide(platform.Caller{Member: m, App: mf.ID}, permission)
			return out, nil
		}
	}
	return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "No action {action}", permission)
}
