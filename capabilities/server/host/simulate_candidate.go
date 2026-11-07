package host

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	trialowner "platformserver/services/devops/trials"
	"platformserver/services/resources/authoring"
	"slices"
	"strings"
	"time"
)

type stagedCandidate struct {
	candidate platform.ReleaseCandidate
	reader    *Tenant
	drafts    []build.JointDraftRef
}

func (t *Tenant) SimulateCandidate(builder platform.Member, request trialowner.CandidateSimulationRequest) (trialowner.CandidateSimulation, *kernel.Error) {
	return t.simulateCandidate(builder, request, nil)
}

func (t *Tenant) simulateCandidate(builder platform.Member, request trialowner.CandidateSimulationRequest, staged *stagedCandidate) (trialowner.CandidateSimulation, *kernel.Error) {
	var empty trialowner.CandidateSimulation
	if err := t.admits(builder); err != nil {
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
	t.mu.Lock()
	if t.quarantined() {
		t.mu.Unlock()
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
			t.mu.Unlock()
			return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "{member} is not a member here", request.As)
		}
	}
	members := []platform.Member{m}
	seenActors := map[string]bool{}
	for _, id := range request.Actors {
		if seenActors[id] {
			t.mu.Unlock()
			return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose distinct known members for role comparison")
		}
		seenActors[id] = true
		if id == "" {
			t.mu.Unlock()
			return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose known members for role comparison")
		}
		if slices.ContainsFunc(members, func(m platform.Member) bool { return m.ID == id }) {
			continue
		}
		actor, ok := t.member(id)
		if !ok {
			t.mu.Unlock()
			return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "{member} is not a member here", id)
		}
		members = append(members, actor)
	}
	for _, step := range request.Steps {
		if len(request.Actors) > 0 && step.As != "" {
			t.mu.Unlock()
			return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Role comparisons use the same member for every scenario step")
		}
		if step.Compute != nil && !step.Compute.Check() {
			t.mu.Unlock()
			return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Use a bounded compute fixture with a completed or failed outcome")
		}
		if step.Function != nil && !step.Function.Check() {
			t.mu.Unlock()
			return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Use a bounded function fixture with a ready or rejected outcome")
		}
		if step.AdvanceSeconds < 0 || step.AdvanceSeconds > 86400 {
			t.mu.Unlock()
			return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Advance each test step by 0–86400 fixed seconds")
		}
		if step.As != "" && !slices.ContainsFunc(members, func(m platform.Member) bool { return m.ID == step.As }) {
			actor, ok := t.member(step.As)
			if !ok {
				t.mu.Unlock()
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
			owner, ok := t.app(build.ID).(build.ConstructionDrafts)
			if !ok {
				err = fmt.Errorf("Construction draft owner is unavailable")
			} else {
				drafts, err = owner.RootDrafts(request.Roots)
			}
		}
		if request.ApplicationID != "" {
			owner, ok := t.app(build.ID).(build.ConstructionDrafts)
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
	t.mu.Unlock()
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
		return &candidateRun{request: request, candidate: candidate, sandbox: sandbox, m: m, now: request.At}, nil
	}
	return trialowner.RunCandidate(request, candidate, m, members, functionName, compose)
}

type candidateRun struct {
	request   trialowner.CandidateSimulationRequest
	candidate platform.ReleaseCandidate
	sandbox   *Tenant
	m         platform.Member
	now       time.Time
}

func (r *candidateRun) Step(i int, step trialowner.SimulationStep) (trialowner.Simulation, *kernel.Error) {
	actor := r.m
	if step.As != "" {
		actor, _ = r.sandbox.Member(step.As)
	}
	before := map[string]trialowner.SimulatedChange{}
	if r.request.ApplicationID != "" || r.request.CandidateID != "" || len(r.request.Roots) > 0 {
		for _, change := range r.visibleChanges(actor) {
			before[change.Type+"/"+change.ID] = change
		}
	}
	trace := trialowner.Trace{Events: []platform.DecisionTrace{}}
	r.sandbox.simulationTrace = trace.Record
	defer func() { r.sandbox.simulationTrace = nil }()
	var read json.RawMessage
	var refusal *kernel.Error
	var issues []platform.FieldIssue
	var sub *pb.Submission
	if step.Read {
		if step.Action != "" || step.Answer != "" || step.AdvanceSeconds != 0 || step.ID == "" || !slices.ContainsFunc(r.candidate.Assets, func(a platform.ReleaseAsset) bool {
			return a.Ref.Kind == platform.AssetObject && a.Ref.Name == step.Type
		}) {
			return trialowner.Simulation{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A read step needs a candidate object and record ID")
		}
		caller := r.sandbox.caller(actor, r.sandbox.app(build.ID), false)
		read, refusal = caller.ReadRecord(step.Type, step.ID, r.now)
	} else if step.Answer != "" {
		if step.Action != "" || step.Flow == "" || step.Step == "" || step.ID == "" {
			return trialowner.Simulation{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "An answer test step needs a r.candidate flow, source record and ask step")
		}
		var found bool
		for _, asset := range r.candidate.Assets {
			if asset.Ref.Kind == platform.AssetFlow && asset.Ref.Name == step.Flow {
				var envelope platform.FlowReleaseDescriptor
				_ = json.Unmarshal(asset.Body, &envelope)
				found = envelope.Subject.Name == step.Type
			}
		}
		if !found {
			return trialowner.Simulation{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The answer must belong to a flow and object in this r.candidate")
		}
		instance, exists := platform.Get[platform.FlowInstance](r.sandbox.automation(platform.FlowApp, false), step.Flow+":"+step.ID)
		taskID := ""
		if exists {
			for _, token := range instance.Tokens {
				if token.Step == step.Step && token.Waits == "ask" {
					taskID = token.Task
				}
			}
		}
		if taskID == "" {
			refusal = platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "This test record has no waiting task at that step")
		} else {
			payload, _ := json.Marshal(map[string]string{"answer": step.Answer})
			sub = &pb.Submission{Authority: platform.WorkApp, Target: &pb.EntityRef{Type: platform.WorkTaskType, Id: taskID}, Schema: &pb.SchemaRef{Name: "work.task.complete", Version: 1}, Payload: payload}
		}
	} else if step.Type == build.ProcessType && step.Action == build.SchemaProcessRun && step.ID == r.request.ProcessID {
		processID := ""
		for _, asset := range r.candidate.Assets {
			if asset.Ref.Kind == platform.AssetFlow {
				var definition platform.FlowReleaseDescriptor
				json.Unmarshal(asset.Body, &definition)
				processID = definition.Name
				break
			}
		}
		sub = &pb.Submission{Authority: build.ID, Target: &pb.EntityRef{Type: step.Type, Id: processID}, Schema: &pb.SchemaRef{Name: step.Action, Version: 1}, Payload: step.Payload}
	} else if step.Type == build.ProcessType && step.Action == "" && step.AdvanceSeconds > 0 && r.request.ProcessID != "" {
		var instanceExists bool
		for _, asset := range r.candidate.Assets {
			if asset.Ref.Kind == platform.AssetFlow {
				var definition platform.FlowReleaseDescriptor
				json.Unmarshal(asset.Body, &definition)
				if _, ok := platform.Get[platform.FlowInstance](r.sandbox.automation(platform.FlowApp, false), definition.Name+":"+step.ID); ok {
					instanceExists = true
				}
			}
		}
		if !instanceExists {
			refusal = platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The manual test has no run with this key")
		}
	} else if step.Action == "" && step.AdvanceSeconds > 0 && step.Flow == "" && step.Step == "" {
		if !slices.ContainsFunc(r.candidate.Assets, func(a platform.ReleaseAsset) bool {
			return a.Ref.Kind == platform.AssetObject && a.Ref.Name == step.Type
		}) || step.ID == "" {
			return trialowner.Simulation{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A clock step needs a r.candidate object and record ID")
		}
		_, refusal = r.sandbox.recordOf(actor, step.Type, step.ID, r.now)
	} else {
		action := r.sandbox.owner["action:"+step.Action]
		business := slices.ContainsFunc(r.candidate.Assets, func(asset platform.ReleaseAsset) bool {
			return asset.Ref.App == build.ID && asset.Ref.Kind == platform.AssetObject && asset.Ref.Name == step.Type
		})
		function := step.Type == build.FunctionCallType && slices.ContainsFunc(r.candidate.Assets, func(asset platform.ReleaseAsset) bool { return asset.Ref.Kind == platform.AssetFunction })
		if (!business && !function) || action == nil || step.ID == "" || action.Manifest().ID != build.ID || !strings.HasPrefix(step.Type, build.ID+".") ||
			step.Type == build.ObjectType || step.Type == build.PageType || step.Type == build.AppType || step.Type == build.TestPlanType || step.Type == build.ProcessType || step.Type == build.FunctionType {
			return trialowner.Simulation{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Test step {step} must name an action and record of the r.candidate objects", fmt.Sprint(i+1))
		}
		payload := step.Payload
		if len(payload) == 0 {
			payload = json.RawMessage(`{}`)
		}
		sub = &pb.Submission{Authority: build.ID, Target: &pb.EntityRef{Type: step.Type, Id: step.ID},
			Schema: &pb.SchemaRef{Name: step.Action, Version: 1}, Payload: payload}
	}
	if sub != nil {
		sub.TenantId, sub.PrincipalId, sub.IdempotencyKey = r.sandbox.ID, actor.ID, fmt.Sprintf("test-%d", i+1)
		_, refusal, issues = r.sandbox.submitDiagnosed(actor, sub, r.now)
	}
	if step.AdvanceSeconds > 0 {
		r.now = r.now.Add(time.Duration(step.AdvanceSeconds) * time.Second)
		r.sandbox.Work(r.now)
	}
	result := trialowner.Simulation{Accepted: refusal == nil, Changes: []trialowner.SimulatedChange{}, Input: &step, Read: read, RolledBack: refusal != nil && sub != nil}
	if step.Compute != nil {
		matched, err := settleComputeFixture(r.sandbox, actor, *step.Compute, r.now)
		if err != nil {
			return trialowner.Simulation{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The compute fixture cannot run: {why}", err.Error())
		}
		result.ComputeMatched = &matched
	}
	if step.Function != nil {
		matched, err := settleFunctionFixture(r.sandbox, actor, *step.Function, r.now)
		if err != nil {
			return trialowner.Simulation{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The function fixture cannot run: {why}", err.Error())
		}
		result.FunctionMatched = &matched
	}
	if r.sandbox.ai != nil {
		page, problem := r.sandbox.recordsRead(actor, build.FunctionCallType, platform.Query{Limit: 100}, r.now)
		if problem == nil {
			for _, record := range page.Records {
				result.Functions = append(result.Functions, record.(build.FunctionRun))
			}
		}
	}
	if refusal != nil {
		result.Refusal = refusal.Message
		result.RefusalCode = refusal.Code.String()
		result.Issues = issues
		result.Unsupported = platform.IsOutsideAcceptedBoundary(refusal)
	} else {
		for _, change := range r.visibleChanges(actor) {
			prior, exists := before[change.Type+"/"+change.ID]
			if r.request.ApplicationID != "" || r.request.CandidateID != "" || len(r.request.Roots) > 0 {
				if exists && bytes.Equal(prior.Record, change.Record) {
					continue
				}
				change.Before = prior.Record
			}
			result.Changes = append(result.Changes, change)
		}

	}
	if r.sandbox.app(platform.FlowApp) != nil {
		page, _ := r.sandbox.recordsRead(actor, platform.FlowInstanceType, platform.Query{Limit: 100}, r.now)
		for _, record := range page.Records {
			raw, _ := json.Marshal(record)
			var instance platform.FlowInstance
			_ = json.Unmarshal(raw, &instance)
			result.Flows = append(result.Flows, trialowner.SimulatedFlow{ID: instance.ID, Flow: instance.Flow, Version: instance.Version, Dependencies: instance.Dependencies, Release: instance.Release, State: instance.State, Tokens: instance.Tokens, Trace: instance.Trace, Outputs: instance.Outputs})
		}
		if inbox, err := r.sandbox.read(actor, "inbox"); err == nil {
			result.Tasks = inbox.([]platform.WorkTask)
		}
	}
	result.Trace = trace.Events
	result.TraceTruncated = trace.Truncated
	return result, nil
}

func (r *candidateRun) visibleChanges(actor platform.Member) []trialowner.SimulatedChange {
	trace := r.sandbox.simulationTrace
	r.sandbox.simulationTrace = nil
	defer func() { r.sandbox.simulationTrace = trace }()
	changes := []trialowner.SimulatedChange{}
	for _, asset := range r.candidate.Assets {
		if asset.Ref.Kind != platform.AssetObject {
			continue
		}
		offset := 0
		for {
			page, err := r.sandbox.recordsRead(actor, asset.Ref.Name, platform.Query{Limit: 500, Offset: offset, Archived: true, Sort: []string{"id"}}, r.now)
			if err != nil {
				break
			}
			for _, record := range page.Records {
				raw, _ := json.Marshal(record)
				var identity struct {
					ID string `json:"id"`
				}
				_ = json.Unmarshal(raw, &identity)
				changes = append(changes, trialowner.SimulatedChange{Type: asset.Ref.Name, ID: identity.ID, Record: raw})
			}
			offset += len(page.Records)
			if offset >= page.Total || len(page.Records) == 0 {
				break
			}
		}
	}
	return changes
}

func (r *candidateRun) HasAI() bool { return r.sandbox.ai != nil }

func (r *candidateRun) Pending() (compute, functions bool) {
	return len(pendingComputeEffects(r.sandbox)) != 0, len(pendingFunctionEffects(r.sandbox)) != 0
}

func (r *candidateRun) Fault() string {
	if fault := r.sandbox.fault.Load(); fault != nil {
		return fault.Reason
	}
	return ""
}

func (r *candidateRun) Snapshot() (json.RawMessage, error) {
	saved, _, err := r.sandbox.Snapshot(func() int64 { return 0 })
	return saved, err
}

func (r *candidateRun) Restore(saved json.RawMessage) error { return r.sandbox.Restore(saved) }

func (r *candidateRun) Check() error {
	if r.sandbox.procs != nil {
		return r.sandbox.procs.Check()
	}
	return nil
}
