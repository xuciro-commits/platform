package construction

import (
	"time"

	"platformserver/platform"
)

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
