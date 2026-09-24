package platformserver

import (
	"encoding/json"
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
	AIApp                = "ai"
	ProviderType         = "ai.provider"
	ModelType            = "ai.model"
	SchemaProviderAdd    = "ai.provider.add"
	SchemaProviderRemove = "ai.provider.remove"
	SchemaModelEnable    = "ai.model.enable"
	SchemaModelDisable   = "ai.model.disable"
	AIAdmin              = "admin"
	AIUser               = "user" // may call models open to users
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
	usage     []Usage
	ledger    *platform.Ledger
}

func NewAI(tenant string) *AI {
	admin := []string{AIAdmin}
	f := func(name, typ, description string, required bool) platform.Field {
		return platform.Field{Name: name, Type: typ, Required: required, Description: description}
	}
	return &AI{ledger: platform.NewLedger(tenant, AIApp, platform.NewCatalog(
		platform.Action{Schema: SchemaProviderAdd, Target: ProviderType, Capability: "providers", Title: "Add AI provider", Roles: admin,
			Description: "Add a source of models: a vendor (Anthropic, OpenAI, Gemini, Moonshot, DeepSeek, Qwen, Zhipu, OpenRouter), a third-party OpenAI-compatible API, or a local model server (LM Studio, Ollama, llama.cpp).",
			Payload: []platform.Field{f("kind", "string", "vendor, compatible or local", true), f("vendor", "string", "For a vendor: its ID", false),
				f("baseUrl", "string", "For compatible and local: the API's base URL, e.g. http://host.docker.internal:1234/v1", false),
				f("secret", "string", "Name of the API key in the secret store (optional for local)", false)}},
		platform.Action{Schema: SchemaProviderRemove, Target: ProviderType, Capability: "providers", Title: "Remove AI provider", Roles: admin,
			Description: "Remove a provider; its models are disabled. Recorded usage stays.", Payload: []platform.Field{}},
		platform.Action{Schema: SchemaModelEnable, Target: ModelType, Capability: "models", Title: "Enable model", Roles: admin,
			Description: "Let members call a provider's model (target <provider>/<model>): everyone in the tenant, or members holding a role in the ai app.",
			Payload:     []platform.Field{f("access", "string", "everyone or users", true)}},
		platform.Action{Schema: SchemaModelDisable, Target: ModelType, Capability: "models", Title: "Disable model", Roles: admin,
			Description: "Stop calls to a model.", Payload: []platform.Field{}},
	), ProviderType, ModelType)}
}

func (a *AI) Manifest() platform.Manifest {
	return platform.Manifest{ID: AIApp, Version: "1", Actions: a.ledger.Catalog, Reads: []string{"ai-providers", "ai-models", "ai-usage"},
		Everyone: []string{"ai-models", "ai-usage"}, Roles: []string{AIUser}} // the user role opens models with access "users"
}

func (a *AI) Declarations() []*pb.AuthorityDeclaration { return a.ledger.Declarations() }

func (a *AI) Input(platform.Caller, string, []byte, time.Time) (any, *kernel.Error) {
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
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
				a.models = append(a.models, Model{Provider: provider, Model: model, Access: p.Access})
			}, nil
		case SchemaModelDisable:
			if !slices.ContainsFunc(a.models, func(m Model) bool { return m.Name() == id }) {
				return nil, notFound
			}
			return func(*pb.ChangeRecord) {
				a.models = slices.DeleteFunc(a.models, func(m Model) bool { return m.Name() == id })
			}, nil
		}
		return nil, invalid
	})
}

// callable is the enabled model name and its provider, when m may call it.
func (a *AI) callable(m platform.Member, name string) (Model, Provider, *kernel.Error) {
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
	return x.Access == "everyone" || m.Roles[AIApp] != ""
}

// meter records a call's usage (a journal entry of kind usage, live or replayed).
func (a *AI) meter(u Usage) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.usage = append(a.usage, u)
	if len(a.usage) > usageKept {
		a.usage = a.usage[len(a.usage)-usageKept:]
	}
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
	admin := c.Role() == AIAdmin
	switch name {
	case "ai-providers":
		if !admin {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
		}
		return append([]Provider{}, a.providers...), nil
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
	return map[string]any{"calls": calls, "totals": totals}, nil
}
