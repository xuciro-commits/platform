// Package ai is the platform's AI app (ADR-0015), on the app API (ADR-0025 D4).
package ai

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// AI is the platform's AI capability (ADR-0015): the providers a tenant uses,
// the models it enables and who may call them, and the usage of every call.
// Providers and models are its decisions; calls are made by the host
// (aicall.go) and each call's usage is journaled; prompts and answers are not.
const (
	ID                   = "ai"
	ProviderType         = "ai.provider"
	ModelType            = "ai.model"
	SchemaProviderAdd    = "ai.provider.add"
	SchemaProviderRemove = "ai.provider.remove"
	SchemaModelEnable    = "ai.model.enable"
	SchemaModelDisable   = "ai.model.disable"
	LimitType            = "ai.limit"
	SchemaLimitSet       = "ai.limit.set"
	SchemaLimitRemove    = "ai.limit.remove"
	// Settings of the defaults a limit overrides (ADR-0029 D1).
	SettingDailyTokens = "daily-tokens"     // tokens per person per day; 0: none
	SettingPerMinute   = "calls-per-minute" // calls per person or agent per minute; 0: none
	Admin              = "admin"
	User               = "user" // may call models open to users
)

// Vendor is a provider with a fixed base URL and wire.
type Vendor struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	BaseURL string `json:"baseUrl"`
	Wire    string `json:"wire"` // openai, anthropic
}

// Vendors are the fixed providers. Anthropic speaks its own Messages API
// through its official SDK (anthropic.go); the others speak the OpenAI wire.
var Vendors = []Vendor{
	{"anthropic", "Anthropic (Claude)", "https://api.anthropic.com", "anthropic"},
	{"openai", "OpenAI", "https://api.openai.com/v1", "openai"},
	{"gemini", "Google Gemini", "https://generativelanguage.googleapis.com/v1beta/openai", "openai"},
	{"moonshot", "Moonshot AI (Kimi)", "https://api.moonshot.cn/v1", "openai"},
	{"deepseek", "DeepSeek", "https://api.deepseek.com/v1", "openai"},
	{"qwen", "Alibaba Qwen (DashScope)", "https://dashscope.aliyuncs.com/compatible-mode/v1", "openai"},
	{"zhipu", "Zhipu GLM", "https://open.bigmodel.cn/api/paas/v4", "openai"},
	{"openrouter", "OpenRouter", "https://openrouter.ai/api/v1", "openai"},
}

// Provider is a source of models the tenant uses.
type Provider struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`             // vendor, compatible, local
	Vendor  string `json:"vendor,omitempty"` // for kind vendor
	BaseURL string `json:"baseUrl"`
	Secret  string `json:"secret,omitempty"` // the key's name in the secret store, never the key
	Wire    string `json:"wire"`
}

// Model is an enabled model and who may call it.
type Model struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`  // the provider's model ID
	Access   string `json:"access"` // everyone, users
	// DailyTokens caps the tokens the whole tenant spends on the model a day; 0: none.
	DailyTokens int `json:"dailyTokens,omitempty"`
}

// Limit overrides the defaults for one member, agent or app (ADR-0029 D1); 0 keeps the default.
type Limit struct {
	Member      string `json:"member"`
	DailyTokens int    `json:"dailyTokens,omitempty"`
	PerMinute   int    `json:"perMinute,omitempty"`
}

// Name is how callers name the model: "<provider>/<model>".
func (m Model) Name() string { return m.Provider + "/" + m.Model }

// Usage is one call's meter reading.
type Usage struct {
	At      time.Time `json:"at"`
	Member  string    `json:"member"`
	Agent   bool      `json:"agent,omitempty"`
	Model   string    `json:"model"`            // "<provider>/<model>"
	Served  string    `json:"served,omitempty"` // the model that answered, when the provider routes
	Input   int       `json:"input"`
	Output  int       `json:"output"`
	Cost    float64   `json:"cost,omitempty"` // USD, when the provider reports it
	Millis  int64     `json:"millis"`
	Outcome string    `json:"outcome"` // ok, or why it failed
}

const usageKept = 5000

type AI struct {
	mu        sync.Mutex
	providers []Provider
	models    []Model
	limits    map[string]Limit
	usage     []Usage
	// Tokens per day ("<day>|<member>", "<day>|model:<name>") and each
	// caller's calls of the last minute, derived from usage as it is metered.
	daily  map[string]int
	recent map[string][]time.Time
	ledger *platform.Ledger
}

// New is a tenant's AI app.
func New(tenant string) *AI {
	admin := []string{Admin}
	f := func(name, typ, description string, required bool) platform.Field {
		return platform.Field{Name: name, Type: typ, Required: required, Description: description}
	}
	return &AI{limits: map[string]Limit{}, daily: map[string]int{}, recent: map[string][]time.Time{}, ledger: platform.NewLedger(tenant, ID, platform.NewCatalog(
		platform.Action{Schema: SchemaProviderAdd, Target: ProviderType, Capability: "providers", Title: "Add AI provider", Roles: admin,
			Description: "Add a source of models: a vendor (Anthropic, OpenAI, Gemini, Moonshot, DeepSeek, Qwen, Zhipu, OpenRouter), a third-party OpenAI-compatible API, or a local model server (LM Studio, Ollama, llama.cpp).",
			Payload: []platform.Field{f("kind", "string", "vendor, compatible or local", true), f("vendor", "string", "For a vendor: its ID", false),
				f("baseUrl", "string", "For compatible and local: the API's base URL, e.g. http://host.docker.internal:1234/v1", false),
				f("secret", "string", "Name of the API key in the secret store (optional for local)", false)}},
		platform.Action{Schema: SchemaProviderRemove, Target: ProviderType, Capability: "providers", Title: "Remove AI provider", Roles: admin,
			Description: "Remove a provider; its models are disabled. Recorded usage stays.", Payload: []platform.Field{}},
		platform.Action{Schema: SchemaModelEnable, Target: ModelType, Capability: "models", Title: "Enable model", Roles: admin,
			Description: "Let members call a provider's model (target <provider>/<model>): everyone in the tenant, or members holding a role in the ai app.",
			Payload:     []platform.Field{f("access", "string", "everyone or users", true), f("dailyTokens", "integer", "Tokens the whole tenant may spend on it a day; 0: no cap", false)}},
		platform.Action{Schema: SchemaModelDisable, Target: ModelType, Capability: "models", Title: "Disable model", Roles: admin,
			Description: "Stop calls to a model.", Payload: []platform.Field{}},
		platform.Action{Schema: SchemaLimitSet, Target: LimitType, New: true, Capability: "limits", Title: "Set AI limit", Roles: admin,
			Description: "Limit what one member, agent or app (target its member ID) may call: tokens a day and calls a minute, in place of the defaults in the AI settings.",
			Payload:     []platform.Field{f("dailyTokens", "integer", "Tokens a day; 0: the default", false), f("perMinute", "integer", "Calls a minute; 0: the default", false)}},
		platform.Action{Schema: SchemaLimitRemove, Target: LimitType, Capability: "limits", Title: "Remove AI limit", Roles: admin,
			Description: "Return a member, agent or app to the default limits.", Payload: []platform.Field{}},
	), ProviderType, ModelType, LimitType)}
}

// Snapshot and Restore: providers, models and usage (ADR-0019 D6).
type aiState struct {
	Providers []Provider       `json:"providers"`
	Models    []Model          `json:"models"`
	Limits    map[string]Limit `json:"limits,omitempty"`
	Usage     []Usage          `json:"usage"`
	Daily     map[string]int   `json:"daily,omitempty"`
}

func (a *AI) Snapshot() (json.RawMessage, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ledger.SnapshotWith(aiState{a.providers, a.models, a.limits, a.usage, a.daily})
}

func (a *AI) Restore(raw json.RawMessage) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	var s aiState
	if err := a.ledger.RestoreWith(raw, &s); err != nil {
		return err
	}
	a.providers, a.models, a.limits, a.usage, a.daily = s.Providers, s.Models, s.Limits, s.Usage, s.Daily
	if a.limits == nil {
		a.limits = map[string]Limit{}
	}
	if a.daily == nil {
		a.daily = map[string]int{}
	}
	a.recent = map[string][]time.Time{}
	return nil
}

func (a *AI) Manifest() platform.Manifest {
	return platform.Manifest{ID: ID, Title: "AI", Version: "1", Actions: a.ledger.Catalog, Reads: []string{"ai-providers", "ai-models", "ai-usage", "ai-limits"},
		Everyone: []string{"ai-models", "ai-usage"}, Roles: []string{User}, // the user role opens models with access "users"
		Settings: []platform.Setting{
			{Name: SettingDailyTokens, Title: "Tokens per person per day", Type: "integer", Default: "0",
				Description: "What each person may spend on models a day, unless an AI limit says otherwise; 0: no limit. Agents have their own, in the Agents settings."},
			{Name: SettingPerMinute, Title: "Calls per minute", Type: "integer", Default: "60",
				Description: "How many model calls each person or agent may make a minute, unless an AI limit says otherwise; 0: no limit."}}}
}

func (a *AI) Declarations() []*pb.AuthorityDeclaration { return a.ledger.Declarations() }

func (a *AI) Input(platform.Caller, string, []byte, time.Time) (any, *kernel.Error) {
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
}

// Provider is a provider the tenant added, by ID.
func (a *AI) Provider(id string) (Provider, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.provider(id)
}

func (a *AI) provider(id string) (Provider, bool) {
	i := slices.IndexFunc(a.providers, func(p Provider) bool { return p.ID == id })
	if i < 0 {
		return Provider{}, false
	}
	return a.providers[i], true
}

func (a *AI) Submit(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		invalid := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
		notFound := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		id := s.GetTarget().GetId()
		var p struct {
			Kind, Vendor, BaseURL, Secret, Access string
			DailyTokens, PerMinute                int
		}
		if json.Unmarshal(s.GetPayload(), &p) != nil {
			return nil, invalid
		}
		switch s.GetSchema().GetName() {
		case SchemaProviderAdd:
			if _, known := a.provider(id); known {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
			}
			if id == "" || strings.Contains(id, "/") {
				return nil, invalid
			}
			pv := Provider{ID: id, Kind: p.Kind, Secret: p.Secret, Wire: "openai"}
			switch p.Kind {
			case "vendor":
				i := slices.IndexFunc(Vendors, func(v Vendor) bool { return v.ID == p.Vendor })
				if i < 0 || p.Secret == "" {
					return nil, invalid
				}
				pv.Vendor, pv.BaseURL, pv.Wire = p.Vendor, Vendors[i].BaseURL, Vendors[i].Wire
			case "compatible", "local":
				u, err := url.Parse(p.BaseURL)
				if err != nil || u.Host == "" || u.Scheme != "https" && !(u.Scheme == "http" && p.Kind == "local") || p.Kind == "compatible" && p.Secret == "" {
					return nil, invalid
				}
				pv.BaseURL = strings.TrimRight(p.BaseURL, "/")
			default:
				return nil, invalid
			}
			return func(*pb.ChangeRecord) { a.providers = append(a.providers, pv) }, nil
		case SchemaProviderRemove:
			if _, known := a.provider(id); !known {
				return nil, notFound
			}
			return func(*pb.ChangeRecord) {
				a.providers = slices.DeleteFunc(a.providers, func(x Provider) bool { return x.ID == id })
				a.models = slices.DeleteFunc(a.models, func(m Model) bool { return m.Provider == id })
			}, nil
		case SchemaModelEnable:
			provider, model, _ := strings.Cut(id, "/")
			if _, known := a.provider(provider); !known {
				return nil, notFound
			}
			if model == "" || p.Access != "everyone" && p.Access != "users" {
				return nil, invalid
			}
			return func(*pb.ChangeRecord) {
				a.models = slices.DeleteFunc(a.models, func(m Model) bool { return m.Name() == id })
				a.models = append(a.models, Model{Provider: provider, Model: model, Access: p.Access, DailyTokens: max(p.DailyTokens, 0)})
			}, nil
		case SchemaModelDisable:
			if !slices.ContainsFunc(a.models, func(m Model) bool { return m.Name() == id }) {
				return nil, notFound
			}
			return func(*pb.ChangeRecord) {
				a.models = slices.DeleteFunc(a.models, func(m Model) bool { return m.Name() == id })
			}, nil
		case SchemaLimitSet:
			if id == "" || p.DailyTokens < 0 || p.PerMinute < 0 {
				return nil, invalid
			}
			return func(*pb.ChangeRecord) {
				a.limits[id] = Limit{Member: id, DailyTokens: p.DailyTokens, PerMinute: p.PerMinute}
			}, nil
		case SchemaLimitRemove:
			if _, known := a.limits[id]; !known {
				return nil, notFound
			}
			return func(*pb.ChangeRecord) { delete(a.limits, id) }, nil
		}
		return nil, invalid
	})
}

// Callable is the enabled model name and its provider, when m may call it.
func (a *AI) Callable(m platform.Member, name string) (Model, Provider, *kernel.Error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	i := slices.IndexFunc(a.models, func(x Model) bool { return x.Name() == name })
	if i < 0 {
		return Model{}, Provider{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	if !a.allows(m, a.models[i]) {
		return Model{}, Provider{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	pv, _ := a.provider(a.models[i].Provider)
	return a.models[i], pv, nil
}

func (a *AI) allows(m platform.Member, x Model) bool {
	return x.Access == "everyone" || m.Roles[ID] != ""
}

// Meter records a call's usage (a journal entry of kind usage, live or replayed).
func (a *AI) Meter(u Usage) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.usage = append(a.usage, u)
	if len(a.usage) > usageKept {
		a.usage = a.usage[len(a.usage)-usageKept:]
	}
	day := u.At.UTC().Format(time.DateOnly)
	if _, today := a.daily[day+"|"+u.Member]; !today { // a new day: keep only it and the one before
		yesterday := u.At.UTC().AddDate(0, 0, -1).Format(time.DateOnly)
		for k := range a.daily {
			if !strings.HasPrefix(k, day) && !strings.HasPrefix(k, yesterday) {
				delete(a.daily, k)
			}
		}
	}
	a.daily[day+"|"+u.Member] += u.Input + u.Output
	a.daily[day+"|model:"+u.Model] += u.Input + u.Output
	a.recent[u.Member] = append(slices.DeleteFunc(a.recent[u.Member], func(t time.Time) bool { return u.At.Sub(t) >= time.Minute }), u.At)
}

// Defaults are the limits a member without its own takes (ADR-0029 D1):
// tokens a day for people and for agents, and calls a minute; 0: none.
type Defaults struct{ People, Agents, PerMinute int }

// Allow says why m may not call model now, or "" when it may: the model's
// daily cap for the tenant, then m's tokens today and calls this minute,
// against m's own limit or the defaults. Apps (app:<id>) take only their own.
func (a *AI) Allow(m platform.Member, model Model, d Defaults, now time.Time) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	day := now.UTC().Format(time.DateOnly)
	if model.DailyTokens > 0 && a.daily[day+"|model:"+model.Name()] >= model.DailyTokens {
		return fmt.Sprintf("The tenant used the %d tokens a day of %s", model.DailyTokens, model.Name())
	}
	own, app := a.limits[m.ID], strings.HasPrefix(m.ID, "app:")
	daily, perMinute := own.DailyTokens, own.PerMinute
	switch {
	case daily == 0 && m.Agent:
		daily = d.Agents
	case daily == 0 && !app:
		daily = d.People
	}
	if perMinute == 0 && !app {
		perMinute = d.PerMinute
	}
	if daily > 0 && a.daily[day+"|"+m.ID] >= daily {
		return fmt.Sprintf("%s used its %d tokens for today", m.ID, daily)
	}
	if perMinute > 0 && len(slices.DeleteFunc(slices.Clone(a.recent[m.ID]), func(t time.Time) bool { return now.Sub(t) >= time.Minute })) >= perMinute {
		return fmt.Sprintf("%s made its %d calls this minute", m.ID, perMinute)
	}
	return ""
}

// Total is usage summed per day, member and model.
type Total struct {
	Day    string  `json:"day"`
	Member string  `json:"member"`
	Model  string  `json:"model"`
	Calls  int     `json:"calls"`
	Failed int     `json:"failed"`
	Input  int     `json:"input"`
	Output int     `json:"output"`
	Cost   float64 `json:"cost"`
}

// Read "ai-providers" (administrators), "ai-models" (what the caller may call;
// administrators see every enabled model), "ai-usage" (the caller's own;
// administrators see everyone's): recent calls, newest first, and totals.
func (a *AI) Read(c platform.Caller, name string) (any, *kernel.Error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	admin := c.Role() == Admin
	switch name {
	case "ai-providers":
		if !admin {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
		}
		return append([]Provider{}, a.providers...), nil
	case "ai-limits":
		if !admin {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
		}
		out := slices.Collect(maps.Values(a.limits))
		slices.SortFunc(out, func(x, y Limit) int { return strings.Compare(x.Member, y.Member) })
		return out, nil
	case "ai-models":
		out := []Model{}
		for _, m := range a.models {
			if admin || a.allows(c.Member, m) {
				out = append(out, m)
			}
		}
		return out, nil
	}
	calls, totals := []Usage{}, []Total{}
	for i := len(a.usage) - 1; i >= 0; i-- {
		u := a.usage[i]
		if !admin && u.Member != c.ID {
			continue
		}
		if len(calls) < 200 {
			calls = append(calls, u)
		}
		day := u.At.UTC().Format(time.DateOnly)
		j := slices.IndexFunc(totals, func(t Total) bool { return t.Day == day && t.Member == u.Member && t.Model == u.Model })
		if j < 0 {
			totals, j = append(totals, Total{Day: day, Member: u.Member, Model: u.Model}), len(totals)
		}
		t := &totals[j]
		t.Calls, t.Input, t.Output, t.Cost = t.Calls+1, t.Input+u.Input, t.Output+u.Output, t.Cost+u.Cost
		if u.Outcome != "ok" {
			t.Failed++
		}
	}
	return AIUsage{Calls: calls, Totals: totals}, nil
}

// AIUsage is model calls metered from the journal, and their totals per day, member and model.
type AIUsage struct {
	Calls  []Usage `json:"calls"`
	Totals []Total `json:"totals"`
}

// Model is an enabled model by name, whatever the caller's access: agents call
// the model the tenant set for them (ADR-0021).
func (a *AI) Model(name string) (Model, Provider, *kernel.Error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	i := slices.IndexFunc(a.models, func(x Model) bool { return x.Name() == name })
	if i < 0 {
		return Model{}, Provider{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	pv, _ := a.provider(a.models[i].Provider)
	return a.models[i], pv, nil
}

// Spent is the tokens a member used on now's day (UTC).
func (a *AI) Spent(member string, now time.Time) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.daily[now.UTC().Format(time.DateOnly)+"|"+member]
}
