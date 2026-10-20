package build

import "platformserver/internal/construction"

import (
	"bytes"
	"encoding/json"
	"io"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func (*Build) testPlanEntity() platform.Entity {
	return platform.Entity{Type: construction.TestPlanType, Title: "Test plan", Plural: "Test plans", Model: construction.TestPlan{}, Display: "title",
		Description: "Fixed sample inputs and expected outcomes for testing a saved object, workflow or function draft.",
		Scope:       platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{construction.Builder: platform.ScopeTenant}},
		Standard:    platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{construction.Builder}, Capability: "objects"}}
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
		var policy []construction.EvaluationPolicy
		decoder := json.NewDecoder(bytes.NewReader(given))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&policy) != nil || len(policy) != 1 || decoder.Decode(new(any)) != io.EOF {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Function evaluation needs quality, total USD cost and per-call latency thresholds")
		}
	}
	if given, ok := fields["steps"]; ok {
		var steps []construction.TestStep
		decoder := json.NewDecoder(bytes.NewReader(given))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&steps) != nil || len(steps) < 1 || len(steps) > 20 {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A test plan needs 1–20 typed steps")
		}
		for _, step := range steps {
			if err := step.Check(); err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
		}
	}
	return nil
}

// Root selection is checked on the patched image, so a partial edit cannot
// accidentally retain both roots or remove the only root. Standard records
// still own reference validation, permissions, revisions and persistence.
func (b *Build) checkTestPlan(c platform.Caller, s *pb.Submission) *kernel.Error {
	if !c.Holds(c.App, construction.Builder) {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	if err := checkTestPlanFields(s.GetPayload()); err != nil {
		return err
	}
	var plan construction.TestPlan
	var previous construction.TestPlan
	if s.GetSchema().GetName() == construction.TestPlanType+".edit" {
		plan, _ = platform.Get[construction.TestPlan](c, s.GetTarget().GetId())
		previous = plan
	}
	if json.Unmarshal(s.GetPayload(), &plan) != nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if previous.Published != "" && plan.Name != previous.Name {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A published definition cannot be renamed; create another definition")
	}
	if previous.Published != "" && testPlanKind(previous) != testPlanKind(plan) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A published test plan keeps its kind; create another plan")
	}
	if plan.Name != "" {
		rows, err := readDefinitionInventory[construction.TestPlan](b.host.Automation(c, construction.ID))
		if err != nil {
			return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
		for _, row := range rows {
			if row.ID != s.GetTarget().GetId() && row.Name == plan.Name {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "This definition name is already in use")
			}
		}
	}
	roots := 0
	for _, root := range []string{string(plan.Object), string(plan.Process), string(plan.Function), string(plan.Application)} {
		if root != "" {
			roots++
		}
	}
	if len(plan.Roots) > 0 {
		if roots != 0 || plan.CandidateID != "" || len(plan.Actors) > 0 || plan.As != "" || plan.Sample != "" || plan.Model != "" || len(plan.Evaluation) > 0 {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Portable test roots cannot include local draft, member or model bindings")
		}
		if err := (platform.TestPlanDescriptor{Name: plan.Name, Title: plan.Title, Roots: plan.Roots, At: plan.At, Samples: plan.Samples, Steps: plan.Steps}).Check(); err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
		return nil
	}
	if (roots != 1 && !(roots == 0 && plan.CandidateID != "")) || plan.CandidateID != "" && (plan.Object != "" || plan.Process != "" || plan.Function != "") || len(plan.Actors) > 3 || len(plan.Sample) > 64 || len(plan.Actors) > 0 && plan.As != "" || len(plan.Model) > 256 || plan.FunctionVersion < 0 || plan.FunctionVersion > 64 || plan.Name != "" && !named(plan.Name) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose exactly one saved object, workflow or function draft for the test plan")
	}
	if len(plan.Evaluation) != 0 {
		if len(plan.Evaluation) != 1 {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Function evaluation needs quality, total USD cost and per-call latency thresholds")
		}
		if plan.Function == "" || plan.Evaluation[0].Check() != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Function evaluation needs quality, total USD cost and per-call latency thresholds")
		}
	}
	return nil
}
