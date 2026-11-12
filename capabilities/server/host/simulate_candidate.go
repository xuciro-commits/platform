package host

import (
	"context"
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	trialowner "platformserver/services/devops/trials"
	"platformserver/services/resources/authoring"
	"slices"
	"time"
)

type stagedCandidate struct {
	candidate platform.ReleaseCandidate
	reader    *Tenant
	drafts    []build.JointDraftRef
}

func (t *Tenant) SimulateCandidate(builder platform.Member, request trialowner.CandidateSimulationRequest) (trialowner.CandidateSimulation, *kernel.Error) {
	return recordRead(t, context.Background(), func(t *Tenant) (trialowner.CandidateSimulation, *kernel.Error) {
		return t.scopedSimulateCandidate(builder, request)
	})
}

func (t *Tenant) scopedSimulateCandidate(builder platform.Member, request trialowner.CandidateSimulationRequest) (trialowner.CandidateSimulation, *kernel.Error) {
	return t.simulateCandidate(builder, request, nil)
}

func (t *Tenant) simulateCandidate(builder platform.Member, request trialowner.CandidateSimulationRequest, staged *stagedCandidate) (trialowner.CandidateSimulation, *kernel.Error) {
	return recordRead(t, context.Background(), func(t *Tenant) (trialowner.CandidateSimulation, *kernel.Error) {
		return t.simulateStagedCandidateRead(builder, request, staged)
	})
}

func (t *Tenant) simulateStagedCandidateRead(builder platform.Member, request trialowner.CandidateSimulationRequest, staged *stagedCandidate) (trialowner.CandidateSimulation, *kernel.Error) {
	var empty trialowner.CandidateSimulation
	if err := t.Admits(builder); err != nil {
		return empty, err
	}
	caller := platform.NewCaller(runtime{t: t}, builder, build.ID, false, false)
	if err := authorizationRefusal(t.authorizeNative(caller, "POST /v1/simulate/candidate", "read", []string{build.Builder}, true)); err != nil {
		return empty, err
	}
	if staged != nil {
		request.ObjectID, request.ProcessID, request.ApplicationID, request.Roots, request.CandidateID = "", "", "", nil, ""
		if request.FunctionID == "" {
			request.CandidateID = staged.candidate.ID
		}
	}
	roots := 0
	for _, root := range []string{request.ObjectID, request.ProcessID, request.FunctionID, request.ApplicationID, request.CandidateID} {
		if root != "" {
			roots++
		}
	}
	if len(request.Roots) > 0 {
		roots++
		seen := map[platform.AssetRef]bool{}
		for _, root := range request.Roots {
			if root.Check() != nil || root.App != build.ID || root.Kind != platform.AssetObject && root.Kind != platform.AssetFlow || seen[root] {
				return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Test roots must be distinct objects or workflows")
			}
			seen[root] = true
		}
		if len(request.Roots) > 16 {
			return empty, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
		}
	}
	if roots != 1 || len(request.Model) > 256 || request.At.IsZero() || len(request.Steps) == 0 || len(request.Steps) > 20 || len(request.Actors) > 3 || len(request.Sample) > 64 || len(request.Actors) > 0 && request.As != "" {
		return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose one saved application, object, process, function or candidate, a fixed time and 1–20 test steps")
	}
	if t.quarantined() {
		return empty, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	m := builder
	if len(request.Actors) > 0 {
		request.As = request.Actors[0]
	}
	if request.As != "" {
		var ok bool
		m, ok = t.member(request.As)
		if !ok {
			return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "{member} is not a member here", request.As)
		}
	}
	members := []platform.Member{m}
	seenActors := map[string]bool{}
	for _, id := range request.Actors {
		if seenActors[id] {
			return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose distinct known members for role comparison")
		}
		seenActors[id] = true
		if id == "" {
			return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose known members for role comparison")
		}
		if slices.ContainsFunc(members, func(m platform.Member) bool { return m.ID == id }) {
			continue
		}
		actor, ok := t.member(id)
		if !ok {
			return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "{member} is not a member here", id)
		}
		members = append(members, actor)
	}
	for _, step := range request.Steps {
		if len(request.Actors) > 0 && step.As != "" {
			return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Role comparisons use the same member for every scenario step")
		}
		if step.Compute != nil && !step.Compute.Check() {
			return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Use a bounded compute fixture with a completed or failed outcome")
		}
		if step.Function != nil && !step.Function.Check() {
			return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Use a bounded function fixture with a ready or rejected outcome")
		}
		if step.AdvanceSeconds < 0 || step.AdvanceSeconds > 86400 {
			return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Advance each test step by 0–86400 fixed seconds")
		}
		if step.As != "" && !slices.ContainsFunc(members, func(m platform.Member) bool { return m.ID == step.As }) {
			actor, ok := t.member(step.As)
			if !ok {
				return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "{member} is not a member here", step.As)
			}
			members = append(members, actor)
		}
	}
	kind, id := platform.AssetObject, request.ObjectID
	if request.ProcessID != "" {
		kind, id = platform.AssetFlow, request.ProcessID
	}
	if request.FunctionID != "" {
		kind, id = platform.AssetFunction, request.FunctionID
	}
	drafts := []build.JointDraftRef{{Kind: kind, ID: id}}
	var candidate platform.ReleaseCandidate
	var err error
	diagnostic := ""
	reader := t
	if staged != nil {
		reader = staged.reader
	}
	if staged != nil && request.FunctionID == "" {
		candidate = staged.candidate
	} else if request.CandidateID != "" {
		candidate, err = t.releases.Candidate(request.CandidateID)
	} else {
		if len(request.Roots) > 0 {
			owner, ok := t.App(build.ID).(build.ConstructionDrafts)
			if !ok {
				err = fmt.Errorf("Construction draft owner is unavailable")
			} else {
				drafts, err = owner.RootDrafts(request.Roots)
			}
		}
		if request.ApplicationID != "" {
			owner, ok := t.App(build.ID).(build.ConstructionDrafts)
			if !ok {
				err = fmt.Errorf("Construction draft owner is unavailable")
			} else {
				drafts, err = owner.ApplicationDrafts(request.ApplicationID)
			}
		}
		if staged != nil {
			if root := (build.JointDraftRef{Kind: kind, ID: id}); !slices.Contains(staged.drafts, root) {
				drafts = append(slices.Clone(staged.drafts), root)
			} else {
				drafts = staged.drafts
			}
		}
		if err == nil {
			review, prepared, problem := reader.previewReleaseLocked(drafts, builder)
			candidate, err, diagnostic = prepared, problem, review.Diagnostic
		}
	}
	if request.Sample != "" && err == nil {
		found := false
		for _, asset := range candidate.Assets {
			if asset.Ref.Kind == platform.AssetSampleSet && asset.Ref.Name == request.Sample {
				var sample platform.SampleSet
				if json.Unmarshal(asset.Body, &sample) != nil || sample.Check() != nil {
					err = fmt.Errorf("Invalid application sample asset")
					break
				}
				found = true
				if request.Samples == nil {
					for _, group := range sample.Samples {
						request.Samples = append(request.Samples, trialowner.SimulationSample{Type: group.Type, Records: group.Records})
					}
				}
				break
			}
		}
		if !found && err == nil {
			err = fmt.Errorf("Choose a sample asset in this application's draft closure")
		}
	}
	functionName := ""
	if request.FunctionID != "" {
		selected, _ := platform.Get[build.Function](reader.automation(build.ID, false), request.FunctionID)
		functionName = selected.Name
	}
	if err == nil && diagnostic != "" {
		err = fmt.Errorf("%s", diagnostic)
	}
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
	modules := environment.Modules
	for _, asset := range candidate.Assets {
		if asset.Ref.Kind == platform.AssetCompute {
			var operation platform.Operation
			if json.Unmarshal(asset.Body, &operation) != nil {
				return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Candidate compute descriptor is invalid")
			}
			if operation.Binding.Kind == "wasm" {
				raw, err := t.readArtifact(context.Background(), operation.Binding.Module)
				if err != nil {
					return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Candidate module bytes are unavailable")
				}
				modules[operation.Binding.Module] = raw
			}
		}
	}
	out, problem := runCandidateSimulation(request, candidate, environment, m, members, functionName)
	if problem != nil {
		return empty, problem
	}
	if len(request.Actors) > 0 {
		out.Actors = []trialowner.SimulationActor{{Member: m.ID, Steps: out.Steps, TestID: out.TestID, Recovered: out.Recovered, Passed: out.Passed}}
		for _, id := range request.Actors[1:] {
			actor := members[slices.IndexFunc(members, func(m platform.Member) bool { return m.ID == id })]
			other, err := runCandidateSimulation(request, candidate, environment, actor, members, functionName)
			if err != nil {
				return empty, err
			}
			out.Actors = append(out.Actors, trialowner.SimulationActor{Member: actor.ID, Steps: other.Steps, TestID: other.TestID, Recovered: other.Recovered, Passed: other.Passed})
		}
	}
	return out, nil
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
			if owner := t.owner["action:"+schema]; owner != nil {
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
		Waiting: func() (bool, bool) { return len(pendingComputeEffects(t)) != 0, len(pendingFunctionEffects(t)) != 0 },
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
