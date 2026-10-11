package build

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func (*Build) testPlanEntity() platform.Entity {
	return platform.Entity{Type: TestPlanType, Title: "Test plan", Plural: "Test plans", Model: TestPlan{}, Display: "title",
		Description: "Fixed sample inputs and expected outcomes for testing a saved object, workflow or function draft.",
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
	if given, ok := fields["evaluation"]; ok && !bytes.Equal(bytes.TrimSpace(given), []byte("null")) {
		var policy []EvaluationPolicy
		decoder := json.NewDecoder(bytes.NewReader(given))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&policy) != nil || len(policy) != 1 || decoder.Decode(new(any)) != io.EOF {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Function evaluation needs quality, total USD cost and per-call latency thresholds")
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
			if !strings.HasPrefix(step.Type, ID+".") || step.Type == ObjectType || step.Type == PageType || step.Type == AppType || step.Type == TestPlanType || step.Type == ProcessType && step.Action != SchemaProcessRun && step.AdvanceSeconds == 0 || step.Type == FunctionType ||
				strings.TrimSpace(step.ID) == "" || step.AdvanceSeconds < 0 || step.AdvanceSeconds > 86400 ||
				(step.Expect != "accepted" && step.Expect != "refused") || json.Unmarshal([]byte(step.Payload), &payload) != nil || payload == nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Every test step needs a candidate action or answer, record ID, JSON object, bounded time and expected outcome")
			}
			if step.Compute != nil && !step.Compute.Check() {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Use a bounded compute fixture with a completed or failed outcome")
			}
			if step.Function != nil && !step.Function.Check() {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Use a bounded function fixture with a ready or rejected outcome")
			}
			if step.Answer == "" {
				if (step.Action == "" && step.AdvanceSeconds == 0 || step.Action != "" && !strings.HasPrefix(step.Action, step.Type+".")) || step.Flow != "" || step.Step != "" {
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
	if !c.Holds(c.App, Builder) {
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
	roots := 0
	for _, root := range []string{string(plan.Object), string(plan.Process), string(plan.Function)} {
		if root != "" {
			roots++
		}
	}
	if roots != 1 || len(plan.Model) > 256 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose exactly one saved object, workflow or function draft for the test plan")
	}
	if len(plan.Evaluation) != 0 {
		if len(plan.Evaluation) != 1 {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Function evaluation needs quality, total USD cost and per-call latency thresholds")
		}
		policy := plan.Evaluation[0]
		if plan.Function == "" || policy.MinQuality <= 0 || policy.MinQuality > 1 ||
			policy.MaxCostUSD <= 0 || policy.MaxCostUSD > 1000 || policy.MaxLatencyMillis < 1 || policy.MaxLatencyMillis > 300000 {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Function evaluation needs quality, total USD cost and per-call latency thresholds")
		}
		if len(policy.Cases) == 0 || len(policy.Cases) > 5 {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A function evaluation needs 1–5 bounded cases")
		}
		seen := map[string]bool{}
		for _, test := range policy.Cases {
			var input, expected map[string]json.RawMessage
			if !named(test.Name) || seen[test.Name] || len(test.Input) > 32<<10 || len(test.Expected) > 32<<10 ||
				json.Unmarshal(test.Input, &input) != nil || input == nil ||
				json.Unmarshal(test.Expected, &expected) != nil || expected == nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Each evaluated function case needs a named synthetic input and expected typed answer")
			}
			seen[test.Name] = true
		}
	}
	return nil
}
