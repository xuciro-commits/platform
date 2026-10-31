package platform

import (
	"encoding/json"
	"platformkernel/kernel"
	"time"
)

type AITrainingPolicy struct {
	ID              string    `json:"id"`
	Revision        int       `json:"revision"`
	Enabled         bool      `json:"enabled"`
	EffectiveAt     time.Time `json:"effectiveAt"`
	Fields          []string  `json:"fields"`
	TrainingAllowed bool      `json:"trainingAllowed"`
	ProviderAllowed bool      `json:"providerAllowed"`
	QualityRule     string    `json:"qualityRule,omitempty"`
	RetainDays      int       `json:"retainDays"`
}

type AITrainingCandidate struct {
	Authorization    string                     `json:"authorization,omitempty"`
	ActionDefinition string                     `json:"actionDefinition,omitempty"`
	ModelConfig      string                     `json:"modelConfig,omitempty"`
	Target           string                     `json:"target,omitempty"`
	ExpectedSources  map[string]uint32          `json:"expectedSources,omitempty"`
	ID               string                     `json:"id"`
	Tenant           string                     `json:"tenant"`
	Intent           string                     `json:"intent"`
	Run              string                     `json:"run"`
	Step             int                        `json:"step"`
	App              string                     `json:"app"`
	Action           string                     `json:"action"`
	Definition       string                     `json:"definition"`
	Sources          map[string]uint32          `json:"sources"`
	SourceFields     map[string][]string        `json:"sourceFields,omitempty"`
	Model            string                     `json:"model,omitempty"`
	PolicyRevision   string                     `json:"policyRevision,omitempty"`
	Evidence         string                     `json:"evidence"`
	At               time.Time                  `json:"at"`
	Policy           string                     `json:"policy,omitempty"`
	PolicyVersion    int                        `json:"policyVersion,omitempty"`
	Outcome          string                     `json:"outcome"`
	Chosen           map[string]json.RawMessage `json:"chosen,omitempty"`
	Rejected         map[string]json.RawMessage `json:"rejected,omitempty"`
	Critique         string                     `json:"critique,omitempty"`
	QualityEvidence  string                     `json:"qualityEvidence,omitempty"`
	Approved         bool                       `json:"approved"`
	Revoked          bool                       `json:"revoked"`
	Problems         []string                   `json:"problems,omitempty"`
}

type AITrainingEvidence struct {
	Candidates []AITrainingCandidate `json:"candidates"`
}

type AITrainingHost interface {
	TrainingEvidence(Event) AITrainingEvidence
	TrainingReadable(Member, AITrainingCandidate, time.Time) string
}

type AttachedAITraining interface{ AttachAITraining(AITrainingHost) }

type AITrainingReport struct {
	Policies   []AITrainingPolicy    `json:"policies"`
	Candidates []AITrainingCandidate `json:"candidates"`
}

type AITrainingExport struct {
	ID       string          `json:"id"`
	Format   string          `json:"format"`
	JSONL    string          `json:"jsonl"`
	Manifest json.RawMessage `json:"manifest"`
}

type AITrainingOwner interface {
	TrainingExport(Caller, string, time.Time) (AITrainingExport, *kernel.Error)
}
