package build

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

const TestPlanType = "build.testplan"

// TestPlan stores fixed inputs, not test state or a claim about the next draft.
type TestPlan struct {
	platform.Record
	Title   string                `json:"title" field:"required,search" title:"Test plan name"`
	Object  platform.Ref[Object]  `json:"object,omitempty" title:"Saved object draft"`
	Process platform.Ref[Process] `json:"process,omitempty" title:"Saved workflow draft"`
	As      string                `json:"as,omitempty" title:"Member ID (empty: you)"`
	At      time.Time             `json:"at" field:"required" title:"Fixed test time"`
	Steps   []TestStep            `json:"steps" field:"required,aside" title:"Test steps"`
}

type TestStep struct {
	Type           string `json:"type"`
	ID             string `json:"id"`
	Action         string `json:"action"`
	Payload        string `json:"payload"`
	Expect         string `json:"expect" title:"Expected outcome"`
	As             string `json:"as,omitempty" title:"Step member ID (empty: plan member)"`
	AdvanceSeconds int    `json:"advanceSeconds,omitempty" title:"Advance clock (seconds)"`
	Flow           string `json:"flow,omitempty"`
	Step           string `json:"step,omitempty"`
	Answer         string `json:"answer,omitempty"`
}

func (*Build) testPlanEntity() platform.Entity {
	return platform.Entity{Type: TestPlanType, Title: "Test plan", Plural: "Test plans", Model: TestPlan{}, Display: "title",
		Description: "Fixed sample inputs and expected outcomes for testing a saved object or workflow draft.",
		Scope:       platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant}},
		Standard:    platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder}, Capability: "objects"}}
}

// Validate only the supplied fields of a create/edit; the standard action owns
// patching, required fields, references, revision checks and durable writes.
func checkTestPlanFields(raw []byte) *kernel.Error {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if given, ok := fields["at"]; ok {
		var at time.Time
		if json.Unmarshal(given, &at) != nil || at.IsZero() {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Use a valid fixed test time")
		}
	}
	if given, ok := fields["steps"]; ok {
		var steps []TestStep
		decoder := json.NewDecoder(bytes.NewReader(given))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&steps) != nil || len(steps) < 1 || len(steps) > 20 {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A test plan needs 1–20 typed steps")
		}
		for _, step := range steps {
			var payload map[string]json.RawMessage
			if !strings.HasPrefix(step.Type, ID+".") || step.Type == ObjectType || step.Type == PageType || step.Type == AppType || step.Type == TestPlanType || step.Type == ProcessType ||
				strings.TrimSpace(step.ID) == "" || step.AdvanceSeconds < 0 || step.AdvanceSeconds > 86400 ||
				(step.Expect != "accepted" && step.Expect != "refused") || json.Unmarshal([]byte(step.Payload), &payload) != nil || payload == nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Every test step needs a candidate action or answer, record ID, JSON object, bounded time and expected outcome")
			}
			if step.Answer == "" {
				if !strings.HasPrefix(step.Action, step.Type+".") || step.Flow != "" || step.Step != "" {
					return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose one candidate action or workflow answer per test step")
				}
			} else if step.Action != "" || !strings.HasPrefix(step.Flow, ID+".") || !named(step.Step) {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose one candidate action or workflow answer per test step")
			}
		}
	}
	return nil
}

// Root selection is checked on the patched image, so a partial edit cannot
// accidentally retain both roots or remove the only root. Standard records
// still own reference validation, permissions, revisions and persistence.
func (b *Build) checkTestPlan(c platform.Caller, s *pb.Submission) *kernel.Error {
	if c.Role() != Builder {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	if err := checkTestPlanFields(s.GetPayload()); err != nil {
		return err
	}
	var plan TestPlan
	if s.GetSchema().GetName() == TestPlanType+".edit" {
		plan, _ = platform.Get[TestPlan](c, s.GetTarget().GetId())
	}
	if json.Unmarshal(s.GetPayload(), &plan) != nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if (plan.Object == "") == (plan.Process == "") {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose exactly one saved object or workflow draft for the test plan")
	}
	return nil
}
