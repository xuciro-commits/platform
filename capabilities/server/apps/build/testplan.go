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
	Title  string               `json:"title" field:"required,search" title:"Test plan name"`
	Object platform.Ref[Object] `json:"object" field:"required" title:"Saved object draft"`
	As     string               `json:"as,omitempty" title:"Member ID (empty: you)"`
	At     time.Time            `json:"at" field:"required" title:"Fixed test time"`
	Steps  []TestStep           `json:"steps" field:"required,aside" title:"Test steps"`
}

type TestStep struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	Action  string `json:"action"`
	Payload string `json:"payload"`
	Expect  string `json:"expect" title:"Expected outcome"`
}

func (*Build) testPlanEntity() platform.Entity {
	return platform.Entity{Type: TestPlanType, Title: "Test plan", Plural: "Test plans", Model: TestPlan{}, Display: "title",
		Description: "Fixed sample inputs and expected outcomes for testing a saved object draft.",
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
			if !strings.HasPrefix(step.Type, ID+".") || step.Type == ObjectType || step.Type == PageType || step.Type == AppType || step.Type == TestPlanType ||
				strings.TrimSpace(step.ID) == "" || !strings.HasPrefix(step.Action, step.Type+".") ||
				(step.Expect != "accepted" && step.Expect != "refused") || json.Unmarshal([]byte(step.Payload), &payload) != nil || payload == nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Every test step needs a candidate action, record ID, JSON object and expected outcome")
			}
		}
	}
	return nil
}
