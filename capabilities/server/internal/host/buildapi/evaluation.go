package buildapi

import (
	"platformserver/platform"
)

const (
	EvaluationType         = "build.evaluation"
	SchemaEvaluationStart  = EvaluationType + ".start"
	SchemaEvaluationAnswer = EvaluationType + ".answer"
	EvaluationRepeats      = 3
)

// Evaluation is one retained observation of an immutable release candidate.
// Its samples are synthetic and builder-only; a new plan or configuration
// requires a new report rather than mutating this one.
type Evaluation struct {
	platform.Record
	Candidate        string              `json:"candidate" field:"readonly"`
	Function         string              `json:"function" field:"readonly"`
	Version          int                 `json:"version" field:"readonly"`
	Definition       string              `json:"definition" field:"readonly" type:"longtext"`
	DefinitionDigest string              `json:"definitionDigest" field:"readonly"`
	Plan             string              `json:"plan" field:"readonly"`
	PlanRevision     uint32              `json:"planRevision" field:"readonly"`
	PlanDigest       string              `json:"planDigest" field:"readonly"`
	Model            string              `json:"model" field:"readonly"`
	ModelConfig      string              `json:"modelConfig" field:"readonly"`
	Policy           []EvaluationPolicy  `json:"policy" field:"readonly"`
	State            string              `json:"state" field:"readonly" choices:"pending,passed,failed"`
	Quality          float64             `json:"quality" field:"readonly"`
	CostUSD          float64             `json:"costUsd" field:"readonly"`
	CostComplete     bool                `json:"costComplete" field:"readonly"`
	PeakLatency      int64               `json:"peakLatencyMillis" field:"readonly"`
	Attempts         []EvaluationAttempt `json:"attempts" field:"readonly"`
}

type EvaluationAttempt struct {
	Case           string  `json:"case"`
	Repeat         int     `json:"repeat"`
	Call           string  `json:"call"`
	Outcome        string  `json:"outcome,omitempty"`
	Answer         string  `json:"answer,omitempty"`
	Reason         string  `json:"reason,omitempty"`
	Matched        bool    `json:"matched,omitempty"`
	Metered        bool    `json:"metered,omitempty"`
	TokensReported bool    `json:"tokensReported,omitempty"`
	InputTokens    int     `json:"inputTokens,omitempty"`
	OutputTokens   int     `json:"outputTokens,omitempty"`
	CostReported   bool    `json:"costReported,omitempty"`
	CostUSD        float64 `json:"costUsd,omitempty"`
	LatencyMillis  int64   `json:"latencyMillis,omitempty"`
	ServedModel    string  `json:"servedModel,omitempty"`
}

// EvaluationStart is formed by the host after it has checked the saved
// candidate, current plan revision and model configuration. Only the host's
// Build automation identity may submit it.
type EvaluationStart struct {
	Candidate        string              `json:"candidate"`
	Function         string              `json:"function"`
	Version          int                 `json:"version"`
	Definition       platform.AIFunction `json:"definition"`
	DefinitionDigest string              `json:"definitionDigest"`
	Plan             string              `json:"plan"`
	PlanRevision     uint32              `json:"planRevision"`
	PlanDigest       string              `json:"planDigest"`
	Model            string              `json:"model"`
	ModelConfig      string              `json:"modelConfig"`
	Policy           EvaluationPolicy    `json:"policy"`
}
