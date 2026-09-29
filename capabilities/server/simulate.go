package platformserver

import (
	"encoding/json"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/build"
	"platformserver/platform"
)

// Simulation is what a submission would change if it were taken now, decided
// in a private staged decision that is then discarded (ADR-0040 21d): no
// journal entry, no record, notice, effect or delivery leaves it.
type Simulation struct {
	Accepted bool              `json:"accepted"`
	Refusal  string            `json:"refusal,omitempty"`
	Changes  []SimulatedChange `json:"changes"`
}

// SimulatedChange is one record the decision would write, as it would read.
type SimulatedChange struct {
	Type   string          `json:"type"`
	ID     string          `json:"id"`
	Record json.RawMessage `json:"record"`
}

// Simulate lets a builder try an action as another member would take it,
// against the tenant's current records, without any of it being kept. Only
// actions that take the accepted-result path can be staged; others are refused
// rather than run for real.
func (t *Tenant) Simulate(builder platform.Member, as string, s *pb.Submission, now time.Time) (Simulation, *kernel.Error) {
	if builder.Roles[build.ID] != build.Builder {
		return Simulation{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	m := builder
	if as != "" {
		other, ok := t.member(as)
		if !ok {
			return Simulation{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "{member} is not a member here", as)
		}
		m = other
	}
	a := t.owner["action:"+s.GetSchema().GetName()]
	ra, ok := a.(platform.ResultApp)
	if a == nil || !ok {
		return Simulation{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA, "{action} cannot be tried", s.GetSchema().GetName())
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.quarantined() {
		return Simulation{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	if !t.acceptsGenerated(a, s) {
		return Simulation{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{action} cannot be tried without running it", s.GetSchema().GetName())
	}
	s.TenantId, s.PrincipalId, s.Authority = t.ID, m.ID, a.Manifest().ID
	if s.GetIdempotencyKey() == "" {
		s.IdempotencyKey = "simulate"
	}
	draft := t.newStagedDecision()
	_, refusal := decideAccepted(ra, draft, m, s, now)
	out := Simulation{Changes: []SimulatedChange{}}
	if refusal != nil {
		out.Refusal = explained(refusal, a, s.GetSchema().GetName(), target(s)).Message
		return out, nil
	}
	out.Accepted = true
	draft.records.mu.Lock()
	defer draft.records.mu.Unlock()
	for _, ref := range slices.Sorted(func(yield func(string) bool) {
		for k := range draft.records.writes {
			if !yield(k) {
				return
			}
		}
	}) {
		typ, id, _ := strings.Cut(ref, "/")
		et := draft.records.types[typ]
		// The builder sees only what their own roles read: a staged change to
		// another app's record they may not read is counted, not shown.
		if et == nil || et.rows[id] == nil || builder.Roles[et.info.App] == "" {
			continue
		}
		raw, _ := json.Marshal(et.rows[id].value.Interface())
		out.Changes = append(out.Changes, SimulatedChange{Type: typ, ID: id, Record: raw})
	}
	return out, nil
}
