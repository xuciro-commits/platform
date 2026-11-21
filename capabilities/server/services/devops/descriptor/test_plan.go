package descriptor

import (
	"time"

	"platformserver/platform"
)

type TestPlan[O, P, F any] struct {
	platform.Record
	Variant           string                      `json:"variant,omitempty" title:"Kind"`
	Organization      *platform.OrgSeed           `json:"organization,omitempty" type:"json" field:"aside" title:"Synthetic organization"`
	Roots             []platform.AssetRef         `json:"roots,omitempty" type:"json" title:"Definition roots" help:"Portable definition roots"`
	Application       string                      `json:"application,omitempty" ref:"build.app" title:"Application draft" help:"Saved application draft"`
	CandidateID       string                      `json:"candidateId,omitempty" title:"Release candidate" help:"Fixed candidate version"`
	Actors            []string                    `json:"actors,omitempty" title:"Comparison members"`
	Sample            string                      `json:"sample,omitempty" title:"Sample set" help:"Application sample set"`
	Name              string                      `json:"name,omitempty" title:"Evaluation set" help:"Evaluation set name"`
	FunctionVersion   int                         `json:"functionVersion,omitempty" title:"Function version" help:"Evaluated function version"`
	DefinitionVersion int                         `json:"definitionVersion,omitempty" field:"readonly"`
	Published         string                      `json:"published,omitempty" field:"readonly,internal" type:"longtext"`
	Versions          []string                    `json:"versions,omitempty" field:"readonly"`
	Title             string                      `json:"title" field:"required,search" title:"Name" help:"Test plan name"`
	Object            platform.Ref[O]             `json:"object,omitempty" title:"Object draft" help:"Saved object draft"`
	Process           platform.Ref[P]             `json:"process,omitempty" title:"Workflow draft" help:"Saved workflow draft"`
	Function          platform.Ref[F]             `json:"function,omitempty" title:"Function draft" help:"Saved function draft"`
	Model             string                      `json:"model,omitempty" title:"Model identifier"`
	As                string                      `json:"as,omitempty" title:"Member ID" help:"Member ID (empty: you)"`
	At                time.Time                   `json:"at" field:"required" title:"Test time" help:"Fixed test time"`
	Steps             []platform.TestStep         `json:"steps" field:"required,aside" title:"Test steps"`
	Samples           []platform.SampleRecords    `json:"samples,omitempty" field:"aside" type:"json"`
	Evaluation        []platform.EvaluationPolicy `json:"evaluation,omitempty" field:"aside" title:"Evaluation thresholds" help:"Release evaluation thresholds"`
}

func TestPlanKind(variant string) platform.AssetKind {
	if variant == "evaluation-set" {
		return platform.AssetEvaluation
	}
	return platform.AssetTestPlan
}
