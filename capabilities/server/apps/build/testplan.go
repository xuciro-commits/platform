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

const TestPlanType = "build.testplan"

// TestPlan stores fixed inputs, not test state or a claim about the next draft.
type TestPlan struct {
	platform.Record
	Title      string                 `json:"title" field:"required,search" title:"Test plan name"`
	Object     platform.Ref[Object]   `json:"object,omitempty" title:"Saved object draft"`
	Process    platform.Ref[Process]  `json:"process,omitempty" title:"Saved workflow draft"`
	Function   platform.Ref[Function] `json:"function,omitempty" title:"Saved function draft"`
	Model      string                 `json:"model,omitempty" title:"Model identifier"`
	As         string                 `json:"as,omitempty" title:"Member ID (empty: you)"`
	At         time.Time              `json:"at" field:"required" title:"Fixed test time"`
	Steps      []TestStep             `json:"steps" field:"required,aside" title:"Test steps"`
	Samples    []TestSample           `json:"samples,omitempty" field:"aside" type:"json"`
	Evaluation []EvaluationPolicy     `json:"evaluation,omitempty" field:"aside" title:"Release evaluation thresholds"`
}

// EvaluationPolicy is a task-specific gate for three real-model runs of each
// synthetic function case. Cost is the total reported USD amount; latency is
// a per-call ceiling. Missing provider cost can never satisfy this policy.
type EvaluationPolicy struct {
	MinQuality       float64          `json:"minQuality"`
	MaxCostUSD       float64          `json:"maxCostUsd"`
	MaxLatencyMillis int64            `json:"maxLatencyMillis"`
	Cases            []EvaluationCase `json:"cases"`
}

// EvaluationCase fixes only synthetic input and a strict typed reference; it
// never copies a production record into an external model request.
type EvaluationCase struct {
	Name     string          `json:"name"`
	Input    json.RawMessage `json:"input"`
	Expected json.RawMessage `json:"expected"`
}

type TestSample struct {
	Type    string            `json:"type"`
	Records []json.RawMessage `json:"records"`
}

type TestStep struct {
	Type           string           `json:"type"`
	ID             string           `json:"id"`
	Action         string           `json:"action"`
	Payload        string           `json:"payload"`
	Expect         string           `json:"expect" title:"Expected outcome"`
	As             string           `json:"as,omitempty" title:"Step member ID (empty: plan member)"`
	AdvanceSeconds int              `json:"advanceSeconds,omitempty" title:"Advance clock (seconds)"`
	Flow           string           `json:"flow,omitempty"`
	Step           string           `json:"step,omitempty"`
	Answer         string           `json:"answer,omitempty"`
	Function       *FunctionFixture `json:"function,omitempty"`
	Compute        *ComputeFixture  `json:"compute,omitempty" type:"json"`
}

// ComputeFixture fixes only a computation answer. Input admission, Schema,
// K9 generation and Flow continuation still use the ordinary host path.
type ComputeFixture struct {
	App          string `json:"app,omitempty"`
	Name         string `json:"name"`
	Output       string `json:"output,omitempty"`
	Error        string `json:"error,omitempty"`
	ExpectState  string `json:"expectState" enum:"completed,failed"`
	ExpectOutput string `json:"expectOutput,omitempty"`
}

func (f ComputeFixture) Check() bool {
	return f.Name != "" && len(f.Name) <= 64 && len(f.Output) <= 48<<10 && len(f.Error) <= 1024 && len(f.ExpectOutput) <= 48<<10 && (f.ExpectState == "completed" && json.Valid([]byte(f.Output)) || f.ExpectState == "failed" && f.Error != "") && (f.ExpectOutput == "" || json.Valid([]byte(f.ExpectOutput)))
}

// FunctionFixture fixes a provider answer for the ordinary model effect path.
// It is test data, with no transport, credentials or executable expressions.
type FunctionFixture struct {
	Output       string `json:"output"`
	InputTokens  int    `json:"inputTokens"`
	OutputTokens int    `json:"outputTokens"`
	ExpectState  string `json:"expectState" enum:"ready,rejected"`
	ExpectOutput string `json:"expectOutput,omitempty"`
}

func (f FunctionFixture) Check() bool {
	return len(f.Output) <= 64<<10 && len(f.ExpectOutput) <= 32<<10 && f.InputTokens >= 0 && f.InputTokens <= 1<<20 &&
		f.OutputTokens >= 0 && f.OutputTokens <= 1<<20 && (f.ExpectState == "ready" || f.ExpectState == "rejected") &&
		(f.ExpectOutput == "" || f.ExpectState == "ready" && json.Valid([]byte(f.ExpectOutput)))
}

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
