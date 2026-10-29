package platform

import (
	"encoding/json"
	"fmt"
	"platformkernel/kernel"
	"slices"
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

func (m AIModel) Resolve(purpose string, override ModelOptions, ceiling int) (AIModelBinding, error) {
	o := m.Defaults.Override(override)
	b := AIModelBinding{Purpose: purpose, Model: m.Name(), Options: o, Revision: m.Revision, Capabilities: m.Capabilities}
	if o.InputTokens < 0 || o.MaxTokens < 0 {
		return b, fmt.Errorf("Token budgets must be positive")
	}
	if o.MaxTokens == 0 {
		o.MaxTokens = ceiling
		if c := m.Capabilities; c != nil {
			if c.OutputTokens != nil && (o.MaxTokens == 0 || o.MaxTokens > *c.OutputTokens) {
				o.MaxTokens = *c.OutputTokens
			}
			if c.ContextTokens != nil && (o.MaxTokens == 0 || o.MaxTokens > *c.ContextTokens-o.InputTokens) {
				o.MaxTokens = max(0, *c.ContextTokens-o.InputTokens)
			}
		}
		b.Options.MaxTokens = o.MaxTokens
	}
	if ceiling > 0 && o.MaxTokens > ceiling {
		return b, fmt.Errorf("Generation budget exceeds the organization limit (%d tokens)", ceiling)
	}

	c := m.Capabilities
	if c == nil {
		if o.Thinking != "" || o.Effort != "" {
			return b, fmt.Errorf("Refresh model capabilities before configuring thinking")
		}
		return b, nil
	}
	if (purpose == "generation" || purpose == "agent") && len(c.OutputModalities) > 0 && !slices.Contains(c.OutputModalities, "text") {
		return b, fmt.Errorf("This model does not support the selected purpose")
	}
	if purpose == "agent" && c.Tools != nil && !*c.Tools {
		return b, fmt.Errorf("This model does not support agent tools")
	}
	if purpose != "" && len(c.Purposes) > 0 && !slices.Contains(c.Purposes, purpose) {
		return b, fmt.Errorf("This model does not support the selected purpose")
	}
	for _, n := range []*int{c.InputTokens, c.OutputTokens, c.ContextTokens} {
		if n != nil && *n <= 0 {
			return b, fmt.Errorf("Provider token limits must be positive or unknown")
		}
	}
	if c.InputTokens != nil && o.InputTokens > *c.InputTokens {
		return b, fmt.Errorf("Input budget exceeds the model limit (%d tokens)", *c.InputTokens)
	}
	if c.OutputTokens != nil && o.MaxTokens > *c.OutputTokens {
		return b, fmt.Errorf("Generation budget exceeds the model limit (%d tokens)", *c.OutputTokens)
	}
	if c.ContextTokens != nil && o.InputTokens+o.MaxTokens > *c.ContextTokens {
		return b, fmt.Errorf("Input and generation budgets exceed the shared context window (%d tokens)", *c.ContextTokens)
	}
	if o.Thinking != "" && !slices.Contains(c.ThinkingModes, o.Thinking) {
		return b, fmt.Errorf("This thinking mode is not supported by the model and adapter")
	}
	if o.Effort != "" && (!slices.Contains(c.EffortLevels, o.Effort) || (o.Thinking != "enabled" && o.Thinking != "adaptive")) {
		return b, fmt.Errorf("This thinking level is not supported by the selected mode")
	}
	if (o.Thinking != "" || o.Effort != "") && c.Adapter != "deepseek" && c.Adapter != "anthropic-adaptive" {
		return b, fmt.Errorf("Thinking configuration is not implemented by this adapter")
	}
	return b, nil
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
	Entities(Member) []EntityInfo
	Catalog(Member) []Action
	ModelToolReads(Member) []string
	Read(Member, string, ...json.RawMessage) (any, *kernel.Error)
	ProposeModelTool(Member, ModelToolProposal, time.Time) *kernel.Error
}
