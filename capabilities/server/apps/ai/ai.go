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
	ID                   = platform.AIApp
	ProviderType         = platform.AIProviderType
	ModelType            = platform.AIModelType
	SchemaProviderAdd    = platform.AISchemaProviderAdd
	SchemaProviderRemove = platform.AISchemaProviderRemove
	SchemaModelEnable    = platform.AISchemaModelEnable
	SchemaModelDisable   = platform.AISchemaModelDisable
	LimitType            = platform.AILimitType
	SchemaLimitSet       = platform.AISchemaLimitSet
	SchemaLimitRemove    = platform.AISchemaLimitRemove
	// Settings of the defaults a limit overrides (ADR-0029 D1).
	SettingDailyTokens = platform.AISettingDailyTokens // tokens per person per day; 0: none
	SettingPerMinute   = platform.AISettingPerMinute   // calls per person or agent per minute; 0: none
	SettingAppModel    = platform.AISettingAppModel    // the model apps ask when they name none (ADR-0029 D3)
	SettingMaxTokens   = platform.AISettingMaxTokens   // the most tokens one answer may ask for; 0: the provider's default
	Admin              = platform.AIAdmin
	User               = platform.AIUser // may call models open to users
)

// Vendor is a provider with a fixed base URL and wire.
type Vendor = platform.AIVendor

// Vendors are the fixed providers. Anthropic speaks its own Messages API
// through its official SDK (anthropic.go); the others speak the OpenAI wire.
var Vendors = platform.AIVendors

// Provider is a source of models the tenant uses.
type Provider = platform.AIProvider

// Model is an enabled model and who may call it.
type Model = platform.AIModel

// Limit overrides the defaults for one member, agent or app (ADR-0029 D1); 0 keeps the default.
type Limit = platform.AILimit

// Name is how callers name the model: "<provider>/<model>".

// Usage is one call's meter reading.
type Usage = platform.AIUsage

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
	// inflight are the calls that passed the door and whose usage is not
	// metered yet: a call in flight counts against its caller's minute, so
	// calls that start together cannot each pass the same limit (ADR-0050 D4).
	inflight map[string][]time.Time
	ledger   *platform.Ledger
}

// New is a tenant's AI app.
func New(tenant string) *AI {
	admin := []string{Admin}
	f := func(name, typ, description string, required bool) platform.Field {
		return platform.Field{Name: name, Type: typ, Required: required, Description: description}
	}
	return &AI{limits: map[string]Limit{}, daily: map[string]int{}, recent: map[string][]time.Time{}, inflight: map[string][]time.Time{}, ledger: platform.NewLedger(tenant, ID, platform.NewCatalog(
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
	a.inflight = map[string][]time.Time{} // calls in flight do not survive a restore
	return nil
}

func (a *AI) Manifest() platform.Manifest {
	return platform.Manifest{ID: ID, Title: "AI", Version: "1", Actions: a.ledger.Catalog, Reads: []string{"ai-providers", "ai-models", "ai-usage", "ai-limits"},
		ReadSources: map[string]string{"ai-providers": "record", "ai-models": "record", "ai-usage": "record", "ai-limits": "record"},
		Everyone:    []string{"ai-models", "ai-usage"}, Roles: []string{User}, // the user role opens models with access "users"
		Settings: []platform.Setting{
			{Name: SettingDailyTokens, Title: "Tokens per person per day", Type: "integer", Default: "0",
				Description: "What each person may spend on models a day, unless an AI limit says otherwise; 0: no limit. Agents have their own, in the Agents settings."},
			{Name: SettingAppModel, Title: "Model apps ask", Type: "text", Default: "",
				Description: "The enabled model (<provider>/<model>) an app's request asks when it names none, such as a ticket's summary; empty: such requests are refused."},
			{Name: SettingPerMinute, Title: "Calls per minute", Type: "integer", Default: "60",
				Description: "How many model calls each person or agent may make a minute, unless an AI limit says otherwise; 0: no limit."},
			{Name: SettingMaxTokens, Title: "Most tokens one answer", Type: "integer", Default: "8192",
				Description: "The most tokens one model answer may ask for; a call asking for more is capped to this. 0: whatever the provider's own default allows."}}}
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
	// The call this usage belongs to is no longer in flight: its booking is
	// released by the meter reading that replaces it, in the same lock.
	if calls := a.inflight[u.Member]; len(calls) > 0 {
		a.inflight[u.Member] = calls[1:]
	}
}

// Usage is the usage kept, oldest first.
func (a *AI) Usage() []Usage {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.usage)
}

// Defaults are the limits a member without its own takes (ADR-0029 D1):
// tokens a day for people and for agents, and calls a minute; 0: none.
type Defaults = platform.AIDefaults

// allowLocked says why m may not call model now, or "" when it may: the
// model's daily cap for the tenant, then m's tokens today and calls this
// minute, against m's own limit or the defaults. Apps (app:<id>) take only
// their own. Calls already in flight count against the minute: they passed the
// door but their usage is not metered yet. Only Reserve takes the door.
func (a *AI) allowLocked(m platform.Member, model Model, d Defaults, now time.Time) string {
	day := now.UTC().Format(time.DateOnly)
	if model.DailyTokens > 0 && a.daily[day+"|model:"+model.Name()] >= model.DailyTokens {
		return fmt.Sprintf("The tenant used the %d tokens a day of %s", model.DailyTokens, model.Name())
	}
	daily, perMinute := a.effectiveLimitsLocked(m, d)
	if daily > 0 && a.daily[day+"|"+m.ID] >= daily {
		return fmt.Sprintf("%s used its %d tokens for today", m.ID, daily)
	}
	if perMinute > 0 && a.callsLocked(m.ID, now) >= perMinute {
		return fmt.Sprintf("%s made its %d calls this minute", m.ID, perMinute)
	}
	return ""
}

func (a *AI) effectiveLimitsLocked(m platform.Member, d Defaults) (int, int) {
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
	return daily, perMinute
}

func (a *AI) Quota(m platform.Member, d Defaults, now time.Time) platform.AIQuota {
	a.mu.Lock()
	defer a.mu.Unlock()
	daily, perMinute := a.effectiveLimitsLocked(m, d)
	return platform.AIQuota{DailyTokens: daily, PerMinute: perMinute, UsedTokens: a.daily[now.UTC().Format(time.DateOnly)+"|"+m.ID], UsedCalls: a.callsLocked(m.ID, now)}
}

// callsLocked is the caller's calls this minute: the ones already metered and
// the ones in flight.
func (a *AI) callsLocked(member string, now time.Time) int {
	recent := slices.DeleteFunc(slices.Clone(a.recent[member]), func(t time.Time) bool { return now.Sub(t) >= time.Minute })
	inflight := slices.DeleteFunc(slices.Clone(a.inflight[member]), func(t time.Time) bool { return now.Sub(t) >= time.Minute })
	return len(recent) + len(inflight)
}

// Reserve is the door one model call passes, taken atomically with the call's
// registration: it refuses with why, or lets the call start and counts it as in
// flight until its usage is metered. It is what every model call takes instead
// of a bare Allow — calls that start together would each pass the same limit
// otherwise (ADR-0050 D4, review AI-03).
func (a *AI) Reserve(m platform.Member, model Model, d Defaults, now time.Time) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.pruneInflightLocked(now)
	if why := a.allowLocked(m, model, d, now); why != "" {
		return why
	}
	a.inflight[m.ID] = append(a.inflight[m.ID], now)
	return ""
}

// pruneInflightLocked forgets calls in flight beyond a minute: a call the
// process died before metering must not hold its caller's limits forever.
func (a *AI) pruneInflightLocked(now time.Time) {
	for member, calls := range a.inflight {
		if left := slices.DeleteFunc(calls, func(t time.Time) bool { return now.Sub(t) >= time.Minute }); len(left) == 0 {
			delete(a.inflight, member)
		} else {
			a.inflight[member] = left
		}
	}
}

// Total is usage summed per day, member and model.
type Total = platform.AITotal

// Read "ai-providers" (administrators), "ai-models" (what the caller may call;
// administrators see every enabled model), "ai-usage" (the caller's own;
// administrators see everyone's): recent calls, newest first, and totals.
func (a *AI) Read(c platform.Caller, name string) (any, *kernel.Error) {
	visibleApps := c.VisibleApplications()
	a.mu.Lock()
	defer a.mu.Unlock()
	admin := c.Holds(c.App, Admin)
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
	byApplication := []platform.AIApplicationTotal{}
	calls, totals := []Usage{}, []Total{}
	for i := len(a.usage) - 1; i >= 0; i-- {
		u := a.usage[i]
		if !admin && u.Member != c.ID {
			continue
		}
		shared := len(u.Applications) > 1
		u.Applications = slices.DeleteFunc(slices.Clone(u.Applications), func(binding platform.AssetBinding) bool { return !slices.Contains(visibleApps, binding.Ref) })
		for _, binding := range u.Applications {
			j := slices.IndexFunc(byApplication, func(total platform.AIApplicationTotal) bool {
				return total.Application == binding && (total.Resource == nil && u.Resource == nil || total.Resource != nil && u.Resource != nil && *total.Resource == *u.Resource)
			})
			if j < 0 {
				byApplication = append(byApplication, platform.AIApplicationTotal{Application: binding, Resource: u.Resource})
				j = len(byApplication) - 1
			}
			total := &byApplication[j]
			total.Calls++
			total.Input += u.Input
			total.Output += u.Output
			total.Cost += u.Cost
			if shared {
				total.SharedCalls++
			}
			if !u.CostReported {
				total.CostUnknown++
			}
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
		switch {
		case u.TokensReported:
			t.Reported += u.Input + u.Output
		case u.TokensEstimated:
			t.Estimated += u.Input + u.Output
		}
		if !u.CostReported {
			t.CostUnknown++
		}
		if u.Outcome != "ok" {
			t.Failed++
		}
	}
	return AIUsage{Calls: calls, Totals: totals, Applications: byApplication}, nil
}

// AIUsage is model calls metered from the journal, and their totals per day, member and model.
type AIUsage = platform.AIUsageReport

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

func (a *AI) NewSimulationApp(tenant string) platform.App { return New(tenant) }
