package descriptor

import (
	"platformserver/platform"
)

type FunctionEvaluationPlan struct {
	Revision   uint32
	Archived   bool
	Name       string
	Function   string
	Model      string
	Evaluation []platform.EvaluationPolicy
	Digest     func(string, []platform.EvaluationPolicy) (string, error)
}

type FunctionEvaluationStart struct {
	Set              *platform.AssetBinding    `json:"evaluationSet,omitempty"`
	Candidate        string                    `json:"candidate"`
	Function         string                    `json:"function"`
	Version          int                       `json:"version"`
	Definition       platform.AIFunction       `json:"definition"`
	DefinitionDigest string                    `json:"definitionDigest"`
	Plan             string                    `json:"plan"`
	PlanRevision     uint32                    `json:"planRevision"`
	PlanDigest       string                    `json:"planDigest"`
	Model            string                    `json:"model"`
	ModelConfig      string                    `json:"modelConfig"`
	Policy           platform.EvaluationPolicy `json:"policy"`
}
