package buildapi

import (
	"encoding/json"

	"time"

	"platformserver/platform"
)

const TestPlanType = "build.testplan"

// TestPlan stores fixed inputs, not test state or a claim about the next draft.
type TestPlan struct {
	platform.Record
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
