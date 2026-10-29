package platformserver

import (
	controlowner "platformserver/kernel/control"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/platform/authz"
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

func (t *Tenant) Permissions() []AppPermissions {
	d, _ := t.app(controlowner.PlatformApp).(platform.IdentityDirectory)
	out := []AppPermissions{}
	for _, a := range t.liveApps() {
		m := a.Manifest()
		ap := AppPermissions{App: m.ID, Title: m.Title, Reads: append([]string{}, m.Reads...), Everyone: append([]string{}, m.Everyone...), Roles: []RolePermissions{}}
		if ap.Title == "" {
			ap.Title = m.ID
		}
		for _, role := range m.AllRoles() {
			rp := RolePermissions{Role: role, Actions: []PermissionAction{}}
			if d != nil {
				rp.Holders = len(d.Holding(m.ID, role))
			}
			for _, act := range m.Actions.For(role) {
				rp.Actions = append(rp.Actions, PermissionAction{ID: act.Schema, Title: act.Title, Scope: act.Target})
			}
			ap.Roles = append(ap.Roles, rp)
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

type Explanation struct {
	Member     string        `json:"member"`
	Permission string        `json:"permission"`
	App        string        `json:"app"`
	Roles      []string      `json:"roles"`
	Allowed    []string      `json:"allowed"`
	Verdict    authz.Verdict `json:"verdict"`
}

func (t *Tenant) ExplainOperation(request platform.AuthzRequest, at time.Time) (platform.AuthzDecision, *kernel.Error) {
	m, known := t.member(request.Principal)
	if !known {
		m = platform.Member{ID: request.Principal, Tenant: request.Tenant}
	}
	app := ""
	if info, exists := t.entity(request.Type); exists {
		app = info.App
	}
	c := platform.NewCaller(runtime{t}, m, app, false, false)
	return t.authorizeFrom(t.records, c, request, at, false), nil
}

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
		out.App, out.Roles = app, nonNil(m.RolesIn(app))
		switch {
		case slices.Contains(mf.Everyone, name):
			out.Allowed = []string{authz.AnyMember}
		case slices.Contains(mf.Reads, name):
			out.Allowed = mf.AllRoles()
		default:
			return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "No read {name} in {app}", name, app)
		}
		out.Verdict = t.authorizeEntry(m, app, name, "read", Now())
		return out, nil
	}
	for _, a := range t.liveApps() {
		mf := a.Manifest()
		if action, own := mf.Actions.Action(permission); own {
			out.App, out.Roles, out.Allowed = mf.ID, nonNil(m.RolesIn(mf.ID)), nonNil(action.Roles)
			out.Verdict = mf.Actions.Decide(platform.NewCaller(runtime{t}, m, mf.ID, false, false), permission)
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
