package platformserver

import (
	"encoding/json"
	"platformserver/internal/acceptance"
	resultformat "platformserver/internal/acceptance/result"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
)

// Replay-only branches of the console, gathered per ADR-0062 Each is a
// contract with journals already written and may be deleted only when no
// hosted journal still contains the input it decodes:
//
//   - enterpriseRoles / legacyConsolePredecessor: directory images written
//     while the enterprise owner was still called "org" (ADR-0022).
//   - legacyMemberLanguage / replayMemberLanguage: the per-member language
//     action retired by ADR-0028 (the profile owns language now).
//
// Nothing here is reached by a live submission.

// ADR-0062 added core:steward to the local bootstrap seats. Existing journals
// still start with the former seats. Remove this one implicit seed role
// (never a decided grant) from the comparison and require the
// entire resulting directory to match the exact saved predecessor digest.
// The accepted image remains authoritative; this does not grant a new role.
func (t *Tenant) legacyCoreSeedPredecessor(saved resultformat.State, prior json.RawMessage) bool {
	owner, ok := t.app(saved.App).(HistoricalIdentity)
	return ok && owner.LegacyCoreSeedPredecessor(saved, prior)
}
func (d *Console) LegacyCoreSeedPredecessor(saved resultformat.State, prior json.RawMessage) bool {
	if d.host == nil || d.host.App("core") == nil {
		return false
	}
	var current consoleState
	if json.Unmarshal(prior, &current) != nil {
		return false
	}
	changed := false
	for _, m := range current.Members {
		if len(m.Grants) == 0 && m.Roles["core"] == "steward" {
			delete(m.Roles, "core")
			changed = true
		}
	}
	hash, err := acceptance.Digest(current)
	return changed && err == nil && hash == saved.Before
}

// ADR-0022 renamed the org owner. Keep accepted directory bytes unchanged:
// historical hashes describe the stored roles, not their current projection.
func (d *Console) enterpriseRoles() bool {
	return d.host != nil && d.host.App("enterprise") != nil && d.host.App("org") == nil
}

// A changed bootstrap may contain enterprise instead of org before the first
// saved directory image. Accept only the exact historical predecessor, never
// arbitrary directory drift or a mixed-role state. This does not mutate state.
func (t *Tenant) legacyConsolePredecessor(saved resultformat.State, prior json.RawMessage) bool {
	owner, ok := t.app(saved.App).(HistoricalIdentity)
	return ok && owner.LegacyConsolePredecessor(saved, prior)
}
func (d *Console) LegacyConsolePredecessor(saved resultformat.State, prior json.RawMessage) bool {
	if !d.enterpriseRoles() {
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
	hash, err := acceptance.Digest(current)
	return changed && err == nil && hash == saved.Before
}

// Retired in ADR-0028. Replay keeps the original schema, change identity and
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
