package platform

import (
	"time"
)

type AIProvider struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Vendor  string `json:"vendor,omitempty"`
	BaseURL string `json:"baseUrl"`
	Secret  string `json:"secret,omitempty"`
	Wire    string `json:"wire"`
}

type AIModel struct {
	Provider     string             `json:"provider"`
	Model        string             `json:"model"`
	Access       string             `json:"access"`
	DailyTokens  int                `json:"dailyTokens,omitempty"`
	Capabilities *ModelCapabilities `json:"capabilities,omitempty"`
	Defaults     ModelOptions       `json:"defaults,omitzero"`
	Revision     int                `json:"revision,omitempty"`
}

type AIUsage struct {
	Call            string         `json:"call,omitempty"`
	Applications    []AssetBinding `json:"applications,omitempty"`
	Resource        *AssetRef      `json:"resource,omitempty"`
	Run             string         `json:"run,omitempty"`
	At              time.Time      `json:"at"`
	Member          string         `json:"member"`
	Agent           bool           `json:"agent,omitempty"`
	Model           string         `json:"model"`
	Served          string         `json:"served,omitempty"`
	Input           int            `json:"input"`
	Output          int            `json:"output"`
	CacheReadInput  *int           `json:"cacheReadInput,omitempty"`
	CacheWriteInput *int           `json:"cacheWriteInput,omitempty"`
	TokensReported  bool           `json:"tokensReported,omitempty"`
	TokensEstimated bool           `json:"tokensEstimated,omitempty"`
	Cost            float64        `json:"cost,omitempty"`
	CostReported    bool           `json:"costReported,omitempty"`
	Millis          int64          `json:"millis"`
	Outcome         string         `json:"outcome"`
}

type AIQuota struct {
	ReservedTokens int `json:"reservedTokens"`
	UnsettledCalls int `json:"unsettledCalls"`
	DailyTokens    int `json:"dailyTokens"`
	UsedTokens     int `json:"usedTokens"`
	PerMinute      int `json:"perMinute"`
	UsedCalls      int `json:"usedCalls"`
}

func (m AIModel) Name() string { return m.Provider + "/" + m.Model }
