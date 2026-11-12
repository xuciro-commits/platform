package trials

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/services/resources/authoring"
)

type Sandbox struct {
	Tenant    string
	AI, Flows bool
	Member    func(id string) (platform.Member, bool)
	Trace     func(func(platform.DecisionTrace)) func(platform.DecisionTrace)
	Read      func(actor platform.Member, typ, id string, now time.Time) (json.RawMessage, *kernel.Error)
	Flow      func(id string) (platform.FlowInstance, bool)
	Current   func(actor platform.Member, typ, id string, now time.Time) *kernel.Error
	Owner     func(schema string) string
	Submit    func(actor platform.Member, s *pb.Submission, now time.Time) (*kernel.Error, []platform.FieldIssue)
	Work      func(now time.Time)
	Compute   func(actor platform.Member, fixture platform.ComputeFixture, now time.Time) (bool, error)
	Function  func(actor platform.Member, fixture platform.FunctionFixture, now time.Time) (bool, error)
	Records   func(actor platform.Member, typ string, q platform.Query, now time.Time) ([]any, int, *kernel.Error)
	Inbox     func(actor platform.Member) ([]platform.WorkTask, bool)
	Waiting   func() (compute, functions bool)
	Faulted   func() string
	Save      func() (json.RawMessage, error)
	Load      func(json.RawMessage) error
	Verify    func() error
}

type CandidateRun struct {
	Request   CandidateSimulationRequest
	Candidate platform.ReleaseCandidate
	Sandbox   Sandbox
	Member    platform.Member
	Now       time.Time
}

func (r *CandidateRun) Step(i int, step SimulationStep) (Simulation, *kernel.Error) {
	actor := r.Member
	if step.As != "" {
		actor, _ = r.Sandbox.Member(step.As)
	}
	before := map[string]SimulatedChange{}
	if r.Request.ApplicationID != "" || r.Request.CandidateID != "" || len(r.Request.Roots) > 0 {
		for _, change := range r.visibleChanges(actor) {
			before[change.Type+"/"+change.ID] = change
		}
	}
	trace := Trace{Events: []platform.DecisionTrace{}}
	r.Sandbox.Trace(trace.Record)
	defer r.Sandbox.Trace(nil)
	var read json.RawMessage
	var refusal *kernel.Error
	var issues []platform.FieldIssue
	var sub *pb.Submission
	if step.Read {
		if step.Action != "" || step.Answer != "" || step.AdvanceSeconds != 0 || step.ID == "" || !slices.ContainsFunc(r.Candidate.Assets, func(a platform.ReleaseAsset) bool {
			return a.Ref.Kind == platform.AssetObject && a.Ref.Name == step.Type
		}) {
			return Simulation{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A read step needs a candidate object and record ID")
		}
		read, refusal = r.Sandbox.Read(actor, step.Type, step.ID, r.Now)
	} else if step.Answer != "" {
		if step.Action != "" || step.Flow == "" || step.Step == "" || step.ID == "" {
			return Simulation{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "An answer test step needs a candidate flow, source record and ask step")
		}
		var found bool
		for _, asset := range r.Candidate.Assets {
			if asset.Ref.Kind == platform.AssetFlow && asset.Ref.Name == step.Flow {
				var envelope platform.FlowReleaseDescriptor
				_ = json.Unmarshal(asset.Body, &envelope)
				found = envelope.Subject.Name == step.Type
			}
		}
		if !found {
			return Simulation{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The answer must belong to a flow and object in this candidate")
		}
		instance, exists := r.Sandbox.Flow(step.Flow + ":" + step.ID)
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
	} else if step.Type == build.ProcessType && step.Action == build.SchemaProcessRun && step.ID == r.Request.ProcessID {
		processID := ""
		for _, asset := range r.Candidate.Assets {
			if asset.Ref.Kind == platform.AssetFlow {
				var definition platform.FlowReleaseDescriptor
				json.Unmarshal(asset.Body, &definition)
				processID = definition.Name
				break
			}
		}
		sub = &pb.Submission{Authority: build.ID, Target: &pb.EntityRef{Type: step.Type, Id: processID}, Schema: &pb.SchemaRef{Name: step.Action, Version: 1}, Payload: step.Payload}
	} else if step.Type == build.ProcessType && step.Action == "" && step.AdvanceSeconds > 0 && r.Request.ProcessID != "" {
		var instanceExists bool
		for _, asset := range r.Candidate.Assets {
			if asset.Ref.Kind == platform.AssetFlow {
				var definition platform.FlowReleaseDescriptor
				json.Unmarshal(asset.Body, &definition)
				if _, ok := r.Sandbox.Flow(definition.Name + ":" + step.ID); ok {
					instanceExists = true
				}
			}
		}
		if !instanceExists {
			refusal = platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The manual test has no run with this key")
		}
	} else if step.Action == "" && step.AdvanceSeconds > 0 && step.Flow == "" && step.Step == "" {
		if !slices.ContainsFunc(r.Candidate.Assets, func(a platform.ReleaseAsset) bool {
			return a.Ref.Kind == platform.AssetObject && a.Ref.Name == step.Type
		}) || step.ID == "" {
			return Simulation{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A clock step needs a candidate object and record ID")
		}
		refusal = r.Sandbox.Current(actor, step.Type, step.ID, r.Now)
	} else {
		owner := r.Sandbox.Owner(step.Action)
		business := slices.ContainsFunc(r.Candidate.Assets, func(asset platform.ReleaseAsset) bool {
			return asset.Ref.App == build.ID && asset.Ref.Kind == platform.AssetObject && asset.Ref.Name == step.Type
		})
		function := step.Type == build.FunctionCallType && slices.ContainsFunc(r.Candidate.Assets, func(asset platform.ReleaseAsset) bool { return asset.Ref.Kind == platform.AssetFunction })
		if (!business && !function) || owner != build.ID || step.ID == "" || !strings.HasPrefix(step.Type, build.ID+".") ||
			step.Type == build.ObjectType || step.Type == build.PageType || step.Type == build.AppType || step.Type == build.TestPlanType || step.Type == build.ProcessType || step.Type == build.FunctionType {
			return Simulation{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Test step {step} must name an action and record of the candidate objects", fmt.Sprint(i+1))
		}
		payload := step.Payload
		if len(payload) == 0 {
			payload = json.RawMessage(`{}`)
		}
		sub = &pb.Submission{Authority: build.ID, Target: &pb.EntityRef{Type: step.Type, Id: step.ID},
			Schema: &pb.SchemaRef{Name: step.Action, Version: 1}, Payload: payload}
	}
	if sub != nil {
		sub.TenantId, sub.PrincipalId, sub.IdempotencyKey = r.Sandbox.Tenant, actor.ID, fmt.Sprintf("test-%d", i+1)
		refusal, issues = r.Sandbox.Submit(actor, sub, r.Now)
	}
	if step.AdvanceSeconds > 0 {
		r.Now = r.Now.Add(time.Duration(step.AdvanceSeconds) * time.Second)
		r.Sandbox.Work(r.Now)
	}
	result := Simulation{Accepted: refusal == nil, Changes: []SimulatedChange{}, Input: &step, Read: read, RolledBack: refusal != nil && sub != nil}
	if step.Compute != nil {
		matched, err := r.Sandbox.Compute(actor, *step.Compute, r.Now)
		if err != nil {
			return Simulation{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The compute fixture cannot run: {why}", err.Error())
		}
		result.ComputeMatched = &matched
	}
	if step.Function != nil {
		matched, err := r.Sandbox.Function(actor, *step.Function, r.Now)
		if err != nil {
			return Simulation{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The function fixture cannot run: {why}", err.Error())
		}
		result.FunctionMatched = &matched
	}
	if r.Sandbox.AI {
		records, _, problem := r.Sandbox.Records(actor, build.FunctionCallType, platform.Query{Limit: 100}, r.Now)
		if problem == nil {
			for _, record := range records {
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
			if r.Request.ApplicationID != "" || r.Request.CandidateID != "" || len(r.Request.Roots) > 0 {
				if exists && bytes.Equal(prior.Record, change.Record) {
					continue
				}
				change.Before = prior.Record
			}
			result.Changes = append(result.Changes, change)
		}

	}
	if r.Sandbox.Flows {
		records, _, _ := r.Sandbox.Records(actor, platform.FlowInstanceType, platform.Query{Limit: 100}, r.Now)
		for _, record := range records {
			raw, _ := json.Marshal(record)
			var instance platform.FlowInstance
			_ = json.Unmarshal(raw, &instance)
			result.Flows = append(result.Flows, SimulatedFlow{ID: instance.ID, Flow: instance.Flow, Version: instance.Version, Dependencies: instance.Dependencies, Release: instance.Release, State: instance.State, Tokens: instance.Tokens, Trace: instance.Trace, Outputs: instance.Outputs})
		}
		if inbox, ok := r.Sandbox.Inbox(actor); ok {
			result.Tasks = inbox
		}
	}
	result.Trace = trace.Events
	result.TraceTruncated = trace.Truncated
	return result, nil
}

func (r *CandidateRun) visibleChanges(actor platform.Member) []SimulatedChange {
	defer r.Sandbox.Trace(r.Sandbox.Trace(nil))
	changes := []SimulatedChange{}
	for _, asset := range r.Candidate.Assets {
		if asset.Ref.Kind != platform.AssetObject {
			continue
		}
		offset := 0
		for {
			records, total, err := r.Sandbox.Records(actor, asset.Ref.Name, platform.Query{Limit: 500, Offset: offset, Archived: true, Sort: []string{"id"}}, r.Now)
			if err != nil {
				break
			}
			for _, record := range records {
				raw, _ := json.Marshal(record)
				var identity struct {
					ID string `json:"id"`
				}
				_ = json.Unmarshal(raw, &identity)
				changes = append(changes, SimulatedChange{Type: asset.Ref.Name, ID: identity.ID, Record: raw})
			}
			offset += len(records)
			if offset >= total || len(records) == 0 {
				break
			}
		}
	}
	return changes
}

func (r *CandidateRun) HasAI() bool { return r.Sandbox.AI }

func (r *CandidateRun) Pending() (compute, functions bool) { return r.Sandbox.Waiting() }

func (r *CandidateRun) Fault() string { return r.Sandbox.Faulted() }

func (r *CandidateRun) Snapshot() (json.RawMessage, error) { return r.Sandbox.Save() }

func (r *CandidateRun) Restore(saved json.RawMessage) error { return r.Sandbox.Load(saved) }

func (r *CandidateRun) Check() error { return r.Sandbox.Verify() }
