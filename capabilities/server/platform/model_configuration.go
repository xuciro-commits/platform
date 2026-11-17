package platform

import (
	"context"
	"encoding/json"
	"time"
)

type ModelCapabilities struct {
	Source                string    `json:"source"`
	ObservedAt            time.Time `json:"observedAt"`
	Purposes              []string  `json:"purposes,omitempty"`
	InputTokens           *int      `json:"inputTokens,omitempty"`
	OutputTokens          *int      `json:"outputTokens,omitempty"`
	ContextTokens         *int      `json:"contextTokens,omitempty"`
	Tools                 *bool     `json:"tools,omitempty"`
	Structured            *bool     `json:"structured,omitempty"`
	ThinkingModes         []string  `json:"thinkingModes,omitempty"`
	EffortLevels          []string  `json:"effortLevels,omitempty"`
	DeclaredThinkingModes []string  `json:"declaredThinkingModes,omitempty"`
	Parameters            []string  `json:"parameters,omitempty"`
	InputModalities       []string  `json:"inputModalities,omitempty"`
	OutputModalities      []string  `json:"outputModalities,omitempty"`

	Adapter string `json:"adapter,omitempty"`
}

type ModelOptions struct {
	InputTokens int    `json:"inputTokens,omitempty"`
	MaxTokens   int    `json:"maxTokens,omitempty"`
	Thinking    string `json:"thinking,omitempty"`
	Effort      string `json:"effort,omitempty"`
}

const (
	AIStageDesign  = "design"
	AIStageRuntime = "runtime"
)

type AIInferenceSite struct {
	Provider string `json:"provider"`
	Origin   string `json:"origin"`
	Location string `json:"location"`
}

type AIInferencePolicy struct {
	Runtime []AIInferenceSite `json:"runtime,omitempty"`
}

type AIModelBinding struct {
	Stage             string             `json:"stage,omitempty"`
	PolicyRevision    string             `json:"policyRevision,omitempty"`
	ProviderOrigin    string             `json:"providerOrigin,omitempty"`
	InferenceLocation string             `json:"inferenceLocation,omitempty"`
	Purpose           string             `json:"purpose,omitempty"`
	Model             string             `json:"model"`
	Options           ModelOptions       `json:"options"`
	Revision          int                `json:"revision,omitempty"`
	Capabilities      *ModelCapabilities `json:"capabilities,omitempty"`
	Provider          *AIProvider        `json:"provider,omitempty"`
}

func (o ModelOptions) Override(v ModelOptions) ModelOptions {
	if v.InputTokens != 0 {
		o.InputTokens = v.InputTokens
	}
	if v.MaxTokens != 0 {
		o.MaxTokens = v.MaxTokens
	}
	if v.Thinking != "" {
		o.Thinking = v.Thinking
	}
	if v.Effort != "" {
		o.Effort = v.Effort
	}
	return o
}

func EstimateModelTokens(text string) int {
	if text == "" {
		return 0
	}
	return (len(text) + 2) / 3
}

type CatalogModel struct {
	ID           string             `json:"id"`
	Name         string             `json:"name,omitempty"`
	Context      int                `json:"context,omitempty"`
	Free         bool               `json:"free,omitempty"`
	PriceKnown   bool               `json:"priceKnown,omitempty"`
	Capabilities *ModelCapabilities `json:"capabilities,omitempty"`
}

type ModelToolProposal struct {
	ID      string
	Key     string
	Action  Action
	Target  string
	Payload json.RawMessage
}
type ModelToolHost interface {
	ModelToolSurface(Member) AgentSurface
	AgentObjects(Member, string, json.RawMessage, time.Time) (AgentObservation, *Error)
	Catalog(Member) []Action
	ModelToolReads(Member) []string
	Read(context.Context, Member, string, ...json.RawMessage) (any, *Error)
	ProposeModelTool(Member, ModelToolProposal, time.Time) *Error
}
