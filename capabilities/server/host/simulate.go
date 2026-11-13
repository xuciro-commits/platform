package host

import (
	"context"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	runtimekernel "platformserver/kernel"
	"platformserver/platform"
	"platformserver/services/actions"
	trialowner "platformserver/services/devops/trials"
	"platformserver/services/resources/authoring"
	"slices"
	"time"
)

func (t *Tenant) Simulate(member platform.Member, as, branch string, s *pb.Submission, now time.Time) (trialowner.Simulation, *kernel.Error) {
	return recordRead(t, context.Background(), func(t *Tenant) (trialowner.Simulation, *kernel.Error) {
		member, m, a, s, refusal := trialowner.SubmissionTrial{
			Admits:        t.Admits,
			Authorization: t.authorizationMember,
			Quarantined:   t.quarantined,
			Authorize: func(member platform.Member) *kernel.Error {
				caller := platform.NewCaller(runtime{t: t, scoped: true}, member, build.ID, false, false)
				return authorizationRefusal(t.authorizeNative(caller, "POST /v1/simulate", "action", []string{build.Builder}, true))
			},
			Member:  t.member,
			Owner:   func(schema string) platform.App { return t.App(t.owner["action:"+schema]) },
			Current: t.currentSubmissionSchema,
			Bound:   t.delegatedBound,
			Tenant:  t.ID,
		}.Prepare(member, as, branch, s)
		if refusal != nil {
			return trialowner.Simulation{}, refusal
		}
		var draft *stagedDecision
		if branch == "" {
			draft = t.newStagedDecision()
		} else {
			world, refusal := t.branchWorldLocked(member, branch, now)
			if refusal != nil {
				return trialowner.Simulation{}, refusal
			}
			draft = t.newStagedDecisionOn(world.view.Projection.ForkRecords())
			if build.BranchDraftType(s.GetTarget().GetType()) {
				s.ExpectedRevision = nil
			}
		}
		draft.Simulation = true
		_, refusal = decideAcceptedAs(a, draft, m, s, now, false)
		out := trialowner.Simulation{Changes: []trialowner.SimulatedChange{}}
		if refusal != nil {
			out.Refusal = actions.Explained(refusal, a, s.GetSchema().GetName(), runtimekernel.Target(s)).Message
			out.RefusalCode = refusal.Code.String()
			out.Issues = t.readableFieldIssues(member, draft.InputIssues[refusal], now)
			out.Unsupported = platform.IsOutsideAcceptedBoundary(refusal)
			return out, nil
		}
		out.Accepted = true
		view := &Tenant{services: t.services, disabledOwners: t.disabledOwners, ID: t.ID, identity: t.identity, ownerSet: t.ownerSet, Projection: draft.Records, directory: t.directory}
		out.Changes = trialowner.SimulatedChanges(slices.Sorted(draft.Records.Writes()), func(typ string, q platform.Query) ([]any, *kernel.Error) {
			page, refusal := view.recordsRead(member, typ, q, now)
			return page.Records, refusal
		})
		return out, nil
	})
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
func (d *stagedDecision) Simulating() bool { return d.Simulation || d.tenant.simulation }

type simulationEnvironment struct {
	trialowner.Environment
	Services platform.RuntimeCapabilities
}

func (t *Tenant) simulationEnvironment(candidate platform.ReleaseCandidate, samples []trialowner.SimulationSample) (simulationEnvironment, error) {
	environment, err := trialowner.PrepareEnvironment(t.apps, candidate, samples)
	return simulationEnvironment{Environment: environment, Services: t.services}, err
}
