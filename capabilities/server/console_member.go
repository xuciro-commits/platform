package platformserver

import (
	"maps"
	"slices"
	"time"

	"platformserver/platform"
)

// The member a caller sees: the stored seat projected through today's profile,
// grants, teams and projects.
func (d *Console) currentMember(m *platform.Member) platform.Member {
	out := clone(m)
	if p := d.profiles[m.ID]; p != nil { // language and timezone are the profile's; Member carries them for callers
		out.Language, out.Timezone = p.Language, p.Timezone
	}
	if out.Timezone == "" {
		out.Timezone = d.tenantDefault(SettingTimezone)
	}
	today := out.Today(time.Now())
	if out.Status == platform.MemberInvited && !d.lastSeen[out.ID].IsZero() {
		out.Status = "" // signing in accepts the invitation
	}
	if !out.Active() { // suspended or left: holds nothing
		out.Grants, out.Roles = nil, map[string]string{}
		return out
	}
	d.migrateGrants(&out)
	out.Grants = append(slices.Clone(out.Grants), d.teamGrants(out.ID)...) // teams' grants are held as long as one belongs
	out.Grants = append(out.Grants, d.projectGrantsLocked(out.ID)...)      // a project's editors build within it (#141)
	d.deriveRoles(&out, today)
	out.Grants = slices.DeleteFunc(slices.Clone(out.Grants), func(g platform.Grant) bool { return !g.Active(today) })
	return out
}

// migrateGrants turns a legacy member's Roles into grants:
// before grants existed, Roles was what they held.
func (d *Console) migrateGrants(m *platform.Member) {
	if len(m.Grants) > 0 {
		return
	}
	for _, app := range slices.Sorted(maps.Keys(m.Roles)) {
		m.Grants = append(m.Grants, platform.Grant{App: app, Role: m.Roles[app]})
	}
}

// deriveRoles preserves a still-held primary role when adding grants. Losing
// that grant selects the app's first declared role still active today. The
// full grant set continues to own authorization.
func (d *Console) deriveRoles(m *platform.Member, today string) {
	active := slices.DeleteFunc(slices.Clone(m.Grants), func(g platform.Grant) bool { return !g.Active(today) })
	previous := m.Roles
	m.Roles = map[string]string{}
	for _, g := range active {
		if m.Roles[g.App] == "" {
			if role := previous[g.App]; slices.ContainsFunc(active, func(held platform.Grant) bool { return held.App == g.App && held.Role == role }) {
				m.Roles[g.App] = role
			} else {
				m.Roles[g.App] = d.primaryRole(g.App, active)
			}
		}
	}
}

// primaryRole is the app's first declared role among grants, else the first granted.
func (d *Console) primaryRole(app string, grants []platform.Grant) string {
	held := func(role string) bool {
		return slices.ContainsFunc(grants, func(g platform.Grant) bool { return g.App == app && g.Role == role })
	}
	if d.host != nil && d.host.App(app) != nil {
		for _, role := range d.host.Manifest(app).AllRoles() {
			if held(role) {
				return role
			}
		}
	}
	for _, g := range grants {
		if g.App == app {
			return g.Role
		}
	}
	return ""
}
