package protocol

import (
	"context"
	"encoding/json"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
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

type ModelToolCatalog struct {
	Entities       []platform.RecordTypeInfo
	Actions        []platform.Action
	Applications   []ApplicationView
	HiddenAIFields map[string][]string
}

type ModelObjectQuery struct {
	Set      platform.ObjectSet
	Sort     []string
	Offset   int
	Limit    int
	Groups   []string
	Measures []string
}

type ModelToolHost interface {
	TenantID() string
	ModelToolCatalog(platform.Member) ModelToolCatalog
	QueryModelObjects(platform.Member, ModelObjectQuery, time.Time) (json.RawMessage, *platform.Error)
	SubmitModelAction(context.Context, platform.Member, *pb.Submission, time.Time) (*pb.ChangeRecord, *platform.Error, []platform.FieldIssue)
}
