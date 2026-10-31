package platform

import (
	"time"
)

type AIVendor struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	BaseURL string `json:"baseUrl"`
	Wire    string `json:"wire"`
}

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

type AILimit struct {
	Member      string `json:"member"`
	DailyTokens int    `json:"dailyTokens,omitempty"`
	PerMinute   int    `json:"perMinute,omitempty"`
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

type AIDefaults struct{ People, Agents, PerMinute int }

type AIReservation struct {
	RequestHash string    `json:"requestHash,omitempty"`
	Call        string    `json:"call"`
	At          time.Time `json:"at"`
	Member      string    `json:"member"`
	Agent       bool      `json:"agent,omitempty"`
	Model       string    `json:"model"`
	Run         string    `json:"run,omitempty"`
	Input       int       `json:"input"`
	Output      int       `json:"output"`
	Accounted   int       `json:"accounted,omitempty"`
	State       string    `json:"state"`
}

type AIReserveInput struct {
	Cancel      bool          `json:"cancel,omitempty"`
	Reservation AIReservation `json:"reservation"`
	Defaults    AIDefaults    `json:"defaults"`
	RunTokens   int           `json:"runTokens,omitempty"`
}

const AIReserveCall = "ai-reserve-call"

type AIQuota struct {
	ReservedTokens int `json:"reservedTokens"`
	UnsettledCalls int `json:"unsettledCalls"`
	DailyTokens    int `json:"dailyTokens"`
	UsedTokens     int `json:"usedTokens"`
	PerMinute      int `json:"perMinute"`
	UsedCalls      int `json:"usedCalls"`
}

type AITotal struct {
	Day         string  `json:"day"`
	Member      string  `json:"member"`
	Model       string  `json:"model"`
	Calls       int     `json:"calls"`
	Failed      int     `json:"failed"`
	Input       int     `json:"input"`
	Output      int     `json:"output"`
	Cost        float64 `json:"cost"`
	Reported    int     `json:"reported"`
	Estimated   int     `json:"estimated"`
	CostUnknown int     `json:"costUnknown"`
}

type AIApplicationTotal struct {
	Resource    *AssetRef    `json:"resource,omitempty"`
	Application AssetBinding `json:"application"`
	Calls       int          `json:"calls"`
	SharedCalls int          `json:"sharedCalls"`
	Input       int          `json:"input"`
	Output      int          `json:"output"`
	Cost        float64      `json:"cost"`
	CostUnknown int          `json:"costUnknown"`
}

type AIUsageReport struct {
	Reservations []AIReservation      `json:"reservations,omitempty"`
	Applications []AIApplicationTotal `json:"applications,omitempty"`
	Calls        []AIUsage            `json:"calls"`
	Totals       []AITotal            `json:"totals"`
}

func (m AIModel) Name() string { return m.Provider + "/" + m.Model }

const AIApp = "ai"

const AIProviderType = "ai.provider"

const AIModelType = "ai.model"

const AISchemaProviderAdd = "ai.provider.add"

const AISchemaProviderEdit = "ai.provider.edit"

const AISchemaProviderRemove = "ai.provider.remove"

const AISchemaModelEnable = "ai.model.enable"

const AISchemaModelDisable = "ai.model.disable"

const AILimitType = "ai.limit"

const AISchemaLimitSet = "ai.limit.set"

const AISchemaLimitRemove = "ai.limit.remove"

const AISettingDailyTokens = "daily-tokens"

const AISettingPerMinute = "calls-per-minute"

const AISettingAppModel = "app-model"

const AISettingMaxTokens = "max-tokens"

const AIAdmin = "admin"

const AIUser = "user"

var AIVendors = []AIVendor{
	{"anthropic", "Anthropic (Claude)", "https://api.anthropic.com", "anthropic"},
	{"openai", "OpenAI", "https://api.openai.com/v1", "openai"},
	{"gemini", "Google Gemini", "https://generativelanguage.googleapis.com/v1beta/openai", "openai"},
	{"moonshot", "Moonshot AI (Kimi)", "https://api.moonshot.cn/v1", "openai"},
	{"deepseek", "DeepSeek", "https://api.deepseek.com/v1", "openai"},
	{"qwen", "Alibaba Qwen (DashScope)", "https://dashscope.aliyuncs.com/compatible-mode/v1", "openai"},
	{"zhipu", "Zhipu GLM", "https://open.bigmodel.cn/api/paas/v4", "openai"},
	{"openrouter", "OpenRouter", "https://openrouter.ai/api/v1", "openai"},
}
