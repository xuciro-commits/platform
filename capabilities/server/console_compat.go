package platformserver

import (
	"encoding/json"

	"platformserver/platform"
)

// ADR-0068 renamed the org owner. Keep accepted directory bytes unchanged:
// historical hashes describe the stored roles, not their current projection.
func (d *Console) enterpriseRoles() bool {
	return d.t != nil && d.t.app("enterprise") != nil && d.t.app("org") == nil
}

func (d *Console) currentMember(m *platform.Member) platform.Member {
	out := clone(m)
	if d.enterpriseRoles() {
		if role := out.Roles["org"]; role != "" {
			if _, explicit := out.Roles["enterprise"]; !explicit {
				out.Roles["enterprise"] = role
			}
			delete(out.Roles, "org")
		}
	}
	return out
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
