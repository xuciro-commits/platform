package platformserver

import (
	"slices"
	"strings"
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
