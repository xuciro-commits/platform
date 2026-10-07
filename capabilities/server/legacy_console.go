package platformserver

import (
	"encoding/json"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
)

// Replay-only branches of the console, gathered per ADR-0080 §1.6. Each is a
// contract with journals already written and may be deleted only when no
// hosted journal still contains the input it decodes:
//
//   - enterpriseRoles / legacyConsolePredecessor: directory images written
//     while the enterprise owner was still called "org" (ADR-0068).
//   - legacyMemberLanguage / replayMemberLanguage: the per-member language
//     action retired by ADR-0079 (the profile owns language now).
//
// Nothing here is reached by a live submission.

// ADR-0068 renamed the org owner. Keep accepted directory bytes unchanged:
// historical hashes describe the stored roles, not their current projection.
func (d *Console) enterpriseRoles() bool {
	return d.t != nil && d.t.app("enterprise") != nil && d.t.app("org") == nil
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
