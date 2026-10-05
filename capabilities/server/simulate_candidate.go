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
	"platformserver/apps/ai"
	"platformserver/apps/build"
	"platformserver/apps/flow"
	"platformserver/apps/work"
	"platformserver/platform"
)

// CandidateSimulationRequest fixes all business inputs of an isolated run.
// Create actions establish its sample data; nothing is copied from live rows.
type CandidateSimulationRequest struct {
	ObjectID   string             `json:"objectId,omitempty"`
	ProcessID  string             `json:"processId,omitempty"`
	FunctionID string             `json:"functionId,omitempty"`
	Model      string             `json:"model,omitempty"`
	As         string             `json:"as,omitempty"`
	At         time.Time          `json:"at"`
	Steps      []SimulationStep   `json:"steps"`
	Samples    []SimulationSample `json:"samples,omitempty"`
}

type SimulationStep struct {
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

type CandidateSimulation struct {
	CandidateID string       `json:"candidateId"`
	TestID      string       `json:"testId,omitempty"`
	Model       string       `json:"model,omitempty"`
	Fixture     bool         `json:"fixture,omitempty"`
	Steps       []Simulation `json:"steps"`
	Recovered   bool         `json:"recovered"`
	Passed      *bool        `json:"passed,omitempty"`
}

// SimulateCandidate installs a saved object, process or function draft in a fresh, bounded tenant.
// Only this builder's objects are supported. All action decisions and result
// reads use the ordinary runtime, with no live records or external bindings.
func (t *Tenant) SimulateCandidate(builder platform.Member, request CandidateSimulationRequest) (CandidateSimulation, *kernel.Error) {
	var empty CandidateSimulation
	if err := t.admits(builder); err != nil {
		return empty, err
	}
	if builder.Roles[build.ID] != build.Builder {
		return empty, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	roots := 0
	for _, root := range []string{request.ObjectID, request.ProcessID, request.FunctionID} {
		if root != "" {
			roots++
		}
	}
	if roots != 1 || len(request.Model) > 256 || request.At.IsZero() || len(request.Steps) == 0 || len(request.Steps) > 20 {
		return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose one saved object, process or function, a fixed time and 1–20 test steps")
	}
	t.mu.Lock()
	if t.quarantined() {
		t.mu.Unlock()
		return empty, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	m := builder
	if request.As != "" {
		var ok bool
		m, ok = t.member(request.As)
		if !ok {
			t.mu.Unlock()
			return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "{member} is not a member here", request.As)
		}
	}
	members := []platform.Member{m}
	for _, step := range request.Steps {
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
	review, candidate, err := t.previewReleaseLocked([]build.JointDraftRef{{Kind: kind, ID: id}})
	functionName := ""
	if request.FunctionID != "" {
		selected, _ := platform.Get[build.Function](t.automation(build.ID, false), request.FunctionID)
		functionName = selected.Name
	}
	environment, environmentErr := t.simulationEnvironment(candidate, request.Samples)
	if err == nil {
		err = environmentErr
	}
	t.mu.Unlock()
	if err == nil && review.Diagnostic != "" {
		err = fmt.Errorf("%s", review.Diagnostic)
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
	// The constructor takes exact owner-produced bytes, not callbacks closing
	// over the production Build or its host. Each invocation owns every app.
	compose := func() (*Tenant, error) {
		sandbox, err := candidateTestTenantWithEnvironment(candidate, m, environment, members[1:]...)
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
	out.TestID, err = canonicalDigest([]any{candidate.ID, request, members})
	if err != nil {
		return empty, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if sandbox.ai != nil {
		out.TestID, err = canonicalDigest([]any{candidate.ID, request, members})
		if err != nil {
			return empty, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
		}
		out.Model, out.Fixture = request.Model, true
	}
	passed, asserted := true, 0
	functionAttempted := false
	now := request.At
	for i, step := range request.Steps {
		if request.FunctionID != "" && step.Action == build.SchemaFunctionCall {
			var call platform.FunctionRequest
			if json.Unmarshal(step.Payload, &call) == nil {
				functionAttempted = functionAttempted || call.Name == functionName
			}
		}
		if step.Expect != "" && step.Expect != "accepted" && step.Expect != "refused" {
			return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose accepted or refused as the expected test outcome")
		}
		actor := m
		if step.As != "" {
			actor, _ = sandbox.Member(step.As)
		}
		var refusal *kernel.Error
		var sub *pb.Submission
		if step.Answer != "" {
			if step.Action != "" || step.Flow == "" || step.Step == "" || step.ID == "" {
				return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "An answer test step needs a candidate flow, source record and ask step")
			}
			var found bool
			for _, asset := range candidate.Assets {
				if asset.Ref.Kind == platform.AssetFlow && asset.Ref.Name == step.Flow {
					var envelope platform.FlowReleaseDescriptor
					_ = json.Unmarshal(asset.Body, &envelope)
					found = envelope.Subject.Name == step.Type
				}
			}
			if !found {
				return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The answer must belong to a flow and object in this candidate")
			}
			instance, exists := platform.Get[flow.FlowInstance](sandbox.automation(flow.ID, false), step.Flow+":"+step.ID)
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
				sub = &pb.Submission{Authority: work.ID, Target: &pb.EntityRef{Type: work.TaskType, Id: taskID}, Schema: &pb.SchemaRef{Name: "work.task.complete", Version: 1}, Payload: payload}
			}
		} else if step.Type == build.ProcessType && step.Action == build.SchemaProcessRun && step.ID == request.ProcessID {
			processID := ""
			for _, asset := range candidate.Assets {
				if asset.Ref.Kind == platform.AssetFlow {
					var definition platform.FlowReleaseDescriptor
					json.Unmarshal(asset.Body, &definition)
					processID = definition.Name
					break
				}
			}
			sub = &pb.Submission{Authority: build.ID, Target: &pb.EntityRef{Type: step.Type, Id: processID}, Schema: &pb.SchemaRef{Name: step.Action, Version: 1}, Payload: step.Payload}
		} else if step.Type == build.ProcessType && step.Action == "" && step.AdvanceSeconds > 0 && request.ProcessID != "" {
			var instanceExists bool
			for _, asset := range candidate.Assets {
				if asset.Ref.Kind == platform.AssetFlow {
					var definition platform.FlowReleaseDescriptor
					json.Unmarshal(asset.Body, &definition)
					if _, ok := platform.Get[flow.FlowInstance](sandbox.automation(flow.ID, false), definition.Name+":"+step.ID); ok {
						instanceExists = true
					}
				}
			}
			if !instanceExists {
				refusal = platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The manual test has no run with this key")
			}
		} else if step.Action == "" && step.AdvanceSeconds > 0 && step.Flow == "" && step.Step == "" {
			if !slices.ContainsFunc(candidate.Assets, func(a platform.ReleaseAsset) bool {
				return a.Ref.Kind == platform.AssetObject && a.Ref.Name == step.Type
			}) || step.ID == "" {
				return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A clock step needs a candidate object and record ID")
			}
			_, refusal = sandbox.RecordOf(actor, step.Type, step.ID, now)
		} else {
			action := sandbox.owner["action:"+step.Action]
			if action == nil || step.ID == "" || action.Manifest().ID != build.ID || !strings.HasPrefix(step.Type, build.ID+".") ||
				step.Type == build.ObjectType || step.Type == build.PageType || step.Type == build.AppType || step.Type == build.TestPlanType || step.Type == build.ProcessType || step.Type == build.FunctionType {
				return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Test step {step} must name an action and record of the candidate objects", fmt.Sprint(i+1))
			}
			payload := step.Payload
			if len(payload) == 0 {
				payload = json.RawMessage(`{}`)
			}
			sub = &pb.Submission{Authority: build.ID, Target: &pb.EntityRef{Type: step.Type, Id: step.ID},
				Schema: &pb.SchemaRef{Name: step.Action, Version: 1}, Payload: payload}
		}
		if sub != nil {
			sub.TenantId, sub.PrincipalId, sub.IdempotencyKey = sandbox.ID, actor.ID, fmt.Sprintf("test-%d", i+1)
			_, refusal = sandbox.Submit(actor, sub, now)
		}
		if step.AdvanceSeconds > 0 {
			now = now.Add(time.Duration(step.AdvanceSeconds) * time.Second)
			sandbox.Work(now)
		}
		result := Simulation{Accepted: refusal == nil, Changes: []SimulatedChange{}}
		if step.Compute != nil {
			matched, err := settleComputeFixture(sandbox, actor, *step.Compute, now)
			if err != nil {
				return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The compute fixture cannot run: {why}", err.Error())
			}
			result.ComputeMatched = &matched
			passed = passed && matched
			out.Fixture = true
		}
		if step.Function != nil {
			matched, err := settleFunctionFixture(sandbox, actor, *step.Function, now)
			if err != nil {
				return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The function fixture cannot run: {why}", err.Error())
			}
			result.FunctionMatched = &matched
			passed = passed && matched
		}
		if sandbox.ai != nil {
			page, problem := sandbox.Records(actor, build.FunctionCallType, platform.Query{Limit: 100}, now)
			if problem == nil {
				for _, record := range page.Records {
					result.Functions = append(result.Functions, record.(build.FunctionRun))
				}
			}
		}
		if step.Expect != "" {
			matched := (step.Expect == "accepted") == result.Accepted
			result.Matched = &matched
			passed = passed && matched
			asserted++
		}
		if refusal != nil {
			result.Refusal = refusal.Message
		} else {
			// All rows here are test rows. Record/field permissions still bound
			// the result, including records created by another object's action.
			for _, asset := range candidate.Assets {
				if asset.Ref.Kind != platform.AssetObject {
					continue
				}
				page, kerr := sandbox.Records(actor, asset.Ref.Name, platform.Query{Limit: 100}, now)
				if kerr != nil {
					continue
				}
				for _, record := range page.Records {
					raw, _ := json.Marshal(record)
					var identity struct {
						ID string `json:"id"`
					}
					_ = json.Unmarshal(raw, &identity)
					result.Changes = append(result.Changes, SimulatedChange{Type: asset.Ref.Name, ID: identity.ID, Record: raw})
				}
			}
		}
		if sandbox.app(flow.ID) != nil {
			page, _ := sandbox.Records(actor, flow.InstanceType, platform.Query{Limit: 100}, now)
			for _, record := range page.Records {
				raw, _ := json.Marshal(record)
				var instance flow.FlowInstance
				_ = json.Unmarshal(raw, &instance)
				result.Flows = append(result.Flows, SimulatedFlow{ID: instance.ID, Flow: instance.Flow, Version: instance.Version, Dependencies: instance.Dependencies, Release: instance.Release, State: instance.State, Tokens: instance.Tokens, Trace: instance.Trace, Outputs: instance.Outputs})
			}
			if inbox, err := sandbox.Read(actor, "inbox"); err == nil {
				result.Tasks = inbox.([]work.WorkTask)
			}
		}
		out.Steps = append(out.Steps, result)
	}
	if request.FunctionID != "" && !functionAttempted {
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

func candidateTestTenant(candidate platform.ReleaseCandidate, member platform.Member, others ...platform.Member) (*Tenant, error) {
	return candidateTestTenantWithEnvironment(candidate, member, simulationEnvironment{}, others...)
}
func candidateTestTenantWithEnvironment(candidate platform.ReleaseCandidate, member platform.Member, environment simulationEnvironment, others ...platform.Member) (*Tenant, error) {
	b := build.New(member.Tenant)
	member.Roles = maps.Clone(member.Roles)
	seats := []Seat{{Subjects: []string{member.ID}, Member: member}}
	for _, other := range others {
		other.Roles = maps.Clone(other.Roles)
		seats = append(seats, Seat{Subjects: []string{other.ID}, Member: other})
	}
	apps := []platform.App{NewConsole(member.Tenant, seats...)}
	hasFunctions := slices.ContainsFunc(candidate.Assets, func(a platform.ReleaseAsset) bool { return a.Ref.Kind == platform.AssetFunction })
	if hasFunctions {
		apps = append(apps, ai.New(member.Tenant))
	}
	if slices.ContainsFunc(candidate.Assets, func(a platform.ReleaseAsset) bool { return a.Ref.Kind == platform.AssetFlow }) {
		apps = append(apps, work.New(member.Tenant), flow.New(member.Tenant))
	}
	for _, manifest := range environment.Manifests {
		apps = append(apps, newSampleOwner(member.Tenant, manifest))
	}
	apps = append(apps, b)
	sandbox, err := NewTenant(member.Tenant, apps...)
	if err != nil {
		return nil, err
	}
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
		if asset.Ref.Kind == platform.AssetPropertyType || asset.Ref.Kind == platform.AssetLinkType || asset.Ref.Kind == platform.AssetQuery || asset.Ref.Kind == platform.AssetFunction || asset.Ref.Kind == platform.AssetCompute {
			continue // compiled after its source objects below
		}
		if asset.Ref.App != build.ID || asset.SourceVersion != b.Manifest().Version {
			return nil, fmt.Errorf("unsupported test dependency or compiler version %s", asset.Ref)
		}
		if asset.Ref.Kind == platform.AssetFlow {
			process, err := build.ProcessFromReleaseAsset(asset)
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
		for _, action := range object.Actions {
			if action.Approval != nil {
				return nil, fmt.Errorf("%s needs an approval test environment", asset.Ref)
			}
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
