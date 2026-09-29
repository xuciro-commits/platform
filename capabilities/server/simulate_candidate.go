package platformserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/build"
	"platformserver/platform"
)

// CandidateSimulationRequest fixes all business inputs of an isolated run.
// Create actions establish its sample data; nothing is copied from live rows.
type CandidateSimulationRequest struct {
	ObjectID string           `json:"objectId"`
	As       string           `json:"as,omitempty"`
	At       time.Time        `json:"at"`
	Steps    []SimulationStep `json:"steps"`
}

type SimulationStep struct {
	Type    string          `json:"type"`
	ID      string          `json:"id"`
	Action  string          `json:"action"`
	Payload json.RawMessage `json:"payload"`
	Expect  string          `json:"expect,omitempty"`
}

type CandidateSimulation struct {
	CandidateID string       `json:"candidateId"`
	Steps       []Simulation `json:"steps"`
	Recovered   bool         `json:"recovered"`
	Passed      *bool        `json:"passed,omitempty"`
}

// SimulateCandidate installs a saved object draft in a fresh, bounded tenant.
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
	if request.ObjectID == "" || request.At.IsZero() || len(request.Steps) == 0 || len(request.Steps) > 20 {
		return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose a saved object, a fixed time and 1–20 test steps")
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
	review, candidate, err := t.previewReleaseLocked(platform.AssetObject, request.ObjectID)
	t.mu.Unlock()
	if err == nil && review.Diagnostic != "" {
		err = fmt.Errorf("%s", review.Diagnostic)
	}
	if err != nil {
		return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The candidate cannot be tested: {why}", err.Error())
	}
	// The constructor takes exact owner-produced bytes, not callbacks closing
	// over the production Build or its host. Each invocation owns every app.
	compose := func() (*Tenant, error) { return candidateTestTenant(candidate, m) }
	sandbox, err := compose()
	if err != nil {
		return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The candidate cannot be tested: {why}", err.Error())
	}
	out := CandidateSimulation{CandidateID: candidate.ID, Steps: []Simulation{}}
	passed, asserted := true, 0
	for i, step := range request.Steps {
		if step.Expect != "" && step.Expect != "accepted" && step.Expect != "refused" {
			return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose accepted or refused as the expected test outcome")
		}
		action := sandbox.owner["action:"+step.Action]
		if action == nil || step.ID == "" || action.Manifest().ID != build.ID || !strings.HasPrefix(step.Type, build.ID+".") ||
			step.Type == build.ObjectType || step.Type == build.PageType || step.Type == build.AppType || step.Type == build.TestPlanType {
			return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Test step {step} must name an action and record of the candidate objects", fmt.Sprint(i+1))
		}
		payload := step.Payload
		if len(payload) == 0 {
			payload = json.RawMessage(`{}`)
		}
		sub := &pb.Submission{TenantId: sandbox.ID, PrincipalId: m.ID, Authority: build.ID,
			IdempotencyKey: fmt.Sprintf("test-%d", i+1), Target: &pb.EntityRef{Type: step.Type, Id: step.ID},
			Schema: &pb.SchemaRef{Name: step.Action, Version: 1}, Payload: payload}
		_, refusal := sandbox.Submit(m, sub, request.At)
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
				page, kerr := sandbox.Records(m, asset.Ref.Name, platform.Query{Limit: 100}, request.At)
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

func candidateTestTenant(candidate platform.ReleaseCandidate, member platform.Member) (*Tenant, error) {
	b := build.New(member.Tenant)
	member.Roles = maps.Clone(member.Roles)
	sandbox, err := NewTenant(member.Tenant, NewConsole(member.Tenant, Seat{Subjects: []string{member.ID}, Member: member}), b)
	if err != nil {
		return nil, err
	}
	definitions := map[string][]recordState{build.ObjectType: {}}
	for _, asset := range candidate.Assets {
		if asset.Ref.App != build.ID {
			return nil, fmt.Errorf("unsupported test dependency %s", asset.Ref)
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
