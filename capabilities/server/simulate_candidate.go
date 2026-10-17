package platformserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

	build "platformserver/apps/build"

	"platformserver/platform"
)

// CandidateSimulationRequest fixes all business inputs of an isolated run.
// Create actions establish its sample data; nothing is copied from live rows.
type CandidateSimulationRequest struct {
	Roots         []platform.AssetRef `json:"roots,omitempty"`
	ApplicationID string              `json:"applicationId,omitempty"`
	CandidateID   string              `json:"candidateId,omitempty"`
	Actors        []string            `json:"actors,omitempty"`
	Sample        string              `json:"sample,omitempty"`
	ObjectID      string              `json:"objectId,omitempty"`
	ProcessID     string              `json:"processId,omitempty"`
	FunctionID    string              `json:"functionId,omitempty"`
	Model         string              `json:"model,omitempty"`
	As            string              `json:"as,omitempty"`
	At            time.Time           `json:"at"`
	Steps         []SimulationStep    `json:"steps"`
	Samples       []SimulationSample  `json:"samples,omitempty"`
}

type SimulationStep struct {
	Read           bool            `json:"read,omitempty"`
	Type           string          `json:"type"`
	ID             string          `json:"id"`
	Action         string          `json:"action"`
	Payload        json.RawMessage `json:"payload"`
	Expect         string          `json:"expect,omitempty"`
	As             string          `json:"as,omitempty"`
	AdvanceSeconds int             `json:"advanceSeconds,omitempty"`
	// Answer identifies a candidate flow's native ask token, never an arbitrary
	// production task. The ordinary work action still checks the chosen actor.
	Flow     string                 `json:"flow,omitempty"`
	Step     string                 `json:"step,omitempty"`
	Answer   string                 `json:"answer,omitempty"`
	Function *build.FunctionFixture `json:"function,omitempty"`
	Compute  *build.ComputeFixture  `json:"compute,omitempty"`
}

type SimulationActor struct {
	Member    string       `json:"member"`
	Steps     []Simulation `json:"steps"`
	TestID    string       `json:"testId"`
	Recovered bool         `json:"recovered"`
	Passed    *bool        `json:"passed,omitempty"`
}

type CandidateSimulation struct {
	Actors      []SimulationActor `json:"actors,omitempty"`
	CandidateID string            `json:"candidateId"`
	TestID      string            `json:"testId,omitempty"`
	Model       string            `json:"model,omitempty"`
	Fixture     bool              `json:"fixture,omitempty"`
	Steps       []Simulation      `json:"steps"`
	Recovered   bool              `json:"recovered"`
	Passed      *bool             `json:"passed,omitempty"`
}

// SimulateCandidate installs a saved object, process or function draft in a fresh, bounded tenant.
// Only this builder's objects are supported. All action decisions and result
// reads use the ordinary runtime, with no live records or external bindings.
func (t *Tenant) SimulateCandidate(builder platform.Member, request CandidateSimulationRequest) (CandidateSimulation, *kernel.Error) {
	var empty CandidateSimulation
	if err := t.admits(builder); err != nil {
		return empty, err
	}
	caller := platform.NewCaller(runtime{t}, builder, build.ID, false, false)
	if err := authorizationRefusal(t.authorizeNative(caller, "POST /v1/simulate/candidate", "read", []string{build.Builder}, true)); err != nil {
		return empty, err
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
	if request.CandidateID != "" {
		candidate, err = t.releases.candidate(request.CandidateID)
	} else {
		if len(request.Roots) > 0 {
			owner, ok := t.app(build.ID).(ConstructionDrafts)
			if !ok {
				err = fmt.Errorf("Construction draft owner is unavailable")
			} else {
				drafts, err = owner.RootDrafts(request.Roots)
			}
		}
		if request.ApplicationID != "" {
			owner, ok := t.app(build.ID).(ConstructionDrafts)
			if !ok {
				err = fmt.Errorf("Construction draft owner is unavailable")
			} else {
				drafts, err = owner.ApplicationDrafts(request.ApplicationID)
			}
		}
		if err == nil {
			review, prepared, problem := t.previewReleaseLocked(drafts, builder)
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
						request.Samples = append(request.Samples, SimulationSample{Type: group.Type, Records: group.Records})
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
		selected, _ := platform.Get[build.Function](t.automation(build.ID, false), request.FunctionID)
		functionName = selected.Name
	}
	if err == nil && diagnostic != "" {
		err = fmt.Errorf("%s", diagnostic)
	}
	environment, environmentErr := t.simulationEnvironment(candidate, request.Samples)
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
		out.Actors = []SimulationActor{{Member: m.ID, Steps: out.Steps, TestID: out.TestID, Recovered: out.Recovered, Passed: out.Passed}}
		for _, id := range request.Actors[1:] {
			actor := members[slices.IndexFunc(members, func(m platform.Member) bool { return m.ID == id })]
			other, err := runCandidateSimulation(request, candidate, environment, actor, members, functionName)
			if err != nil {
				return empty, err
			}
			out.Actors = append(out.Actors, SimulationActor{Member: actor.ID, Steps: other.Steps, TestID: other.TestID, Recovered: other.Recovered, Passed: other.Passed})
		}
	}
	return out, nil
}

func runCandidateSimulation(request CandidateSimulationRequest, candidate platform.ReleaseCandidate, environment simulationEnvironment, m platform.Member, members []platform.Member, functionName string) (CandidateSimulation, *kernel.Error) {
	var empty CandidateSimulation
	var err error
	// The constructor takes exact owner-produced bytes, not callbacks closing
	// over the production Build or its host. Each invocation owns every app.
	compose := func() (*Tenant, error) {
		sandbox, err := candidateTestTenantWithEnvironment(candidate, m, environment, slices.DeleteFunc(slices.Clone(members), func(actor platform.Member) bool { return actor.ID == m.ID })...)
		if err == nil && sandbox.ai != nil {
			err = configureFunctionFixtureModel(sandbox, request.Model, request.At)
		}
		return sandbox, err
	}
	sandbox, err := compose()
	if err != nil {
		return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The candidate cannot be tested: {why}", err.Error())
	}
	out := CandidateSimulation{CandidateID: candidate.ID, Steps: []Simulation{}}
	out.TestID, err = canonicalDigest([]any{candidate.ID, request, members, m.ID})
	if err != nil {
		return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The candidate cannot be tested: {why}", err.Error())
	}
	if sandbox.ai != nil {
		out.TestID, err = canonicalDigest([]any{candidate.ID, request, members, m.ID})
		if err != nil {
			return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The candidate cannot be tested: {why}", err.Error())
		}
		out.Model, out.Fixture = request.Model, true
	}
	r := &candidateRun{request: request, candidate: candidate, sandbox: sandbox, m: m, functionName: functionName, now: request.At, passed: true, out: out}
	for i, step := range request.Steps {
		if err := r.step(i, step); err != nil {
			return empty, err
		}
	}
	out, passed, asserted := r.out, r.passed, r.asserted
	if request.FunctionID != "" && !r.functionAttempted {
		return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A function test must call the selected candidate function")
	}
	if len(pendingComputeEffects(sandbox)) != 0 {
		return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Provide a fixed answer for every pending compute call")
	}
	if len(pendingFunctionEffects(sandbox)) != 0 {
		return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Provide a fixed answer for every pending function call")
	}
	if asserted == len(request.Steps) {
		out.Passed = &passed
	}
	if fault := sandbox.fault.Load(); fault != nil {
		return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Candidate execution failed: {why}", fault.Reason)
	}
	saved, _, err := sandbox.Snapshot(func() int64 { return 0 })
	if err == nil {
		var restored *Tenant
		restored, err = compose()
		if err == nil {
			err = restored.Restore(saved)
		}
		if err == nil && restored.procs != nil {
			err = restored.procs.Check()
		}
		if err == nil {
			var after json.RawMessage
			after, _, err = restored.Snapshot(func() int64 { return 0 })
			out.Recovered = err == nil && bytes.Equal(saved, after)
		}
	}
	if err != nil || !out.Recovered {
		return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The test state did not recover exactly: {why}", fmt.Sprint(err))
	}
	return out, nil
}

// candidateRun is one candidate test in flight: the sandbox tenant, the clock
// and what the steps so far asserted.
type candidateRun struct {
	request           CandidateSimulationRequest
	candidate         platform.ReleaseCandidate
	sandbox           *Tenant
	m                 platform.Member
	functionName      string
	now               time.Time
	passed            bool
	asserted          int
	functionAttempted bool
	out               CandidateSimulation
}

// step runs test step i in the sandbox and appends its result.
func (r *candidateRun) step(i int, step SimulationStep) *kernel.Error {
	if r.request.FunctionID != "" && step.Action == build.SchemaFunctionCall {
		var call platform.FunctionRequest
		if json.Unmarshal(step.Payload, &call) == nil {
			r.functionAttempted = r.functionAttempted || call.Name == r.functionName
		}
	}
	if step.Expect != "" && step.Expect != "accepted" && step.Expect != "refused" {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose accepted or refused as the expected test outcome")
	}
	actor := r.m
	if step.As != "" {
		actor, _ = r.sandbox.Member(step.As)
	}
	before := map[string]SimulatedChange{}
	if r.request.ApplicationID != "" || r.request.CandidateID != "" || len(r.request.Roots) > 0 {
		for _, change := range r.visibleChanges(actor) {
			before[change.Type+"/"+change.ID] = change
		}
	}
	trace := []platform.DecisionTrace{}
	truncated := false
	traceBytes := 0
	deniedReads := map[string]bool{}
	r.sandbox.simulationTrace = func(event platform.DecisionTrace) {
		// Row filtering may ask about rows the actor cannot see. Explain the
		// original denial once without revealing those rows' IDs or their count.
		if event.Request != nil && event.Authorization != nil && event.Request.Kind == "read" && !event.Authorization.Allow {
			question := *event.Request
			question.Record = ""
			question.From = ""
			event.Request = &question
			key := string(platform.Raw(event))
			if deniedReads[key] {
				return
			}
			deniedReads[key] = true
		}
		if len(trace) >= 512 || traceBytes >= 1<<20 {
			truncated = true
			return
		}
		raw, _ := json.Marshal(event)
		if len(raw) > 64<<10 {
			event.Reads = nil
			event.Before = nil
			event.After = nil
			truncated = true
			raw, _ = json.Marshal(event)
		}
		if len(raw) > 64<<10 || traceBytes+len(raw) > 1<<20 {
			truncated = true
			return
		}
		traceBytes += len(raw)
		trace = append(trace, event)
	}
	defer func() { r.sandbox.simulationTrace = nil }()
	var read json.RawMessage
	var refusal *kernel.Error
	var sub *pb.Submission
	if step.Read {
		if step.Action != "" || step.Answer != "" || step.AdvanceSeconds != 0 || step.ID == "" || !slices.ContainsFunc(r.candidate.Assets, func(a platform.ReleaseAsset) bool {
			return a.Ref.Kind == platform.AssetObject && a.Ref.Name == step.Type
		}) {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A read step needs a candidate object and record ID")
		}
		caller := r.sandbox.caller(actor, r.sandbox.app(build.ID), false)
		read, refusal = caller.ReadRecord(step.Type, step.ID, r.now)
	} else if step.Answer != "" {
		if step.Action != "" || step.Flow == "" || step.Step == "" || step.ID == "" {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "An answer test step needs a r.candidate flow, source record and ask step")
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
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The answer must belong to a flow and object in this r.candidate")
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
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A clock step needs a r.candidate object and record ID")
		}
		_, refusal = r.sandbox.RecordOf(actor, step.Type, step.ID, r.now)
	} else {
		action := r.sandbox.owner["action:"+step.Action]
		business := slices.ContainsFunc(r.candidate.Assets, func(asset platform.ReleaseAsset) bool {
			return asset.Ref.App == build.ID && asset.Ref.Kind == platform.AssetObject && asset.Ref.Name == step.Type
		})
		function := step.Type == build.FunctionCallType && slices.ContainsFunc(r.candidate.Assets, func(asset platform.ReleaseAsset) bool { return asset.Ref.Kind == platform.AssetFunction })
		if (!business && !function) || action == nil || step.ID == "" || action.Manifest().ID != build.ID || !strings.HasPrefix(step.Type, build.ID+".") ||
			step.Type == build.ObjectType || step.Type == build.PageType || step.Type == build.AppType || step.Type == build.TestPlanType || step.Type == build.ProcessType || step.Type == build.FunctionType {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Test step {step} must name an action and record of the r.candidate objects", fmt.Sprint(i+1))
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
		_, refusal = r.sandbox.Submit(actor, sub, r.now)
	}
	if step.AdvanceSeconds > 0 {
		r.now = r.now.Add(time.Duration(step.AdvanceSeconds) * time.Second)
		r.sandbox.Work(r.now)
	}
	result := Simulation{Accepted: refusal == nil, Changes: []SimulatedChange{}, Input: &step, Read: read, RolledBack: refusal != nil && sub != nil}
	if step.Compute != nil {
		matched, err := settleComputeFixture(r.sandbox, actor, *step.Compute, r.now)
		if err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The compute fixture cannot run: {why}", err.Error())
		}
		result.ComputeMatched = &matched
		r.passed = r.passed && matched
		r.out.Fixture = true
	}
	if step.Function != nil {
		matched, err := settleFunctionFixture(r.sandbox, actor, *step.Function, r.now)
		if err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The function fixture cannot run: {why}", err.Error())
		}
		result.FunctionMatched = &matched
		r.passed = r.passed && matched
	}
	if r.sandbox.ai != nil {
		page, problem := r.sandbox.Records(actor, build.FunctionCallType, platform.Query{Limit: 100}, r.now)
		if problem == nil {
			for _, record := range page.Records {
				result.Functions = append(result.Functions, record.(build.FunctionRun))
			}
		}
	}
	if step.Expect != "" {
		matched := (step.Expect == "accepted") == result.Accepted
		result.Matched = &matched
		r.passed = r.passed && matched
		r.asserted++
	}
	if refusal != nil {
		result.Refusal = refusal.Message
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
		page, _ := r.sandbox.Records(actor, platform.FlowInstanceType, platform.Query{Limit: 100}, r.now)
		for _, record := range page.Records {
			raw, _ := json.Marshal(record)
			var instance platform.FlowInstance
			_ = json.Unmarshal(raw, &instance)
			result.Flows = append(result.Flows, SimulatedFlow{ID: instance.ID, Flow: instance.Flow, Version: instance.Version, Dependencies: instance.Dependencies, Release: instance.Release, State: instance.State, Tokens: instance.Tokens, Trace: instance.Trace, Outputs: instance.Outputs})
		}
		if inbox, err := r.sandbox.Read(actor, "inbox"); err == nil {
			result.Tasks = inbox.([]platform.WorkTask)
		}
	}
	result.Trace = trace
	result.TraceTruncated = truncated
	r.out.Steps = append(r.out.Steps, result)
	return nil
}

// visibleChanges is a role-filtered projection of this run's synthetic rows.
func (r *candidateRun) visibleChanges(actor platform.Member) []SimulatedChange {
	trace := r.sandbox.simulationTrace
	r.sandbox.simulationTrace = nil
	defer func() { r.sandbox.simulationTrace = trace }()
	changes := []SimulatedChange{}
	for _, asset := range r.candidate.Assets {
		if asset.Ref.Kind != platform.AssetObject {
			continue
		}
		offset := 0
		for {
			page, err := r.sandbox.Records(actor, asset.Ref.Name, platform.Query{Limit: 500, Offset: offset, Archived: true, Sort: []string{"id"}}, r.now)
			if err != nil {
				break
			}
			for _, record := range page.Records {
				raw, _ := json.Marshal(record)
				var identity struct {
					ID string `json:"id"`
				}
				_ = json.Unmarshal(raw, &identity)
				changes = append(changes, SimulatedChange{Type: asset.Ref.Name, ID: identity.ID, Record: raw})
			}
			offset += len(page.Records)
			if offset >= page.Total || len(page.Records) == 0 {
				break
			}
		}
	}
	return changes
}

func candidateTestTenantWithEnvironment(candidate platform.ReleaseCandidate, member platform.Member, environment simulationEnvironment, others ...platform.Member) (*Tenant, error) {
	fresh := func(id string) (platform.App, error) {
		factory := environment.Factories[id]
		if factory == nil {
			return nil, fmt.Errorf("simulation owner %s is not available", id)
		}
		app := factory.NewSimulationApp(member.Tenant)
		if app == nil || app.Manifest().ID != id {
			return nil, fmt.Errorf("simulation owner identity differs")
		}
		return app, nil
	}
	construction, err := fresh(build.ID)
	if err != nil {
		return nil, err
	}
	b, ok := construction.(ConstructionSandbox)
	if !ok {
		return nil, fmt.Errorf("construction owner has no isolated asset installer")
	}
	member.Roles = maps.Clone(member.Roles)
	if environment.Identity == nil {
		return nil, fmt.Errorf("candidate identity factory is unavailable")
	}
	principals := append([]platform.Member{member}, others...)
	identity := environment.Identity.NewSimulationIdentity(member.Tenant, principals)
	if identity == nil || identity.Manifest().ID != PlatformApp {
		return nil, fmt.Errorf("candidate identity factory changed its owner")
	}
	apps := []platform.App{identity}
	hasFunctions := slices.ContainsFunc(candidate.Assets, func(a platform.ReleaseAsset) bool { return a.Ref.Kind == platform.AssetFunction })
	hasFlow := slices.ContainsFunc(candidate.Assets, func(a platform.ReleaseAsset) bool { return a.Ref.Kind == platform.AssetFlow })
	hasProposal := slices.ContainsFunc(candidate.Assets, func(a platform.ReleaseAsset) bool {
		if a.Ref.Kind != platform.AssetFunction {
			return false
		}
		var definition platform.AIFunction
		return json.Unmarshal(a.Body, &definition) == nil && definition.Proposal != nil
	})
	hasApproval := slices.ContainsFunc(candidate.Assets, func(a platform.ReleaseAsset) bool {
		if a.Ref.Kind != platform.AssetObject || a.Ref.App != build.ID {
			return false
		}
		var object build.Object
		return json.Unmarshal(a.Body, &object) == nil && slices.ContainsFunc(object.Actions, func(action build.Action) bool { return action.Approval != nil })
	})
	if (hasProposal || hasApproval) && !hasFlow {
		app, err := fresh(platform.WorkApp)
		if err != nil {
			return nil, err
		}
		apps = append(apps, app)
	}
	if hasFunctions {
		app, err := fresh(platform.AIApp)
		if err != nil {
			return nil, err
		}
		apps = append(apps, app)
	}
	if slices.ContainsFunc(candidate.Assets, func(a platform.ReleaseAsset) bool { return a.Ref.Kind == platform.AssetFlow }) {
		for _, id := range []string{platform.WorkApp, platform.FlowApp} {
			app, err := fresh(id)
			if err != nil {
				return nil, err
			}
			apps = append(apps, app)
		}
	}
	if slices.ContainsFunc(candidate.Assets, func(a platform.ReleaseAsset) bool { return a.Ref.Kind == platform.AssetAgent }) {
		app, err := fresh(AgentApp)
		if err != nil {
			return nil, err
		}
		apps = append(apps, app)
	}
	for _, manifest := range environment.Manifests {
		apps = append(apps, newSampleOwner(member.Tenant, manifest))
	}
	apps = append(apps, b)
	sandbox, err := NewTenant(member.Tenant, apps...)
	if err != nil {
		return nil, err
	}
	sandbox.simulation = true
	// Trials execute these exact frozen definitions. Passive integrations have
	// no live connections to reconstruct their release descriptors from.
	if err := sandbox.releases.put(candidate.ID, candidate.Bytes); err != nil {
		return nil, err
	}
	sandbox.releases.active = candidate.ID
	sandbox.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { return e.Body, nil }
	for digest, raw := range environment.Modules {
		if err := sandbox.files().Put(context.Background(), artifactKey(sandbox.ID, digest), raw, "application/wasm"); err != nil {
			return nil, err
		}
	}
	for _, asset := range candidate.Assets {
		if asset.Ref.App == build.ID && asset.Ref.Kind == platform.AssetPropertyType {
			if err := b.InstallPropertyTypeAsset(asset); err != nil {
				return nil, err
			}
		}
	}
	definitions := map[string][]recordState{build.ObjectType: {}}
	processes := map[string][]recordState{}
	for _, asset := range candidate.Assets {
		if asset.Ref.App != build.ID {
			continue
		}
		// Native Build records (for example a proposal's original function
		// call) are already provided by this fresh Build. They are fixed code
		// prerequisites, never tenant object drafts to reconstruct.
		if asset.Ref.Kind == platform.AssetObject && slices.ContainsFunc(b.Manifest().Entities, func(e platform.Entity) bool { return e.Type == asset.Ref.Name }) {
			continue
		}
		if asset.Ref.Kind == platform.AssetAlert {
			var d platform.AlertRule
			if json.Unmarshal(asset.Body, &d) != nil || d.Check() != nil {
				return nil, fmt.Errorf("invalid candidate alert rule")
			}
			var version int
			if _, err := fmt.Sscanf(asset.SourceVersion, b.Manifest().Version+".alert-%d", &version); err != nil || version < 1 || version > 64 || asset.SourceVersion != fmt.Sprintf("%s.alert-%d", b.Manifest().Version, version) {
				return nil, fmt.Errorf("invalid candidate alert version")
			}
			alert := build.Alert{Record: platform.Record{ID: asset.Ref.Name}, Name: d.Name, Title: d.Title, Object: d.Object.Name, Field: d.Field, Operator: d.Operator, Value: d.Value, Message: d.Message, Role: d.Role, Every: d.Every, Active: d.Active, State: "published", Version: version, DefinitionVersion: version, Published: string(platform.Raw(asset))}
			definitions[build.AlertType] = append(definitions[build.AlertType], recordState{Value: platform.Raw(alert)})
			continue
		}
		if asset.Ref.Kind == platform.AssetPropertyType || asset.Ref.Kind == platform.AssetLinkType || asset.Ref.Kind == platform.AssetQuery || asset.Ref.Kind == platform.AssetFunction || asset.Ref.Kind == platform.AssetCompute || asset.Ref.Kind == platform.AssetAgent || ownedMetadataKind(asset.Ref.Kind) {
			continue // compiled after its source objects below
		}
		if asset.Ref.App != build.ID || asset.SourceVersion != b.Manifest().Version {
			return nil, fmt.Errorf("unsupported test dependency or compiler version %s", asset.Ref)
		}
		if asset.Ref.Kind == platform.AssetFlow {
			process, err := b.ProcessFromAsset(asset)
			if err != nil {
				return nil, err
			}
			raw, _ := json.Marshal(process)
			processes[build.ProcessType] = append(processes[build.ProcessType], recordState{Value: raw})
			continue
		}
		if asset.Ref.Kind == platform.AssetAction || asset.Ref.Kind == platform.AssetPage || asset.Ref.Kind == platform.AssetApp {
			continue
		}
		if asset.Ref.Kind != platform.AssetObject {
			return nil, fmt.Errorf("unsupported test dependency %s", asset.Ref)
		}
		var object build.Object
		if err := json.Unmarshal(asset.Body, &object); err != nil {
			return nil, err
		}
		object.ID, object.State, object.Installed = asset.Ref.Name, "published", asset.Ref.Name
		published, err := json.Marshal(object)
		if err != nil {
			return nil, err
		}
		object.Published = string(published)
		raw, _ := json.Marshal(object)
		definitions[build.ObjectType] = append(definitions[build.ObjectType], recordState{Value: raw})
	}
	if _, err := sandbox.restoreRecords(definitions); err != nil {
		return nil, err
	}
	if err := b.Reinstall(); err != nil {
		return nil, err
	}
	if hasFunctions {
		for _, asset := range candidate.Assets {
			if asset.Ref.Kind == platform.AssetFunction && asset.Ref.App == build.ID {
				if err := b.InstallFunctionAsset(asset); err != nil {
					return nil, err
				}
			}
		}
		sandbox.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { return e.Body, nil }
	}
	for _, asset := range candidate.Assets {
		if asset.Ref.Kind == platform.AssetCompute && asset.Ref.App == build.ID {
			if err := b.InstallOperationAsset(asset); err != nil {
				return nil, err
			}
		}
	}
	for _, asset := range candidate.Assets {
		if asset.Ref.App == build.ID && asset.Ref.Kind == platform.AssetLinkType {
			if err := b.InstallLinkTypeAsset(asset); err != nil {
				return nil, err
			}
		}
	}
	for _, asset := range candidate.Assets {
		if asset.Ref.App == build.ID && asset.Ref.Kind == platform.AssetQuery {
			if err := b.InstallQueryAsset(asset); err != nil {
				return nil, err
			}
		}
	}
	for _, asset := range candidate.Assets {
		if asset.Ref.Kind == platform.AssetAgent && asset.Ref.App == build.ID {
			if err := b.InstallAgentAsset(asset); err != nil {
				return nil, err
			}
		}
	}
	// Functions must exist before process validation resolves their pinned versions.
	if len(processes) != 0 {
		if _, err := sandbox.restoreRecords(processes); err != nil {
			return nil, err
		}
		if err := b.Reinstall(); err != nil {
			return nil, err
		}
	}
	// Restore the same dependent page/application descriptors the release
	// review includes. Actions are compiled solely from their owning objects.
	for _, asset := range candidate.Assets {
		if asset.Ref.Kind == platform.AssetPage {
			var page platform.Page
			if err := json.Unmarshal(asset.Body, &page); err != nil {
				return nil, err
			}
			if err := sandbox.InstallPage(b, page); err != nil {
				return nil, err
			}
		}
	}
	// Passive owner declarations stay in the application closure. Their live
	// connections, credentials, datasets and migration runs are never imported.
	for _, asset := range candidate.Assets {
		if ownedMetadataKind(asset.Ref.Kind) {
			definition := platform.Definition{Ref: asset.Ref, Source: "builder", Version: asset.SourceVersion, ContractVersion: asset.ContractVersion, Requires: asset.Requires, Provides: asset.Provides}
			switch asset.Ref.Kind {
			case platform.AssetObservation:
				var observation platform.ObservationDefinition
				if json.Unmarshal(asset.Body, &observation) != nil || observation.Check() != nil {
					return nil, fmt.Errorf("invalid observation declaration")
				}
				definition.Observation = &observation
			case platform.AssetSampleSet:
				var sample platform.SampleSet
				if json.Unmarshal(asset.Body, &sample) != nil || sample.Check() != nil {
					return nil, fmt.Errorf("Invalid sample asset")
				}
				definition.SampleSet = &sample
			case platform.AssetEvaluation:
				evaluation, err := platform.ReadEvaluationSet(asset.Body)
				if err != nil {
					return nil, err
				}
				definition.Evaluation = &evaluation
			}
			sandbox.definitions = append(sandbox.definitions, definition)
		}
	}

	for _, asset := range candidate.Assets {
		if asset.Ref.Kind == platform.AssetApp {
			var application platform.Application
			if err := json.Unmarshal(asset.Body, &application); err != nil {
				return nil, err
			}
			if err := sandbox.InstallApplication(b, application); err != nil {
				return nil, err
			}
		}
	}
	rows := map[string][]recordState{}
	for _, sample := range environment.Samples {
		for _, raw := range sample.Records {
			rows[sample.Type] = append(rows[sample.Type], recordState{Value: raw})
		}
	}
	if len(rows) > 0 {
		held, err := sandbox.restoreRecords(rows)
		if err != nil {
			return nil, err
		}
		if len(held) > 0 {
			return nil, fmt.Errorf("sample records have no candidate schema")
		}
	}
	return sandbox, nil
}
