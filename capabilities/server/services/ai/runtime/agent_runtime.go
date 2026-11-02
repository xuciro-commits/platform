package agents

import (
	"cmp"
	"embed"
	"encoding/json"
	"fmt"
	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/services/ai/evaluation"
	"slices"
	"strings"
	"sync"
	"time"
)

//go:embed i18n
var languageFiles embed.FS

var languages = platform.LoadLanguages(languageFiles, "i18n")

const (
	outcomeLimit  = 4000
	modelFailures = 3
)

type Agents struct {
	host       platform.AgentHost
	mu         sync.Mutex
	tenant     string
	ledger     *platform.Ledger
	defs       map[string]*platform.CompiledAgent
	buildStamp string
	busy       map[string]bool
}

func (a *Agents) AttachAgent(h platform.AgentHost) { a.host = h }

func New(tenant string) *Agents {
	var actions []platform.Action
	for _, e := range agentEntities() {
		actions = append(actions, platform.EntityActions(e)...)
	}
	actions = append(actions,
		platform.Action{Schema: platform.SchemaRunStart, Target: platform.RunType, Capability: "runs", Title: "Ask an agent", Roles: []string{platform.AnyMember},
			Description: "Give one of your apps' agents a goal; it works on your behalf, within what you may do yourself.",
			Payload: []platform.Field{{Name: "agent", Type: "string", Required: true, Description: "The agent, <app>.<name>"},
				{Name: "goal", Type: "string", Required: true, Description: "What it should achieve"}, {Name: "ref", Type: "string", Description: "The record it is about, <type>/<id>"},
				{Name: "act", Type: "boolean", Description: "Retained historical input; new runs always require confirmation for writes"},
				{Name: "language", Type: "string", Description: "The language to answer in, such as zh-CN; empty: English"}}},
		platform.Action{Schema: platform.SchemaRunStep, Target: platform.RunType, Capability: "runs", Title: "Take step", Payload: []platform.Field{}, Roles: []string{platform.AgentAdmin},
			Description: "Made by the host: a step the model chose."},
		platform.Action{Schema: platform.SchemaRunCancel, Target: platform.RunType, Capability: "runs", Title: "Stop run", Payload: []platform.Field{}, Roles: []string{platform.AgentAdmin, platform.AnyMember},
			Description: "Stop a run; what it did stays done. Anyone may stop a run on their behalf."},
		platform.Action{Schema: platform.SchemaRunConfirm, Target: platform.RunType, Capability: "runs", Title: "Confirm draft", Roles: []string{platform.AnyMember},
			Description: "Do what the agent running for you drafted, as yourself; change its fields first if you want.",
			Payload:     []platform.Field{{Name: "payload", Type: "json", Description: "The action's fields, changed; empty: as drafted"}}},
		platform.Action{Schema: platform.SchemaRunReject, Target: platform.RunType, Capability: "runs", Title: "Reject draft", Roles: []string{platform.AnyMember},
			Description: "Refuse what the agent running for you drafted; it goes on with your reason.",
			Payload:     []platform.Field{{Name: "reason", Type: "string", Description: "Why, for the agent"}}},
		platform.Action{Schema: platform.SchemaEvalStart, Target: platform.EvaluationType, Capability: "evaluations", Title: "Evaluate a model", Roles: []string{platform.AgentAdmin},
			Description: "Re-run an agent's past runs that people confirmed or corrected, dry, with a candidate model, and compare.",
			Payload: []platform.Field{{Name: "agent", Type: "string", Required: true, Description: "The agent, <app>.<name>"},
				{Name: "model", Type: "string", Required: true, Description: "The candidate, an enabled model <provider>/<model>"},
				{Name: "suite", Type: "boolean", Description: "Run the agent's declared cases, three times each, instead of its past runs"},
				{Name: "candidate", Type: "string", Description: "Saved release candidate containing this agent; requires a builder and declared cases"}}})
	actions = append(actions, platform.Action{Schema: platform.SchemaToolPropose, Target: platform.RunType, Capability: "runs", Title: "Propose a tool action", Description: "Save a tool's write as a proposal for the person to confirm; nothing changes until they do.", Roles: []string{platform.AnyMember}, Payload: []platform.Field{
		{Name: "owner", Type: "string", Required: true}, {Name: "action", Type: "string", Required: true}, {Name: "target", Type: "string", Required: true}, {Name: "payload", Type: "json", Required: true}, {Name: "rationale", Type: "string"}}})
	actions = append(actions, switchActions()...)
	return &Agents{tenant: tenant, ledger: platform.NewLedger(tenant, platform.AgentApp, platform.NewCatalog(actions...), platform.RunType, platform.EvaluationType, platform.MemoryType, platform.SwitchType), defs: map[string]*platform.CompiledAgent{}, busy: map[string]bool{}}
}

func (a *Agents) NewSimulationApp(tenant string) platform.App { return New(tenant) }

func agentEntities() []platform.Entity {
	return []platform.Entity{{Type: platform.RunType, Title: "Agent run", Model: platform.AgentRunRecord{}, Display: "title",
		Derived: []platform.Derivation{
			{From: "ref", Fields: []string{"seen", "citations"}},
			{From: "steps.sources", Fields: []string{"arguments", "outcome", "calls", "content"}},
			{From: "citations.document", Element: true},
			{From: "draft.type/target", Element: true},
			{From: "*", Fields: []string{"result", "pending"}}},
		Withheld: "withheld", Scope: platform.Scope{Participants: func(record any) []string { return []string{record.(platform.AgentRunRecord).OnBehalf} }}},
		{Type: platform.EvaluationType, Title: "Agent evaluation", Model: platform.Evaluation{}, Display: "model"}, memoryEntity(),
		{Type: platform.SwitchType, Title: "Agent switch", Model: platform.Switch{}, Description: "An agent's off switch: suspended by an administrator, or working."}}
}

func (a *Agents) Manifest() platform.Manifest {
	return platform.Manifest{Languages: languages, ID: platform.AgentApp, Title: "Agents", Version: "1", Actions: a.ledger.Catalog, Entities: agentEntities(),
		Reads: []string{"agents", "runs", "memories", "agent-overview"}, Everyone: []string{"agents", "runs", "memories"},
		ReadSources: map[string]string{"agents": "record", "agent-overview": "record"},
		Settings: []platform.Setting{
			{Name: platform.SettingAgentModel, Title: "Model for agents", Type: "text", Default: "",
				Description: "The enabled model agents call, <provider>/<model>; it must call tools. Empty: agents stop and hand their goal to a person."},
			{Name: platform.SettingAgentModel + "-options", Title: "Agent model options", Type: "text", Default: "", Description: "Input, generation and thinking overrides configured in AI configuration."},
			{Name: platform.SettingAgentDaily, Title: "Tokens per agent per day", Type: "integer", Default: "200000",
				Description: "An agent that has used this many tokens today stops its runs until tomorrow."},
			{Name: platform.SettingPublished, Title: "Published over A2A", Type: "text", Default: "",
				Description: "Agents other systems may call over A2A 1.0, comma-separated <app>.<name>; their cards are at /a2a/<tenant>/<agent>/.well-known/agent-card.json."},
			{Name: platform.SettingTranscriptDays, Title: "Days transcripts are kept", Type: "integer", Default: "30",
				Description: "Every model call's full request and answer are kept this many days outside the journal, for agent administrators; they may hold personal data. 0: none are kept, and what was kept is forgotten."},
		}}
}

func (a *Agents) Declarations() []*pb.AuthorityDeclaration { return a.ledger.Declarations() }

func (a *Agents) AcceptedLedger() *platform.Ledger { return a.ledger }

func (a *Agents) SnapshotFormat() string { return "platform.ledger.v1" }

func (a *Agents) Snapshot() (json.RawMessage, error) { return a.ledger.Snapshot() }

func (a *Agents) Restore(raw json.RawMessage) error { return a.ledger.Restore(raw) }

var jsonTypes = map[string]string{"string": "string", "integer": "integer", "number": "number", "boolean": "boolean", "date": "string", "datetime": "string"}

func agentToolName(s string) string {
	return strings.NewReplacer(".", "_", "#", "__", "/", "_", ":", "_", "-", "_").Replace(s)
}

func rationale() map[string]any {
	return map[string]any{"type": "string", "description": "Why you take this step, in one sentence"}
}

func (a *Agents) def(id string) *platform.CompiledAgent {
	a.refresh()
	return a.defs[id]
}

func (a *Agents) IdleAgentTurn(run string) { delete(a.busy, run) }

func (a *Agents) each(fn func(id string, d *platform.CompiledAgent)) {
	for id, d := range a.defs {
		fn(id, d)
	}
}

func (a *Agents) refresh() {
	var stamps []string
	for _, app := range a.host.Apps() {
		if owner, ok := app.(platform.DynamicAgentDefinitions); ok {
			stamps = append(stamps, app.Manifest().ID+":"+owner.AgentStamp())
		}
	}
	slices.Sort(stamps)
	stamp := strings.Join(stamps, "\n")
	if stamp == a.buildStamp {
		return
	}
	for id, d := range a.defs {
		if _, ok := a.host.App(d.App).(platform.DynamicAgentDefinitions); ok {
			delete(a.defs, id)
		}
	}
	a.buildStamp = stamp
	for _, app := range a.host.Apps() {
		if _, ok := app.(platform.DynamicAgentDefinitions); ok {
			_ = a.DeclareAgentApp(app)
		}
	}
}

func (a *Agents) automation(app string, replaying bool) platform.Caller {
	c := a.host.Automation(platform.Caller{}, app)
	c.Replaying = replaying
	return c
}

func (a *Agents) authorityOf(typ string) string { app, _ := a.host.OwnerOf(typ); return app }

func (a *Agents) AgentPrincipal(agent string) platform.Member {
	return platform.Member{ID: "agent:" + agent, Tenant: a.tenant, Roles: map[string]string{}, Agent: true}
}

func (a *Agents) run(r platform.AgentRunRecord) platform.AgentRun {
	return platform.AgentRun{ID: r.ID, Agent: r.Agent, Goal: r.Goal, Ref: r.Ref, OnBehalf: r.OnBehalf}
}

func (a *Agents) runs(c platform.Caller, state ...string) []platform.AgentRunRecord {
	var terms []any
	for i, s := range state {
		if i < len(state)-1 {
			terms = append(terms, "|")
		}
		terms = append(terms, []any{"state", "=", s})
	}
	domain, _ := json.Marshal(terms)
	out, _, _ := platform.Find[platform.AgentRunRecord](c, platform.Query{Domain: domain, Sort: []string{"id"}})
	return out
}

func (a *Agents) Read(c platform.Caller, name string) (any, *kernel.Error) {
	if name == "memories" {
		about, _ := json.Marshal([]any{[]any{"for", "=", c.ID}, []any{"state", "!=", "forgotten"}})
		out, _, _ := platform.Find[platform.Memory](a.host.Automation(c, platform.AgentApp), platform.Query{Domain: about, Sort: []string{"-created"}, Limit: 100})
		return out, nil
	}
	if name == "agent-overview" {
		if c.Roles[platform.AgentApp] != platform.AgentAdmin {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
		}
		return a.AgentOverview(), nil
	}
	if name == "runs" {
		mine, _ := json.Marshal([]any{[]any{"onBehalf", "=", c.ID}})
		out, _, _ := platform.Find[platform.AgentRunRecord](a.host.Automation(c, platform.AgentApp), platform.Query{Domain: mine, Sort: []string{"-created"}, Limit: 50})
		out = slices.DeleteFunc(out, func(run platform.AgentRunRecord) bool {
			d := a.def(run.Agent)
			return !run.ToolOnly && (d == nil || c.Roles[d.App] == "")
		})
		return out, nil
	}
	out := []platform.AgentInfo{}
	a.refresh()
	for _, id := range slices.Sorted(maps.Keys(a.defs)) {
		d := a.defs[id]
		if c.Roles[platform.AgentApp] != platform.AgentAdmin && c.Roles[d.App] == "" {
			continue
		}
		out = append(out, platform.AgentInfo{ID: id, App: d.App, Title: d.Title, Instructions: visibleAgentInstructions(c, d), Tools: append(slices.Clone(d.Agent.Tools), "context", "search", "knowledge", "remember", "ask", "finish"), Budget: d.Budget})
	}
	return out, nil
}

func (a *Agents) definitionVersion(d *platform.CompiledAgent) string {
	version := ""
	if owner := a.host.App(d.App); owner != nil {
		version = owner.Manifest().Version
	}
	parts := []any{d.App, version, d.Name, d.Title, d.Instructions, d.Agent.Tools, d.Budget}
	if d.Model != "" || d.ModelOptions != (platform.ModelOptions{}) {
		parts = append(parts, d.Model, d.ModelOptions)
	}
	if b, ok := a.host.App(d.App).(platform.DynamicAgentDefinitions); ok {
		parts = append(parts, b.AgentVersion(d.Name))
	}
	if len(d.Checkpoints) > 0 {
		parts = append(parts, d.Checkpoints)
	}
	for _, name := range d.Agent.Tools {
		if query, ok := d.Toolset[agentToolName(name)]; ok && query.Kind == "query" {
			parts = append(parts, query.Query)
		}
	}
	digest, _ := platform.CanonicalDigest(parts)
	return "agent.sha256." + digest
}

func (a *Agents) changedDefinition(run platform.AgentRunRecord, d *platform.CompiledAgent) bool {
	if d == nil {
		return false
	}
	_, dynamic := a.host.App(d.App).(platform.DynamicAgentDefinitions)
	return dynamic && run.DefinitionVersion != "" && run.DefinitionVersion != a.definitionVersion(d)
}

func (a *Agents) ForkDecision() (platform.App, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return &Agents{tenant: a.tenant, host: a.host, ledger: a.ledger, defs: maps.Clone(a.defs),
		buildStamp: a.buildStamp, busy: maps.Clone(a.busy)}, nil
}

var _ platform.DecisionForkApp = (*Agents)(nil)

func (a *Agents) AllowsAgentRun(run platform.AgentRunRecord) bool {
	d := a.def(run.Agent)
	if d == nil {
		return false
	}
	if run.OnBehalf == "" {
		return true
	}
	reader := a.reader(run)
	return reader != nil && reader.Tenant == a.tenant && reader.Roles[d.App] != ""
}

func (a *Agents) AgentDeclaration(id string) (platform.AgentCardDeclaration, bool) {
	d := a.def(id)
	if d == nil {
		return platform.AgentCardDeclaration{}, false
	}
	return platform.AgentCardDeclaration{App: d.App, Title: d.Title, Description: d.Description}, true
}

func (a *Agents) Start(c platform.Caller, r *pb.ChangeRecord, s platform.RunStart, now time.Time) {
	run, err := a.create(s.ID, s.Agent, s.Goal, s.Ref, "", s.Flow, s.Step, s.Token, now, !c.Replaying && c.Staging())
	if err != nil {
		c.FailDecision(err)
		return
	}
	a.host.Automation(c, platform.AgentApp).Put(r, run)
}

func (a *Agents) Signal(c platform.Caller, r *pb.ChangeRecord, run string, s platform.RunSignal, now time.Time) {
	a.RecordAgentSignal(c, r, run, platform.Signal{At: s.At, Kind: s.Kind, By: s.By, Detail: s.Detail}, now)
}

func (a *Agents) Finished(c platform.Caller, flow, step string) []string {
	where := []any{[]any{"flow", "=", flow}, []any{"state", "=", "done"}}
	if step != "" {
		where = append(where, []any{"step", "=", step})
	}
	domain, _ := json.Marshal(where)
	runs, _, _ := platform.Find[platform.AgentRunRecord](a.host.Automation(c, platform.AgentApp), platform.Query{Domain: domain, Sort: []string{"created", "id"}})
	var out []string
	for _, run := range runs {
		out = append(out, run.ID)
	}
	return out
}

func (a *Agents) Interested(_ []string, e platform.Event) bool { return a.interested(e) }

func (a *Agents) Listen(c platform.Caller, e platform.Event, _ []string, now time.Time) *kernel.Error {
	return a.handle(c, e, now)
}

func (a *Agents) interested(e platform.Event) bool {
	s := e.Record.GetSubmission()
	return e.App == platform.WorkApp && s.GetSchema().GetName() == "work.task.complete" && strings.HasPrefix(s.GetTarget().GetId(), platform.AgentApp+":")
}

func (a *Agents) handle(c platform.Caller, e platform.Event, now time.Time) *kernel.Error {
	id := e.Record.GetSubmission().GetTarget().GetId()
	for _, run := range a.runs(c, "waiting") {
		if run.Task != id {
			continue
		}
		task, _ := platform.Get[platform.WorkTask](a.host.Automation(c, platform.WorkApp), id)
		s := &pb.Submission{TenantId: a.tenant, PrincipalId: c.ID, Authority: platform.AgentApp, IdempotencyKey: fmt.Sprintf("%s:%d", run.ID, run.Revision+1),
			Target: &pb.EntityRef{Type: platform.RunType, Id: run.ID}, Schema: &pb.SchemaRef{Name: platform.SchemaRunStep, Version: 1}, Payload: []byte("{}")}
		_, err := a.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
			last := &run.Steps[len(run.Steps)-1]
			last.Outcome += fmt.Sprintf("\nanswered by %s: %s", task.Assignee, cmp.Or(task.Answer, "done"))
			run.State, run.Task = "running", ""
			return func(r *pb.ChangeRecord) { c.Put(r, run) }, nil
		})
		return err
	}
	return nil
}

func (a *Agents) AgentEffectEnded(c platform.Caller, r *pb.ChangeRecord, x platform.Effect, o platform.Outcome, now time.Time) {
	task := "effect:" + x.Event + ":" + x.Key
	for _, run := range a.runs(a.host.Automation(c, platform.AgentApp), "waiting") {
		if run.Task != task {
			continue
		}
		last := &run.Steps[len(run.Steps)-1]
		switch o.Result {
		case "delivered":
			last.Outcome += "\nanswered: " + agents.Clip(a.host.AgentAnswerText(o), outcomeLimit)
		case "discarded":
			last.Outcome += "\n" + o.Detail
		default:
			last.Outcome += "\nthe receiver " + x.State + " it: " + o.Detail
		}
		run.State, run.Task = "running", ""
		if r != nil {
			a.host.Automation(c, platform.AgentApp).Put(r, run)
			return
		}
		ac := a.host.Automation(c, platform.AgentApp)
		s := &pb.Submission{TenantId: a.tenant, PrincipalId: ac.ID, Authority: platform.AgentApp, IdempotencyKey: fmt.Sprintf("%s:%d", run.ID, run.Revision+1),
			Target: &pb.EntityRef{Type: platform.RunType, Id: run.ID}, Schema: &pb.SchemaRef{Name: platform.SchemaRunStep, Version: 1}, Payload: []byte("{}")}
		a.ledger.Receive(ac, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
			return func(r *pb.ChangeRecord) { ac.Put(r, run) }, nil
		})
		return
	}
}
