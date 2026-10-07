package platformserver

import (
	"encoding/json"
	"maps"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// ADR-0068 renamed the org owner. Keep accepted directory bytes unchanged:
// historical hashes describe the stored roles, not their current projection.
func (d *Console) enterpriseRoles() bool {
	return d.t != nil && d.t.app("enterprise") != nil && d.t.app("org") == nil
}

func (d *Console) currentMember(m *platform.Member) platform.Member {
	out := clone(m)
	if p := d.profiles[m.ID]; p != nil { // language and timezone are the profile's (ADR-0079); Member carries them for callers
		out.Language, out.Timezone = p.Language, p.Timezone
	}
	if out.Timezone == "" {
		out.Timezone = d.tenantDefault(SettingTimezone)
	}
	if d.enterpriseRoles() {
		if role := out.Roles["org"]; role != "" {
			if _, explicit := out.Roles["enterprise"]; !explicit {
				out.Roles["enterprise"] = role
			}
			delete(out.Roles, "org")
		}
		for i := range out.Grants {
			if out.Grants[i].App == "org" {
				out.Grants[i].App = "enterprise"
			}
		}
	}
	today := out.Today(time.Now())
	if out.Status == platform.MemberInvited && !d.lastSeen[out.ID].IsZero() {
		out.Status = "" // signing in accepts the invitation
	}
	if !out.Active() { // suspended or left: holds nothing (ADR-0079 §4)
		out.Grants, out.Roles = nil, map[string]string{}
		return out
	}
	d.migrateGrants(&out)
	out.Grants = append(slices.Clone(out.Grants), d.teamGrants(out.ID)...) // teams' grants are held as long as one belongs (ADR-0078 §3.3)
	d.deriveRoles(&out, today)
	out.Grants = slices.DeleteFunc(slices.Clone(out.Grants), func(g platform.Grant) bool { return !g.Active(today) })
	return out
}

// migrateGrants turns a legacy member's Roles into grants (ADR-0078 §3.3):
// before grants existed, Roles was what they held.
func (d *Console) migrateGrants(m *platform.Member) {
	if len(m.Grants) > 0 {
		return
	}
	for _, app := range slices.Sorted(maps.Keys(m.Roles)) {
		m.Grants = append(m.Grants, platform.Grant{App: app, Role: m.Roles[app]})
	}
}

// deriveRoles projects a member's grants onto Roles: the primary role per
// app among the grants active today (the app's first declared role held),
// for the places that ask for one.
func (d *Console) deriveRoles(m *platform.Member, today string) {
	active := slices.DeleteFunc(slices.Clone(m.Grants), func(g platform.Grant) bool { return !g.Active(today) })
	m.Roles = map[string]string{}
	for _, g := range active {
		if m.Roles[g.App] == "" {
			m.Roles[g.App] = d.primaryRole(g.App, active)
		}
	}
}

// primaryRole is the app's first declared role among grants, else the first granted.
func (d *Console) primaryRole(app string, grants []platform.Grant) string {
	held := func(role string) bool {
		return slices.ContainsFunc(grants, func(g platform.Grant) bool { return g.App == app && g.Role == role })
	}
	if d.t != nil {
		if a := d.t.app(app); a != nil {
			for _, role := range a.Manifest().AllRoles() {
				if held(role) {
					return role
				}
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

// A changed bootstrap may contain enterprise instead of org before the first
// saved directory image. Accept only the exact historical predecessor, never
// arbitrary directory drift or a mixed-role state. This does not mutate state.
func (t *Tenant) legacyConsolePredecessor(saved acceptedState, prior json.RawMessage) bool {
	d, ok := t.app(saved.App).(*Console)
	if !ok || !d.enterpriseRoles() {
		return false
	}
	var current, image consoleState
	if json.Unmarshal(prior, &current) != nil || json.Unmarshal(saved.Image, &image) != nil {
		return false
	}
	legacy := false
	for _, m := range image.Members {
		if _, exists := m.Roles["enterprise"]; exists {
			return false
		}
		legacy = legacy || m.Roles["org"] != ""
	}
	if !legacy {
		return false
	}
	changed := false
	for _, m := range current.Members {
		if role, exists := m.Roles["enterprise"]; exists {
			if _, conflict := m.Roles["org"]; conflict {
				return false
			}
			m.Roles["org"] = role
			delete(m.Roles, "enterprise")
			changed = true
		}
	}
	hash, err := canonicalDigest(current)
	return changed && err == nil && hash == saved.Before
}

// Retired in ADR-0079. Replay keeps the original schema, change identity and
// Member.Language bytes; the current account projects them until edited.
const legacyMemberLanguage = "platform.member.language"

func (d *Console) replayMemberLanguage(s *pb.Submission) (func(*pb.ChangeRecord), *kernel.Error) {
	var p struct{ Language string }
	if s.GetTarget().GetType() != MemberType || json.Unmarshal(s.GetPayload(), &p) != nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	d.mu.Lock()
	member := d.members[s.GetTarget().GetId()]
	d.mu.Unlock()
	if member == nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	return func(*pb.ChangeRecord) { d.mu.Lock(); defer d.mu.Unlock(); member.Language = p.Language }, nil
}
