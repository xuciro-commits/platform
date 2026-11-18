package host

import (
	"context"
	"encoding/json"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/protocol"
	"platformserver/platform"
	"platformserver/services/actions"
	trialowner "platformserver/services/devops/trials"
	"platformserver/services/resources/authoring"
	"slices"
	"time"
)

type stagedCandidate struct {
	trialowner.StagedCandidate
	reader *Tenant
}

func (t *Tenant) SimulateCandidate(builder platform.Member, request trialowner.CandidateSimulationRequest) (trialowner.CandidateSimulation, *platform.Error) {
	return t.simulateCandidate(builder, request, nil)
}

func (t *Tenant) simulateCandidate(builder platform.Member, request trialowner.CandidateSimulationRequest, staged *stagedCandidate) (trialowner.CandidateSimulation, *platform.Error) {
	return recordRead(t, context.Background(), func(t *Tenant) (trialowner.CandidateSimulation, *platform.Error) {
		if err := t.buildAllowed(builder, "POST /v1/simulate/candidate", build.Builder); err != nil {
			return trialowner.CandidateSimulation{}, err
		}
		reader := t
		var selected *trialowner.StagedCandidate
		if staged != nil {
			reader, selected = staged.reader, &staged.StagedCandidate
		}
		return trialowner.SimulateCandidate(request, builder, selected, trialowner.CandidateHooks[simulationEnvironment]{
			Quarantined: t.quarantined,
			Member:      t.member,
			Source: trialowner.CandidateSource{
				Drafts:    func() (build.ConstructionDrafts, bool) { owner := t.builders[build.ID]; return owner, owner != nil },
				Candidate: func(id string) (platform.ReleaseCandidate, error) { return t.releases.Candidate(id) },
				Preview: func(drafts []build.JointDraftRef) (string, platform.ReleaseCandidate, error) {
					review, prepared, problem := reader.releasePlannerLocked().Preview(drafts, builder)
					return review.Diagnostic, prepared, problem
				},
				Function: func(id string) string {
					selected, _ := platform.Get[protocol.Function](reader.automation(build.ID, false), id)
					return selected.Name
				},
			},
			Environment: func(candidate platform.ReleaseCandidate) (simulationEnvironment, error) {
				prepared, err := trialowner.PrepareEnvironment(t.apps, t.simulations, actions.NewAuthority, candidate, request.Samples)
				environment := simulationEnvironment{Environment: prepared, Services: t.services}
				if err == nil {
					err = platform.CheckSimulationOrganization(request.Organization)
					environment.Organization = request.Organization
				}
				return environment, err
			},
			Modules:    func(environment simulationEnvironment) map[string][]byte { return environment.Modules },
			ReadModule: func(module string) ([]byte, error) { return t.artifacts().Read(context.Background(), module) },
			Run: func(candidate platform.ReleaseCandidate, environment simulationEnvironment, actor platform.Member, members []platform.Member, functionName string) (trialowner.CandidateSimulation, *platform.Error) {
				return runCandidateSimulation(request, candidate, environment, actor, members, functionName)
			},
		})
	})
}

func runCandidateSimulation(request trialowner.CandidateSimulationRequest, candidate platform.ReleaseCandidate, environment simulationEnvironment, m platform.Member, members []platform.Member, functionName string) (trialowner.CandidateSimulation, *platform.Error) {
	compose := func() (trialowner.Runtime, error) {
		sandbox, err := candidateTestTenantWithEnvironment(candidate, m, environment, slices.DeleteFunc(slices.Clone(members), func(actor platform.Member) bool { return actor.ID == m.ID })...)
		if err != nil {
			return nil, err
		}
		if sandbox.Models != nil {
			if err := configureFunctionFixtureModel(sandbox, request.Model, request.At); err != nil {
				return nil, err
			}
		}
		return &trialowner.CandidateRun{Request: request, Candidate: candidate, Sandbox: sandbox.trialSandbox(), Member: m, Now: request.At}, nil
	}
	return trialowner.RunCandidate(request, candidate, m, members, functionName, compose)
}

func (t *Tenant) trialSandbox() trialowner.Sandbox {
	return trialowner.Sandbox{Tenant: t.ID, AI: t.Models != nil, Flows: t.App(protocol.FlowApp) != nil, Member: t.Member, Work: t.Work,
		Trace: func(record func(platform.DecisionTrace)) func(platform.DecisionTrace) {
			previous := t.simulationTrace
			t.simulationTrace = record
			return previous
		},
		Read: func(actor platform.Member, typ, id string, now time.Time) (json.RawMessage, *platform.Error) {
			return t.caller(actor, t.App(build.ID), false).ReadRecord(typ, id, now)
		},
		Flow: func(id string) (protocol.FlowInstance, bool) {
			return platform.Get[protocol.FlowInstance](t.automation(protocol.FlowApp, false), id)
		},
		Current: func(actor platform.Member, typ, id string, now time.Time) *platform.Error {
			_, refusal := t.recordOf(actor, typ, id, now)
			return refusal
		},
		Owner: func(schema string) string {
			if owner := t.App(t.owner["action:"+schema]); owner != nil {
				return owner.Manifest().ID
			}
			return ""
		},
		Submit: func(actor platform.Member, s *pb.Submission, now time.Time) (*platform.Error, []platform.FieldIssue) {
			_, refusal, issues := t.submitDiagnosed(actor, s, now)
			return refusal, issues
		},
		Compute: func(actor platform.Member, fixture platform.ComputeFixture, now time.Time) (bool, error) {
			return settleComputeFixture(t, actor, fixture, now)
		},
		Function: func(actor platform.Member, fixture platform.FunctionFixture, now time.Time) (bool, error) {
			return settleFunctionFixture(t, actor, fixture, now)
		},
		Records: func(actor platform.Member, typ string, q platform.Query, now time.Time) ([]any, int, *platform.Error) {
			page, refusal := t.objectOwner(t.Projection).Records(actor, typ, q, now)
			return page.Records, page.Total, refusal
		},
		Inbox: func(actor platform.Member) ([]platform.WorkTask, bool) {
			inbox, err := t.read(actor, "inbox")
			if err != nil {
				return nil, false
			}
			return inbox.([]platform.WorkTask), true
		},
		Waiting: func() (bool, bool) {
			effects := t.outboundEffects()
			return len(trialowner.PendingCompute(effects)) != 0, len(trialowner.PendingFunctionCalls(effects, t.modelAsk)) != 0
		},
		Faulted: func() string {
			if fault := t.Fault.Load(); fault != nil {
				return fault.Reason
			}
			return ""
		},
		Save: func() (json.RawMessage, error) {
			saved, _, err := t.Snapshot(func() int64 { return 0 })
			return saved, err
		},
		Load: t.Restore,
		Verify: func() error {
			if t.Processes != nil {
				return t.Processes.Check()
			}
			return nil
		}}
}
