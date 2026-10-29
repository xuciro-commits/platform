package ai

import (
	"embed"
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/aistate"
	"platformserver/platform"
)

//go:embed i18n
var languageFiles embed.FS

var languages = platform.LoadLanguages(languageFiles, "i18n")

const (
	ID                   = platform.AIApp
	ProviderType         = platform.AIProviderType
	ModelType            = platform.AIModelType
	SchemaProviderAdd    = platform.AISchemaProviderAdd
	SchemaProviderEdit   = platform.AISchemaProviderEdit
	SchemaProviderRemove = platform.AISchemaProviderRemove
	SchemaModelEnable    = platform.AISchemaModelEnable
	SchemaModelDisable   = platform.AISchemaModelDisable
	LimitType            = platform.AILimitType
	SchemaLimitSet       = platform.AISchemaLimitSet
	SchemaLimitRemove    = platform.AISchemaLimitRemove
	SettingDailyTokens   = platform.AISettingDailyTokens
	SettingPerMinute     = platform.AISettingPerMinute
	SettingAppModel      = platform.AISettingAppModel
	SettingMaxTokens     = platform.AISettingMaxTokens
	Admin                = platform.AIAdmin
	User                 = platform.AIUser
)

type Vendor = platform.AIVendor

var Vendors = platform.AIVendors

type Provider = platform.AIProvider

type Model = platform.AIModel

type Limit = platform.AILimit

type Usage = platform.AIUsage

type AI struct {
	trainingHost       platform.AITrainingHost
	trainingPolicies   map[string]platform.AITrainingPolicy
	trainingCandidates map[string]platform.AITrainingCandidate
	mu                 sync.Mutex
	providers          []Provider
	models             []Model
	limits             map[string]Limit
	usage              []Usage
	daily              map[string]int
	recent             map[string][]time.Time
	reservations       map[string]platform.AIReservation
	ledger             *platform.Ledger
}

func New(tenant string) *AI {
	admin := []string{Admin}
	f := func(name, typ, description string, required bool) platform.Field {
		return platform.Field{Name: name, Type: typ, Required: required, Description: description}
	}
	return &AI{trainingPolicies: map[string]platform.AITrainingPolicy{}, trainingCandidates: map[string]platform.AITrainingCandidate{}, limits: map[string]Limit{}, daily: map[string]int{}, recent: map[string][]time.Time{}, reservations: map[string]platform.AIReservation{}, ledger: platform.NewLedger(tenant, ID, platform.NewCatalog(append(trainingActions(), []platform.Action{
		platform.Action{Schema: SchemaProviderAdd, Target: ProviderType, Capability: "providers", Title: "Add AI provider", Roles: admin,
			Description: "Add a source of models: a vendor (Anthropic, OpenAI, Gemini, Moonshot, DeepSeek, Qwen, Zhipu, OpenRouter), a third-party OpenAI-compatible API, or a local model server (LM Studio, Ollama, llama.cpp).",
			Payload: []platform.Field{f("kind", "string", "vendor, compatible or local", true), f("vendor", "string", "For a vendor: its ID", false),
				f("baseUrl", "string", "For compatible and local: the API's base URL, e.g. http://host.docker.internal:1234/v1", false),
				f("secret", "string", "Name of the API key in the secret store (optional for local)", false)}},
		platform.Action{Schema: SchemaProviderEdit, Target: ProviderType, Capability: "providers", Title: "Edit AI provider", Roles: admin,
			Description: "Update provider configuration while retaining its identity, enabled models and usage history.",
			Payload: []platform.Field{f("kind", "string", "vendor, compatible or local", true), f("vendor", "string", "For a vendor: its ID", false),
				f("baseUrl", "string", "For compatible and local: the API's base URL, e.g. http://host.docker.internal:1234/v1", false),
				f("secret", "string", "Name of the API key in the secret store (optional for local)", false)}},
		platform.Action{Schema: SchemaProviderRemove, Target: ProviderType, Capability: "providers", Title: "Remove AI provider", Roles: admin,
			Description: "Remove a provider; its models are disabled. Recorded usage stays.", Payload: []platform.Field{}},
		platform.Action{Schema: SchemaModelEnable, Target: ModelType, Capability: "models", Title: "Enable model", Roles: admin,
			Description: "Let members call a provider's model (target <provider>/<model>): everyone in the tenant, or members holding a role in the ai app.",
			Payload:     []platform.Field{f("access", "string", "everyone or users", true), f("dailyTokens", "integer", "Tokens the whole tenant may spend on it a day; 0: no cap", false), f("capabilities", "object", "Reviewed provider capabilities and provenance", false), f("defaults", "object", "Default input, generation and thinking options", false), f("expectedRevision", "integer", "Configuration revision being edited", false)}},
		platform.Action{Schema: SchemaModelDisable, Target: ModelType, Capability: "models", Title: "Disable model", Roles: admin,
			Description: "Stop calls to a model.", Payload: []platform.Field{}},
		platform.Action{Schema: SchemaLimitSet, Target: LimitType, New: true, Capability: "limits", Title: "Set AI limit", Roles: admin,
			Description: "Limit what one member, agent or app (target its member ID) may call: tokens a day and calls a minute, in place of the defaults in the AI settings.",
			Payload:     []platform.Field{f("dailyTokens", "integer", "Tokens a day; 0: the default", false), f("perMinute", "integer", "Calls a minute; 0: the default", false)}},
		platform.Action{Schema: SchemaLimitRemove, Target: LimitType, Capability: "limits", Title: "Remove AI limit", Roles: admin,
			Description: "Return a member, agent or app to the default limits.", Payload: []platform.Field{}},
	}...)...), ProviderType, ModelType, LimitType, trainingPolicyType, trainingCandidateType)}
}

func (a *AI) SnapshotFormat() string { return "ai.state.v1" }

func (a *AI) Snapshot() (json.RawMessage, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ledger.SnapshotWith(aistate.State{Providers: a.providers, Models: a.models, Limits: a.limits, Usage: a.usage, Daily: a.daily, Reservations: a.reservations, TrainingPolicies: a.trainingPolicies, TrainingCandidates: a.trainingCandidates})
}

func (a *AI) Restore(raw json.RawMessage) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	var s aistate.State
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
	for _, u := range a.usage {
		a.recent[u.Member] = append(a.recent[u.Member], u.At)
	}
	a.trainingPolicies, a.trainingCandidates = s.TrainingPolicies, s.TrainingCandidates
	if a.trainingPolicies == nil {
		a.trainingPolicies = map[string]platform.AITrainingPolicy{}
	}
	if a.trainingCandidates == nil {
		a.trainingCandidates = map[string]platform.AITrainingCandidate{}
	}
	a.reservations = s.Reservations
	if a.reservations == nil {
		a.reservations = map[string]platform.AIReservation{}
	}
	return nil
}

func (a *AI) Manifest() platform.Manifest {
	return platform.Manifest{Languages: languages, ID: ID, Title: "AI", Version: "1", Actions: a.ledger.Catalog, Reads: []string{"ai-providers", "ai-models", "ai-usage", "ai-limits", "ai-training"},
		ReadSources: map[string]string{"ai-providers": "record", "ai-models": "record", "ai-usage": "record", "ai-limits": "record", "ai-training": "record"},
		Everyone:    []string{"ai-models", "ai-usage"}, Roles: []string{User},
		Settings: []platform.Setting{
			{Name: SettingDailyTokens, Title: "Tokens per person per day", Type: "integer", Default: "0",
				Description: "What each person may spend on models a day, unless an AI limit says otherwise; 0: no limit. Agents have their own, in the Agents settings."},
			{Name: SettingAppModel, Title: "Model apps ask", Type: "text", Default: "",
				Description: "The enabled model (<provider>/<model>) an app's request asks when it names none, such as a ticket's summary; empty: such requests are refused."},
			{Name: SettingAppModel + "-options", Title: "Application model options", Type: "text", Default: "", Description: "Input, generation and thinking overrides configured in AI configuration."},
			{Name: SettingPerMinute, Title: "Calls per minute", Type: "integer", Default: "60",
				Description: "How many model calls each person or agent may make a minute, unless an AI limit says otherwise; 0: no limit."},
			{Name: SettingMaxTokens, Title: "Most tokens one answer", Type: "integer", Default: "8192",
				Description: "Organization-wide generation budget per call. New model options must fit within it; 0 requires a reviewed model limit."}}}
}

func (a *AI) Declarations() []*pb.AuthorityDeclaration { return a.ledger.Declarations() }

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
		if s.GetSchema().GetName() == trainingPolicySet || s.GetSchema().GetName() == trainingReview || s.GetSchema().GetName() == trainingRevoke {
			return a.trainingDecision(c, s, now)
		}
		var p struct {
			Kind, Vendor, BaseURL, Secret, Access string
			DailyTokens, PerMinute                int
			Capabilities                          *platform.ModelCapabilities
			Defaults                              *platform.ModelOptions
			ExpectedRevision                      *int
		}
		if json.Unmarshal(s.GetPayload(), &p) != nil {
			return nil, invalid
		}
		switch s.GetSchema().GetName() {
		case SchemaProviderAdd, SchemaProviderEdit:
			_, known := a.provider(id)
			if s.GetSchema().GetName() == SchemaProviderAdd && known {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
			}
			if s.GetSchema().GetName() == SchemaProviderEdit && !known {
				return nil, notFound
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
				if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Scheme != "https" && !(u.Scheme == "http" && p.Kind == "local") || p.Kind == "compatible" && p.Secret == "" {
					return nil, invalid
				}
				pv.BaseURL = strings.TrimRight(p.BaseURL, "/")
			default:
				return nil, invalid
			}
			return func(*pb.ChangeRecord) {
				index := slices.IndexFunc(a.providers, func(value Provider) bool { return value.ID == id })
				if index >= 0 {
					a.providers[index] = pv
				} else {
					a.providers = append(a.providers, pv)
				}
			}, nil
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
			next := Model{Provider: provider, Model: model}
			if i := slices.IndexFunc(a.models, func(m Model) bool { return m.Name() == id }); i >= 0 {
				next = a.models[i]
			}
			if p.ExpectedRevision != nil && *p.ExpectedRevision != next.Revision {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Model configuration changed; reload before saving")
			}
			next.Access, next.DailyTokens = p.Access, p.DailyTokens
			if next.DailyTokens < 0 {
				return nil, invalid
			}
			if p.Capabilities != nil {
				next.Capabilities = p.Capabilities
			}
			if p.Defaults != nil {
				next.Defaults = *p.Defaults
			}
			ceiling := 0
			if !c.Replaying {
				ceiling, _ = strconv.Atoi(c.Setting(SettingMaxTokens))
			}
			if _, err := next.Resolve("", platform.ModelOptions{}, ceiling); err != nil {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
			if c := next.Capabilities; c != nil {
				pv, _ := a.provider(provider)
				if c.Adapter == "deepseek" && pv.Wire != "openai" || c.Adapter == "anthropic-adaptive" && pv.Wire != "anthropic" {
					return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Model options do not match the provider protocol")
				}
			}
			return func(record *pb.ChangeRecord) {
				next.Revision = int(record.GetRevision())
				a.models = slices.DeleteFunc(a.models, func(m Model) bool { return m.Name() == id })
				a.models = append(a.models, next)
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

func (a *AI) Meter(u Usage) {
	a.mu.Lock()
	defer a.mu.Unlock()
	state := aistate.State{Usage: a.usage, Daily: a.daily, Reservations: a.reservations, TrainingPolicies: a.trainingPolicies, TrainingCandidates: a.trainingCandidates}
	state.Meter(u)
	a.usage, a.daily, a.reservations = state.Usage, state.Daily, state.Reservations
	a.recent[u.Member] = append(slices.DeleteFunc(a.recent[u.Member], func(t time.Time) bool { return u.At.Sub(t) >= time.Minute }), u.At)

}

func (a *AI) Usage() []Usage {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.usage)
}

type Defaults = platform.AIDefaults

func (a *AI) allowLocked(m platform.Member, model Model, d Defaults, r platform.AIReservation, now time.Time) string {
	day := now.UTC().Format(time.DateOnly)
	memberReserved, modelReserved := 0, 0
	for _, pending := range a.reservations {
		remaining := max(0, pending.Input+pending.Output-pending.Accounted)
		if pending.Model == model.Name() {
			if pending.State == "exceeded" {
				return "A provider exceeded the reserved token bound; reconcile its usage before calling this model again"
			}
			modelReserved += remaining
		}
		if pending.Member == m.ID {
			memberReserved += remaining
		}
	}
	tokens := r.Input + r.Output
	if model.DailyTokens > 0 && tokens > model.DailyTokens-a.daily[day+"|model:"+model.Name()]-modelReserved {
		return fmt.Sprintf("The tenant used the %d tokens a day of %s", model.DailyTokens, model.Name())
	}
	daily, perMinute := a.effectiveLimitsLocked(m, d)
	if daily > 0 && tokens > daily-a.daily[day+"|"+m.ID]-memberReserved {
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
	quota := platform.AIQuota{DailyTokens: daily, PerMinute: perMinute, UsedTokens: a.daily[now.UTC().Format(time.DateOnly)+"|"+m.ID], UsedCalls: a.callsLocked(m.ID, now)}
	for _, r := range a.reservations {
		if r.Member == m.ID {
			quota.ReservedTokens += max(0, r.Input+r.Output-r.Accounted)
			quota.UnsettledCalls++
		}
	}
	return quota
}

func (a *AI) callsLocked(member string, now time.Time) int {
	n := 0
	for _, at := range a.recent[member] {
		if now.Sub(at) < time.Minute {
			n++
		}
	}
	for _, r := range a.reservations {
		if r.Member == member && r.State == "pending" && now.Sub(r.At) < time.Minute {
			n++
		}
	}
	return n
}

func (a *AI) Reservation(call string) (platform.AIReservation, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	r, ok := a.reservations[call]
	if !ok {
		for _, u := range a.usage {
			if u.Call != "" && u.Call == call {
				return platform.AIReservation{Call: call, Member: u.Member, Model: u.Model, State: "settled"}, true
			}
		}
	}
	return r, ok
}

type Total = platform.AITotal

func (a *AI) Read(c platform.Caller, name string) (any, *kernel.Error) {
	visibleApps := c.VisibleApplications()
	a.mu.Lock()
	defer a.mu.Unlock()
	admin := c.Holds(c.App, Admin)
	switch name {
	case "ai-training":
		if !admin {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "AI administration access is required")
		}
		report := platform.AITrainingReport{Policies: slices.Collect(maps.Values(a.trainingPolicies)), Candidates: []platform.AITrainingCandidate{}}
		for _, candidate := range a.trainingCandidates {
			if why := a.trainingProblem(c.Member, candidate, time.Now().UTC(), true); why != "" {
				candidate.Problems = []string{why}
			}
			if a.trainingHost != nil && a.trainingHost.TrainingReadable(c.Member, candidate, time.Now().UTC()) != "" {
				candidate.Sources, candidate.SourceFields, candidate.ExpectedSources = nil, nil, nil
				candidate.Target, candidate.Run, candidate.Intent = "", "", ""
			}
			candidate.Chosen, candidate.Rejected = nil, nil
			report.Candidates = append(report.Candidates, candidate)
		}
		slices.SortFunc(report.Candidates, func(a, b platform.AITrainingCandidate) int { return strings.Compare(a.ID, b.ID) })
		return report, nil
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
	reservations := []platform.AIReservation{}
	for _, r := range a.reservations {
		if admin || r.Member == c.ID {
			reservations = append(reservations, r)
		}
	}
	slices.SortFunc(reservations, func(a, b platform.AIReservation) int { return strings.Compare(a.Call, b.Call) })
	return AIUsage{Calls: calls, Totals: totals, Applications: byApplication, Reservations: reservations}, nil
}

type AIUsage = platform.AIUsageReport

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

func (a *AI) Spent(member string, now time.Time) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.daily[now.UTC().Format(time.DateOnly)+"|"+member]
}

func (a *AI) NewSimulationApp(tenant string) platform.App { return New(tenant) }
