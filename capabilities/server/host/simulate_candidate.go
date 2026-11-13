package host

import (
	"context"
	"encoding/json"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	trialowner "platformserver/services/devops/trials"
	"platformserver/services/resources/authoring"
	"slices"
	"time"
)

type stagedCandidate struct {
	trialowner.StagedCandidate
	reader *Tenant
}

func (t *Tenant) SimulateCandidate(builder platform.Member, request trialowner.CandidateSimulationRequest) (trialowner.CandidateSimulation, *kernel.Error) {
	return t.simulateCandidate(builder, request, nil)
}

func (t *Tenant) simulateCandidate(builder platform.Member, request trialowner.CandidateSimulationRequest, staged *stagedCandidate) (trialowner.CandidateSimulation, *kernel.Error) {
	return recordRead(t, context.Background(), func(t *Tenant) (trialowner.CandidateSimulation, *kernel.Error) {
		var empty trialowner.CandidateSimulation
		if err := t.Admits(builder); err != nil {
			return empty, err
		}
		caller := platform.NewCaller(runtime{t: t}, builder, build.ID, false, false)
		if err := authorizationRefusal(t.authorizeNative(caller, "POST /v1/simulate/candidate", "read", []string{build.Builder}, true)); err != nil {
			return empty, err
		}
		var stagedCandidate *platform.ReleaseCandidate
		reader := t
		var selected *trialowner.StagedCandidate
		if staged != nil {
			stagedCandidate, reader, selected = &staged.Candidate, staged.reader, &staged.StagedCandidate
		}
		if refusal := request.Check(stagedCandidate); refusal != nil {
			return empty, refusal
		}
		if t.quarantined() {
			return empty, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
		m, members, refusal := request.Members(builder, t.member)
		if refusal != nil {
			return empty, refusal
		}
		candidate, functionName, err := request.Select(trialowner.CandidateSource{
			Drafts:    func() (build.ConstructionDrafts, bool) { owner := t.builders[build.ID]; return owner, owner != nil },
			Candidate: func(id string) (platform.ReleaseCandidate, error) { return t.releases.Candidate(id) },
			Preview: func(drafts []build.JointDraftRef) (string, platform.ReleaseCandidate, error) {
				review, prepared, problem := reader.previewReleaseLocked(drafts, builder)
				return review.Diagnostic, prepared, problem
			},
			Function: func(id string) string {
				selected, _ := platform.Get[build.Function](reader.automation(build.ID, false), id)
				return selected.Name
			},
		}, selected)
		environment, environmentErr := t.simulationEnvironment(candidate, request.Samples)
		if environmentErr == nil {
			environmentErr = platform.CheckSimulationOrganization(request.Organization)
			environment.Organization = request.Organization
		}
		if err == nil {
			err = environmentErr
		}
		if err != nil {
			return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The candidate cannot be tested: {why}", err.Error())
		}
		if refusal := trialowner.CandidateModules(candidate, environment.Modules, func(module string) ([]byte, error) { return t.artifacts().Read(context.Background(), module) }); refusal != nil {
			return empty, refusal
		}
		out, problem := runCandidateSimulation(request, candidate, environment, m, members, functionName)
		if problem != nil {
			return empty, problem
		}
		return trialowner.RunActors(request, out, m, members, func(actor platform.Member) (trialowner.CandidateSimulation, *kernel.Error) {
			return runCandidateSimulation(request, candidate, environment, actor, members, functionName)
		})
	})
}

func runCandidateSimulation(request trialowner.CandidateSimulationRequest, candidate platform.ReleaseCandidate, environment simulationEnvironment, m platform.Member, members []platform.Member, functionName string) (trialowner.CandidateSimulation, *kernel.Error) {
	compose := func() (trialowner.Runtime, error) {
		sandbox, err := candidateTestTenantWithEnvironment(candidate, m, environment, slices.DeleteFunc(slices.Clone(members), func(actor platform.Member) bool { return actor.ID == m.ID })...)
		if err != nil {
			return nil, err
		}
		if sandbox.ai != nil {
			if err := configureFunctionFixtureModel(sandbox, request.Model, request.At); err != nil {
				return nil, err
			}
		}
		return &trialowner.CandidateRun{Request: request, Candidate: candidate, Sandbox: sandbox.trialSandbox(), Member: m, Now: request.At}, nil
	}
	return trialowner.RunCandidate(request, candidate, m, members, functionName, compose)
}

func (t *Tenant) trialSandbox() trialowner.Sandbox {
	return trialowner.Sandbox{Tenant: t.ID, AI: t.ai != nil, Flows: t.App(platform.FlowApp) != nil, Member: t.Member, Work: t.Work,
		Trace: func(record func(platform.DecisionTrace)) func(platform.DecisionTrace) {
			previous := t.simulationTrace
			t.simulationTrace = record
			return previous
		},
		Read: func(actor platform.Member, typ, id string, now time.Time) (json.RawMessage, *kernel.Error) {
			return t.caller(actor, t.App(build.ID), false).ReadRecord(typ, id, now)
		},
		Flow: func(id string) (platform.FlowInstance, bool) {
			return platform.Get[platform.FlowInstance](t.automation(platform.FlowApp, false), id)
		},
		Current: func(actor platform.Member, typ, id string, now time.Time) *kernel.Error {
			_, refusal := t.recordOf(actor, typ, id, now)
			return refusal
		},
		Owner: func(schema string) string {
			if owner := t.App(t.owner["action:"+schema]); owner != nil {
				return owner.Manifest().ID
			}
			return ""
		},
		Submit: func(actor platform.Member, s *pb.Submission, now time.Time) (*kernel.Error, []platform.FieldIssue) {
			_, refusal, issues := t.submitDiagnosed(actor, s, now)
			return refusal, issues
		},
		Compute: func(actor platform.Member, fixture platform.ComputeFixture, now time.Time) (bool, error) {
			return settleComputeFixture(t, actor, fixture, now)
		},
		Function: func(actor platform.Member, fixture platform.FunctionFixture, now time.Time) (bool, error) {
			return settleFunctionFixture(t, actor, fixture, now)
		},
		Records: func(actor platform.Member, typ string, q platform.Query, now time.Time) ([]any, int, *kernel.Error) {
			page, refusal := t.recordsRead(actor, typ, q, now)
			return page.Records, page.Total, refusal
		},
		Inbox: func(actor platform.Member) ([]platform.WorkTask, bool) {
			inbox, err := t.read(actor, "inbox")
			if err != nil {
				return nil, false
			}
			return inbox.([]platform.WorkTask), true
		},
		Waiting: func() (bool, bool) { return len(pendingComputeEffects(t)) != 0, len(pendingFunctionCalls(t)) != 0 },
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
			if t.procs != nil {
				return t.procs.Check()
			}
			return nil
		}}
}
