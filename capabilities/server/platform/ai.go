package platform

import "time"

type AIVendor struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	BaseURL string `json:"baseUrl"`
	Wire    string `json:"wire"` // openai, anthropic
}

type AIProvider struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`             // vendor, compatible, local
	Vendor  string `json:"vendor,omitempty"` // for kind vendor
	BaseURL string `json:"baseUrl"`
	Secret  string `json:"secret,omitempty"` // the key's name in the secret store, never the key
	Wire    string `json:"wire"`
}

type AIModel struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`  // the provider's model ID
	Access   string `json:"access"` // everyone, users
	// DailyTokens caps the tokens the whole tenant spends on the model a day; 0: none.
	DailyTokens int `json:"dailyTokens,omitempty"`
}

type AILimit struct {
	Member      string `json:"member"`
	DailyTokens int    `json:"dailyTokens,omitempty"`
	PerMinute   int    `json:"perMinute,omitempty"`
}

type AIUsage struct {
	Applications   []AssetBinding `json:"applications,omitempty"`
	Resource       *AssetRef      `json:"resource,omitempty"`
	Run            string         `json:"run,omitempty"`
	At             time.Time      `json:"at"`
	Member         string         `json:"member"`
	Agent          bool           `json:"agent,omitempty"`
	Model          string         `json:"model"`            // "<provider>/<model>"
	Served         string         `json:"served,omitempty"` // the model that answered, when the provider routes
	Input          int            `json:"input"`
	Output         int            `json:"output"`
	TokensReported bool           `json:"tokensReported,omitempty"`
	// TokensEstimated marks usage the provider did not report: the numbers are
	// the platform's conservative estimate, so an unknown call is never booked
	// as a free one (ADR-0050 D7, review AI-05).
	TokensEstimated bool    `json:"tokensEstimated,omitempty"`
	Cost            float64 `json:"cost,omitempty"` // USD, when the provider reports it
	CostReported    bool    `json:"costReported,omitempty"`
	Millis          int64   `json:"millis"`
	Outcome         string  `json:"outcome"` // ok, or why it failed
}

type AIDefaults struct{ People, Agents, PerMinute int }

// AIQuota is an original owner counter/limit projection, not an app quota.
type AIQuota struct {
	DailyTokens int `json:"dailyTokens"`
	UsedTokens  int `json:"usedTokens"`
	PerMinute   int `json:"perMinute"`
	UsedCalls   int `json:"usedCalls"`
}

type AITotal struct {
	Day    string  `json:"day"`
	Member string  `json:"member"`
	Model  string  `json:"model"`
	Calls  int     `json:"calls"`
	Failed int     `json:"failed"`
	Input  int     `json:"input"`
	Output int     `json:"output"`
	Cost   float64 `json:"cost"`
	// Reported and Estimated split Input+Output into what the provider said
	// and what the platform had to estimate; CostUnknown counts the calls
	// whose cost no provider reported, so a cost of 0 is never read as free
	// when it was simply not said (ADR-0050 D7, review AI-05).
	Reported    int `json:"reported"`
	Estimated   int `json:"estimated"`
	CostUnknown int `json:"costUnknown"`
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
	Applications []AIApplicationTotal `json:"applications,omitempty"`
	Calls        []AIUsage            `json:"calls"`
	Totals       []AITotal            `json:"totals"`
}

func (m AIModel) Name() string { return m.Provider + "/" + m.Model }

const AIApp = "ai"

const AIProviderType = "ai.provider"

const AIModelType = "ai.model"

const AISchemaProviderAdd = "ai.provider.add"

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
