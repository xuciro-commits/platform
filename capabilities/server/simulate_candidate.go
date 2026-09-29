package platformserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/build"
	"platformserver/apps/flow"
	"platformserver/apps/work"
	"platformserver/platform"
)

// CandidateSimulationRequest fixes all business inputs of an isolated run.
// Create actions establish its sample data; nothing is copied from live rows.
type CandidateSimulationRequest struct {
	ObjectID  string           `json:"objectId,omitempty"`
	ProcessID string           `json:"processId,omitempty"`
	As        string           `json:"as,omitempty"`
	At        time.Time        `json:"at"`
	Steps     []SimulationStep `json:"steps"`
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
	Flow   string `json:"flow,omitempty"`
	Step   string `json:"step,omitempty"`
	Answer string `json:"answer,omitempty"`
}

type CandidateSimulation struct {
	CandidateID string       `json:"candidateId"`
	Steps       []Simulation `json:"steps"`
	Recovered   bool         `json:"recovered"`
	Passed      *bool        `json:"passed,omitempty"`
}

// SimulateCandidate installs a saved object or process draft in a fresh, bounded tenant.
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
	if (request.ObjectID == "") == (request.ProcessID == "") || request.At.IsZero() || len(request.Steps) == 0 || len(request.Steps) > 20 {
		return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose one saved object or process, a fixed time and 1–20 test steps")
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
	review, candidate, err := t.previewReleaseLocked(kind, id)
	t.mu.Unlock()
	if err == nil && review.Diagnostic != "" {
		err = fmt.Errorf("%s", review.Diagnostic)
	}
	if err != nil {
		return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The candidate cannot be tested: {why}", err.Error())
	}
	// The constructor takes exact owner-produced bytes, not callbacks closing
	// over the production Build or its host. Each invocation owns every app.
	compose := func() (*Tenant, error) { return candidateTestTenant(candidate, m, members[1:]...) }
	sandbox, err := compose()
	if err != nil {
		return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The candidate cannot be tested: {why}", err.Error())
	}
	out := CandidateSimulation{CandidateID: candidate.ID, Steps: []Simulation{}}
	passed, asserted := true, 0
	now := request.At
	for i, step := range request.Steps {
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
		} else {
			action := sandbox.owner["action:"+step.Action]
			if action == nil || step.ID == "" || action.Manifest().ID != build.ID || !strings.HasPrefix(step.Type, build.ID+".") ||
				step.Type == build.ObjectType || step.Type == build.PageType || step.Type == build.AppType || step.Type == build.TestPlanType || step.Type == build.ProcessType {
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
				result.Flows = append(result.Flows, SimulatedFlow{ID: instance.ID, Flow: instance.Flow, Version: instance.Version, Dependencies: instance.Dependencies, Release: instance.Release, State: instance.State, Tokens: instance.Tokens, Trace: instance.Trace})
			}
			if inbox, err := sandbox.Read(actor, "inbox"); err == nil {
				result.Tasks = inbox.([]work.WorkTask)
			}
		}
		out.Steps = append(out.Steps, result)
	}
	if asserted == len(request.Steps) {
		out.Passed = &passed
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
		return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The test state did not recover exactly")
	}
	return out, nil
}

func candidateTestTenant(candidate platform.ReleaseCandidate, member platform.Member, others ...platform.Member) (*Tenant, error) {
	b := build.New(member.Tenant)
	member.Roles = maps.Clone(member.Roles)
	seats := []Seat{{Subjects: []string{member.ID}, Member: member}}
	for _, other := range others {
		other.Roles = maps.Clone(other.Roles)
		seats = append(seats, Seat{Subjects: []string{other.ID}, Member: other})
	}
	apps := []platform.App{NewConsole(member.Tenant, seats...)}
	if slices.ContainsFunc(candidate.Assets, func(a platform.ReleaseAsset) bool { return a.Ref.Kind == platform.AssetFlow }) {
		apps = append(apps, work.New(member.Tenant), flow.New(member.Tenant))
	}
	apps = append(apps, b)
	sandbox, err := NewTenant(member.Tenant, apps...)
	if err != nil {
		return nil, err
	}
	definitions := map[string][]recordState{build.ObjectType: {}}
	for _, asset := range candidate.Assets {
		if asset.Ref.App != build.ID || asset.SourceVersion != b.Manifest().Version {
			return nil, fmt.Errorf("unsupported test dependency or compiler version %s", asset.Ref)
		}
		if asset.Ref.Kind == platform.AssetFlow {
			process, err := build.ProcessFromReleaseAsset(asset)
			if err != nil {
				return nil, err
			}
			raw, _ := json.Marshal(process)
			definitions[build.ProcessType] = append(definitions[build.ProcessType], recordState{Value: raw})
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
	return sandbox, nil
}
