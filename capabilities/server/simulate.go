package platformserver

import (
	"encoding/json"
	"google.golang.org/protobuf/proto"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/studio/build"
	trialowner "platformserver/studio/trials"
	"slices"
	"strings"
	"time"
)

// Simulate lets a member try their own action, or a builder try another member's action,
// against the tenant's current records, without any of it being kept. Only
// actions that take the accepted-result path can be staged; others are refused
// rather than run for real.
func (t *Tenant) Simulate(member platform.Member, as string, s *pb.Submission, now time.Time) (trialowner.Simulation, *kernel.Error) {
	if err := t.admits(member); err != nil {
		return trialowner.Simulation{}, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	member = t.authorizationMember(member)
	if !member.Active() {
		return trialowner.Simulation{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	if s == nil || s.GetSchema() == nil || s.GetTarget() == nil {
		return trialowner.Simulation{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "a submission with a schema and a target is required")
	}
	if t.quarantined() {
		return trialowner.Simulation{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	m := member
	if as != "" && as != member.ID {
		caller := platform.NewCaller(runtime{t}, member, build.ID, false, false)
		if refusal := authorizationRefusal(t.authorizeNative(caller, "POST /v1/simulate", "action", []string{build.Builder}, true)); refusal != nil {
			return trialowner.Simulation{}, refusal
		}
		other, ok := t.member(as)
		if !ok {
			return trialowner.Simulation{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "{member} is not a member here", as)
		}
		m = other
	}
	a := t.owner["action:"+s.GetSchema().GetName()]
	if a == nil {
		return trialowner.Simulation{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA, "{action} cannot be tried", s.GetSchema().GetName())
	}
	s = proto.Clone(s).(*pb.Submission)
	s.TenantId, s.PrincipalId, s.Authority = t.ID, m.ID, a.Manifest().ID
	s = t.currentSubmissionSchema(a, s)
	if err := t.delegatedBound(m, s); err != nil {
		return trialowner.Simulation{}, err
	}
	if s.GetIdempotencyKey() == "" {
		s.IdempotencyKey = "simulate"
	}
	draft := t.newStagedDecision()
	draft.simulation = true
	_, refusal := decideAccepted(a, draft, m, s, now)
	out := trialowner.Simulation{Changes: []trialowner.SimulatedChange{}}
	if refusal != nil {
		out.Refusal = explained(refusal, a, s.GetSchema().GetName(), target(s)).Message
		out.RefusalCode = refusal.Code.String()
		out.Issues = t.readableFieldIssues(member, draft.inputIssues[refusal], now)
		out.Unsupported = refusal.Code == pb.ErrorCode_ERROR_CODE_CONFLICT && refusal.Message == "This action uses an effect outside the accepted-result boundary"
		return out, nil
	}
	out.Accepted = true
	// Use the member-facing read contract on the private records, so scope,
	// field masks and source-derived restrictions all apply to the requester.
	// Privacy read auditing is private to this view and is discarded too.
	view := &Tenant{services: t.services, disabledOwners: t.disabledOwners, ID: t.ID, identity: t.identity, apps: t.apps, appIndex: t.appIndex, owner: t.owner, records: draft.records, directory: t.directory}
	for _, ref := range slices.Sorted(func(yield func(string) bool) {
		for k := range draft.records.writes {
			if !yield(k) {
				return
			}
		}
	}) {
		typ, id, _ := strings.Cut(ref, "/")
		domain, _ := json.Marshal([]any{[]any{"id", "=", id}})
		page, err := view.recordsRead(member, typ, platform.Query{Domain: domain, Limit: 1, Archived: true}, now)
		if err != nil || len(page.Records) == 0 {
			continue
		}
		raw, _ := json.Marshal(page.Records[0])
		out.Changes = append(out.Changes, trialowner.SimulatedChange{Type: typ, ID: id, Record: raw})
	}
	return out, nil
}

func (r runtime) TraceEnabled() bool { return r.t.simulationTrace != nil }
func (r runtime) RecordDecisionTrace(event platform.DecisionTrace) {
	if r.t.simulationTrace != nil {
		r.t.simulationTrace(event)
	}
}
func (d *stagedDecision) TraceEnabled() bool { return d.tenant.simulationTrace != nil }
func (d *stagedDecision) RecordDecisionTrace(event platform.DecisionTrace) {
	if d.tenant.simulationTrace != nil {
		d.tenant.simulationTrace(event)
	}
}

func (r runtime) Simulating() bool         { return r.t.simulation }
func (d *stagedDecision) Simulating() bool { return d.simulation || d.tenant.simulation }

type simulationEnvironment struct {
	trialowner.Environment
	Services RuntimeCapabilities
}

func (t *Tenant) simulationEnvironment(candidate platform.ReleaseCandidate, samples []trialowner.SimulationSample) (simulationEnvironment, error) {
	environment, err := trialowner.PrepareEnvironment(t.apps, candidate, samples)
	return simulationEnvironment{Environment: environment, Services: t.services}, err
}
