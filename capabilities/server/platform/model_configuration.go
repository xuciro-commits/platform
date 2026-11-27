package platform

import "time"

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
