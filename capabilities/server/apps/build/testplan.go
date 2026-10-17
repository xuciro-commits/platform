package build

import (
	"bytes"
	"encoding/json"
	"io"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"time"
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
	if !c.Holds(c.App, Builder) {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	if err := checkTestPlanFields(s.GetPayload()); err != nil {
		return err
	}
	var plan TestPlan
	var previous TestPlan
	if s.GetSchema().GetName() == TestPlanType+".edit" {
		plan, _ = platform.Get[TestPlan](c, s.GetTarget().GetId())
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
		rows, err := readDefinitionInventory[TestPlan](b.host.Automation(c, ID))
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

const TestPlanType = "build.testplan"

// TestPlan stores fixed inputs, not test state or a claim about the next draft.
type TestPlan struct {
	platform.Record
	Roots             []platform.AssetRef    `json:"roots,omitempty" type:"json" title:"Portable definition roots"`
	Application       string                 `json:"application,omitempty" ref:"build.app" title:"Saved application draft"`
	CandidateID       string                 `json:"candidateId,omitempty" title:"Fixed candidate version"`
	Actors            []string               `json:"actors,omitempty" title:"Comparison members"`
	Sample            string                 `json:"sample,omitempty" title:"Application sample set"`
	Name              string                 `json:"name,omitempty" title:"Evaluation set name"`
	FunctionVersion   int                    `json:"functionVersion,omitempty" title:"Evaluated function version"`
	DefinitionVersion int                    `json:"definitionVersion,omitempty" field:"readonly"`
	Published         string                 `json:"published,omitempty" field:"readonly" type:"longtext"`
	Versions          []string               `json:"versions,omitempty" field:"readonly"`
	Title             string                 `json:"title" field:"required,search" title:"Test plan name"`
	Object            platform.Ref[Object]   `json:"object,omitempty" title:"Saved object draft"`
	Process           platform.Ref[Process]  `json:"process,omitempty" title:"Saved workflow draft"`
	Function          platform.Ref[Function] `json:"function,omitempty" title:"Saved function draft"`
	Model             string                 `json:"model,omitempty" title:"Model identifier"`
	As                string                 `json:"as,omitempty" title:"Member ID (empty: you)"`
	At                time.Time              `json:"at" field:"required" title:"Fixed test time"`
	Steps             []TestStep             `json:"steps" field:"required,aside" title:"Test steps"`
	Samples           []TestSample           `json:"samples,omitempty" field:"aside" type:"json"`
	Evaluation        []EvaluationPolicy     `json:"evaluation,omitempty" field:"aside" title:"Release evaluation thresholds"`
}

type EvaluationPolicy = platform.EvaluationPolicy
type EvaluationCase = platform.EvaluationCase

type TestSample = platform.SampleRecords
type TestStep = platform.TestStep
type ComputeFixture = platform.ComputeFixture
type FunctionFixture = platform.FunctionFixture
