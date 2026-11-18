package protocol

import (
	"context"
	"encoding/json"
	"platformserver/platform"
	"time"
)

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

func EstimateModelTokens(text string) int {
	if text == "" {
		return 0
	}
	return (len(text) + 2) / 3
}

type CatalogModel struct {
	ID           string                      `json:"id"`
	Name         string                      `json:"name,omitempty"`
	Context      int                         `json:"context,omitempty"`
	Free         bool                        `json:"free,omitempty"`
	PriceKnown   bool                        `json:"priceKnown,omitempty"`
	Capabilities *platform.ModelCapabilities `json:"capabilities,omitempty"`
}

type ModelToolProposal struct {
	ID      string
	Key     string
	Action  platform.Action
	Target  string
	Payload json.RawMessage
}

type ModelToolHost interface {
	ModelToolSurface(platform.Member) platform.AgentSurface
	AgentObjects(platform.Member, string, json.RawMessage, time.Time) (AgentObservation, *platform.Error)
	Catalog(platform.Member) []platform.Action
	ModelToolReads(platform.Member) []string
	Read(context.Context, platform.Member, string, ...json.RawMessage) (any, *platform.Error)
	ProposeModelTool(platform.Member, ModelToolProposal, time.Time) *platform.Error
}
