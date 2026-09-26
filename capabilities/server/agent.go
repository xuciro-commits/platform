package platformserver

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// The agent app runs the agents apps declare (ADR-0021). A run is a record of
// it. The host calls the model outside the tenant's lock (Think), then journals
// the step the model chose — tool, arguments, rationale, usage — as an entry of
// kind agent and applies it as one decision of this app: the tool's action is
// submitted as the agent's principal, a read or the context graph is read, a
// question becomes a task, finish ends the run. Replay applies the same entries
// and never calls a model.
const (
	AgentApp          = "agent"
	RunType           = "agent.run"
	AgentAdmin        = "admin"
	SchemaRunStart    = "agent.run.start"
	SchemaRunStep     = "agent.run.step"
	SchemaRunCancel   = "agent.run.cancel"
	SchemaRunConfirm  = "agent.run.confirm"
	SchemaRunReject   = "agent.run.reject"
	SettingAgentModel = "model"
	SettingAgentDaily = "daily-tokens"
	outcomeLimit      = 4000
	modelFailures     = 3
)

// AgentRunRecord is one run of an agent.
type AgentRunRecord struct {
	platform.Record
	Agent       string     `json:"agent" field:"readonly,search"` // "<app>.<name>"
	Title       string     `json:"title" field:"readonly,search"`
	Goal        string     `json:"goal" field:"readonly" type:"longtext"`
	Ref         string     `json:"ref,omitempty" field:"readonly"`
	Seen        string     `json:"seen,omitempty" field:"readonly" type:"longtext" title:"What it saw of the record"` // at the start: the prompt's context, the evaluation's too
	OnBehalf    string     `json:"onBehalf,omitempty" field:"readonly" title:"On behalf of"`
	Acts        bool       `json:"acts,omitempty" field:"readonly" title:"Acts without drafts"` // for its person, within their grants: an A2A caller
	Language    string     `json:"language,omitempty" field:"readonly"`                         // the person's, which the agent answers in (ADR-0023 D6)
	Flow        string     `json:"flow,omitempty" field:"readonly"`                             // the flow instance whose step started it
	Token       int        `json:"token,omitempty" field:"readonly"`
	Step        string     `json:"step,omitempty" field:"readonly"` // that step's name
	State       string     `json:"state" field:"readonly" choices:"running,waiting,done,stopped"`
	Model       string     `json:"model,omitempty" field:"readonly"`
	Steps       []RunStep  `json:"steps" field:"readonly"`
	StepsUsed   int        `json:"stepsUsed" field:"readonly" title:"Model turns"`
	TokensUsed  int        `json:"tokensUsed" field:"readonly" title:"Tokens"`
	ActionsUsed int        `json:"actionsUsed" field:"readonly" title:"Actions"`
	Cost        float64    `json:"cost,omitempty" field:"readonly"`
	Result      string     `json:"result,omitempty" field:"readonly" type:"longtext"`
	Task        string     `json:"task,omitempty" field:"readonly"`
	Stopped     string     `json:"stopped,omitempty" field:"readonly" title:"Why it stopped"`
	Draft       []Draft    `json:"draft,omitempty" field:"readonly" title:"Draft to confirm"` // at most one
	Citations   []Citation `json:"citations,omitempty" field:"readonly" title:"Sources it read"`
	Signals     []Signal   `json:"signals,omitempty" field:"readonly" title:"What people made of it"`
}

// Draft is an action an agent running for a person proposes; the person
// confirms it, changed or not, or rejects it (ADR-0021 D6).
type Draft struct {
	Kind      string `json:"kind"` // action or protocol
	Action    string `json:"action"`
	Target    string `json:"target"`
	Type      string `json:"type,omitempty"`
	Payload   string `json:"payload"`
	Rationale string `json:"rationale,omitempty"`
	Step      int    `json:"step"` // the run's step that drafted it
}

// Citation is a passage of knowledge a run read (ADR-0022 D4).
type Citation struct {
	Document string `json:"document"`
	Title    string `json:"title"`
	Chunk    int    `json:"chunk"`
	Step     int    `json:"step"`
}

// Signal is what a person made of an agent's work (ADR-0021 D7): a draft
// confirmed, changed or rejected; a proposal accepted, corrected, or bypassed
// by someone acting meanwhile. Signals are the evaluation's reference.
type Signal struct {
	At     time.Time `json:"at"`
	Kind   string    `json:"kind"` // confirmed, changed, rejected, accepted, corrected, bypassed
	By     string    `json:"by"`
	Detail string    `json:"detail,omitempty"`
	Value  string    `json:"value,omitempty"` // what the person chose instead: a changed payload
}

// RunStep is one step the model chose, and what came of it: the decision trace.
type RunStep struct {
	At        time.Time `json:"at"`
	Tool      string    `json:"tool"`
	Arguments string    `json:"arguments,omitempty"`
	Rationale string    `json:"rationale,omitempty"`
	Outcome   string    `json:"outcome"`
	Tokens    int       `json:"tokens,omitempty"`
}

type agentTool struct {
	kind   string // action, protocol, read, context, search, ask, finish
	schema string // the action, "<protocol>#<action>" or the read's name
	target string // the action's target type
	platform.Field
	tool Tool
}

type agentDef struct {
	app string
	platform.Agent
	tools map[string]agentTool // by the model's name for it
}

// Agents is a tenant's agent app.
type Agents struct {
	mu     sync.Mutex
	t      *Tenant
	ledger *platform.Ledger
	defs   map[string]*agentDef // "<app>.<name>"
	busy   map[string]bool      // runs whose model is being called
}

func NewAgents(tenant string) *Agents {
	var actions []platform.Action
	for _, e := range agentEntities() {
		actions = append(actions, platform.EntityActions(e)...)
	}
	actions = append(actions,
		platform.Action{Schema: SchemaRunStart, Target: RunType, Capability: "runs", Title: "Ask an agent", Roles: []string{platform.AnyMember},
			Description: "Give one of your apps' agents a goal; it works on your behalf, within what you may do yourself.",
			Payload: []platform.Field{{Name: "agent", Type: "string", Required: true, Description: "The agent, <app>.<name>"},
				{Name: "goal", Type: "string", Required: true, Description: "What it should achieve"}, {Name: "ref", Type: "string", Description: "The record it is about, <type>/<id>"},
				{Name: "act", Type: "boolean", Description: "Act within your grants instead of drafting for you to confirm (how A2A callers run agents)"},
				{Name: "language", Type: "string", Description: "The language to answer in, such as zh-CN; empty: English"}}},
		platform.Action{Schema: SchemaRunStep, Target: RunType, Capability: "runs", Title: "Take step", Payload: []platform.Field{}, Roles: []string{AgentAdmin},
			Description: "Made by the host: a step the model chose."},
		platform.Action{Schema: SchemaRunCancel, Target: RunType, Capability: "runs", Title: "Stop run", Payload: []platform.Field{}, Roles: []string{AgentAdmin, platform.AnyMember},
			Description: "Stop a run; what it did stays done. Anyone may stop a run on their behalf."},
		platform.Action{Schema: SchemaRunConfirm, Target: RunType, Capability: "runs", Title: "Confirm draft", Roles: []string{platform.AnyMember},
			Description: "Do what the agent running for you drafted, as yourself; change its fields first if you want.",
			Payload:     []platform.Field{{Name: "payload", Type: "json", Description: "The action's fields, changed; empty: as drafted"}}},
		platform.Action{Schema: SchemaRunReject, Target: RunType, Capability: "runs", Title: "Reject draft", Roles: []string{platform.AnyMember},
			Description: "Refuse what the agent running for you drafted; it goes on with your reason.",
			Payload:     []platform.Field{{Name: "reason", Type: "string", Description: "Why, for the agent"}}},
		platform.Action{Schema: SchemaEvalStart, Target: EvaluationType, Capability: "evaluations", Title: "Evaluate a model", Roles: []string{AgentAdmin},
			Description: "Re-run an agent's past runs that people confirmed or corrected, dry, with a candidate model, and compare.",
			Payload: []platform.Field{{Name: "agent", Type: "string", Required: true, Description: "The agent, <app>.<name>"},
				{Name: "model", Type: "string", Required: true, Description: "The candidate, an enabled model <provider>/<model>"}}})
	return &Agents{ledger: platform.NewLedger(tenant, AgentApp, platform.NewCatalog(actions...), RunType, EvaluationType, MemoryType), defs: map[string]*agentDef{}, busy: map[string]bool{}}
}

func agentEntities() []platform.Entity {
	return []platform.Entity{{Type: RunType, Title: "Agent run", Model: AgentRunRecord{}, Display: "title"},
		{Type: EvaluationType, Title: "Agent evaluation", Model: Evaluation{}, Display: "model"}, memoryEntity()}
}

func (a *Agents) Manifest() platform.Manifest {
	return platform.Manifest{ID: AgentApp, Title: "Agents", Version: "1", Actions: a.ledger.Catalog, Entities: agentEntities(),
		Reads: []string{"agents", "runs", "memories"}, Everyone: []string{"agents", "runs", "memories"},
		Settings: []platform.Setting{
			{Name: SettingAgentModel, Title: "Model for agents", Type: "text", Default: "",
				Description: "The enabled model agents call, <provider>/<model>; it must call tools. Empty: agents stop and hand their goal to a person."},
			{Name: SettingAgentDaily, Title: "Tokens per agent per day", Type: "integer", Default: "200000",
				Description: "An agent that has used this many tokens today stops its runs until tomorrow."},
			{Name: SettingPublished, Title: "Published over A2A", Type: "text", Default: "",
				Description: "Agents other systems may call over A2A 1.0, comma-separated <app>.<name>; their cards are at /a2a/<tenant>/<agent>/.well-known/agent-card.json."},
			{Name: SettingTranscriptDays, Title: "Days transcripts are kept", Type: "integer", Default: "30",
				Description: "Every model call's full request and answer are kept this many days outside the journal, for agent administrators; they may hold personal data."},
		}}
}

func (a *Agents) Declarations() []*pb.AuthorityDeclaration { return a.ledger.Declarations() }
func (a *Agents) Snapshot() (json.RawMessage, error)       { return a.ledger.Snapshot() }
func (a *Agents) Restore(raw json.RawMessage) error        { return a.ledger.Restore(raw) }
func (a *Agents) Input(platform.Caller, string, []byte, time.Time) (any, *kernel.Error) {
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
}

var jsonTypes = map[string]string{"string": "string", "integer": "integer", "number": "number", "boolean": "boolean", "date": "string", "datetime": "string"}

func agentToolName(s string) string {
	return strings.NewReplacer(".", "_", "#", "__", "/", "_", ":", "_", "-", "_").Replace(s)
}

func rationale() map[string]any {
	return map[string]any{"type": "string", "description": "Why you take this step, in one sentence"}
}

// declare registers an app's agents and their tools (NewTenant).
func (a *Agents) declare(app platform.App) error {
	m := app.Manifest()
	for _, ag := range m.Agents {
		id := m.ID + "." + ag.Name
		if ag.Name == "" || ag.Title == "" || ag.Instructions == "" || a.defs[id] != nil {
			return fmt.Errorf("agent %s: a unique name, a title and instructions are required", id)
		}
		d := &agentDef{app: m.ID, Agent: ag, tools: map[string]agentTool{}}
		if d.Budget.Steps == 0 {
			d.Budget.Steps = 10
		}
		if d.Budget.Tokens == 0 {
			d.Budget.Tokens = 40000
		}
		if d.Budget.Actions == 0 {
			d.Budget.Actions = 3
		}
		for _, name := range ag.Tools {
			var tool agentTool
			switch {
			case strings.HasPrefix(name, "emit:"):
				kind := strings.TrimPrefix(name, "emit:")
				i := slices.IndexFunc(m.Emits, func(e platform.EffectKind) bool { return e.Name == kind })
				if i < 0 {
					return fmt.Errorf("agent %s emits %s, not an effect kind of %s", id, kind, m.ID)
				}
				tool = agentTool{kind: "effect", schema: kind, tool: Tool{Description: m.Emits[i].Title + ": " + m.Emits[i].Description + " Waits for the receiver's answer.",
					Properties: map[string]any{"message": map[string]any{"type": "string", "description": "What to ask or tell the receiver"}, "rationale": rationale()},
					Required:   []string{"message", "rationale"}}}
			case strings.HasPrefix(name, "read:"):
				read := strings.TrimPrefix(name, "read:")
				if !slices.Contains(m.Reads, read) {
					return fmt.Errorf("agent %s reads %s, not a read of %s", id, read, m.ID)
				}
				tool = agentTool{kind: "read", schema: read, tool: Tool{Description: "Read " + read + " of " + m.ID + ".", Properties: map[string]any{"rationale": rationale()}, Required: []string{"rationale"}}}
			default:
				var action platform.Action
				kind := "action"
				if protocol, schema, ok := strings.Cut(name, "#"); ok {
					p := slices.IndexFunc(m.Consumes, func(c platform.Consumption) bool { return c.Protocol == protocol })
					if p < 0 {
						return fmt.Errorf("agent %s uses %s of a protocol %s does not consume", id, name, m.ID)
					}
					provided, found := platform.Action{}, false
					for _, x := range a.t.protocolActions(protocol) {
						if x.Schema == schema {
							provided, found = x, true
						}
					}
					if !found {
						return fmt.Errorf("agent %s uses %s, not an action of the protocol", id, name)
					}
					action, kind = provided, "protocol"
				} else {
					own, ok := m.Actions.Action(name)
					if !ok {
						return fmt.Errorf("agent %s uses %s, not an action of %s", id, name, m.ID)
					}
					action = own
				}
				props := map[string]any{"target": map[string]any{"type": "string", "description": "The ID of the " + cmpOr(action.Target, "record") + " the action is about"}, "rationale": rationale()}
				required := []string{"target", "rationale"}
				for _, f := range action.Payload {
					props[f.Name] = map[string]any{"type": cmpOr(jsonTypes[f.Type], "string"), "description": f.Description}
					if f.Required {
						required = append(required, f.Name)
					}
				}
				tool = agentTool{kind: kind, schema: name, target: action.Target, tool: Tool{Description: action.Title + ": " + action.Description, Properties: props, Required: required}}
			}
			tool.tool.Name = agentToolName(name)
			d.tools[tool.tool.Name] = tool
		}
		for _, b := range []agentTool{
			{kind: "context", tool: Tool{Name: "context", Description: "Read a record with its history, the records it references and that reference it, its links across apps, the flows and tasks about it.",
				Properties: map[string]any{"type": map[string]any{"type": "string", "description": "The entity type, like mes.order"}, "id": map[string]any{"type": "string"}, "rationale": rationale()}, Required: []string{"type", "id", "rationale"}}},
			{kind: "search", tool: Tool{Name: "search", Description: "Search records of every type you may read by text; answers types, IDs and titles.",
				Properties: map[string]any{"query": map[string]any{"type": "string"}, "rationale": rationale()}, Required: []string{"query", "rationale"}}},
			{kind: "knowledge", tool: Tool{Name: "knowledge", Description: "Search the tenant's documents and knowledge (house rules, manuals, FAQs) by what you need to know; answers passages with their sources, to cite.",
				Properties: map[string]any{"query": map[string]any{"type": "string"}, "rationale": rationale()}, Required: []string{"query", "rationale"}}},
			{kind: "remember", tool: Tool{Name: "remember", Description: "Keep a short fact for your next runs: something people told or corrected that you will need again. People see and may forget it.",
				Properties: map[string]any{"fact": map[string]any{"type": "string"}, "about_person": map[string]any{"type": "boolean", "description": "Only for runs for the person you work for now"}, "rationale": rationale()},
				Required:   []string{"fact", "rationale"}}},
			{kind: "ask", tool: Tool{Name: "ask", Description: "Ask a person when you are unsure or need a decision; the run waits for the answer.",
				Properties: map[string]any{"question": map[string]any{"type": "string"}, "answers": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "The answers to offer, if any"}, "rationale": rationale()},
				Required:   []string{"question", "rationale"}}},
			{kind: "finish", tool: Tool{Name: "finish", Description: "End the run with its result, the answer to the goal.",
				Properties: map[string]any{"result": map[string]any{"type": "string", "description": "The result; JSON when the goal asks for it"}, "rationale": rationale()}, Required: []string{"result", "rationale"}}},
		} {
			d.tools[b.tool.Name] = b
		}
		a.defs[id] = d
	}
	return nil
}

// agentMember is the principal an agent signs as.
func (a *Agents) member(agent string) platform.Member {
	return platform.Member{ID: "agent:" + agent, Tenant: a.t.ID, Roles: map[string]string{}, Agent: true}
}

func (a *Agents) run(r AgentRunRecord) platform.AgentRun {
	return platform.AgentRun{ID: r.ID, Agent: r.Agent, Goal: r.Goal, Ref: r.Ref, OnBehalf: r.OnBehalf}
}

func (a *Agents) runs(c platform.Caller, state ...string) []AgentRunRecord {
	var terms []any
	for i, s := range state {
		if i < len(state)-1 {
			terms = append(terms, "|")
		}
		terms = append(terms, []any{"state", "=", s})
	}
	domain, _ := json.Marshal(terms)
	out, _, _ := platform.Find[AgentRunRecord](c, platform.Query{Domain: domain, Sort: []string{"id"}})
	return out
}

// Submit takes members' requests, their answers to drafts and stops; steps are the host's.
func (a *Agents) Submit(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if s.GetSchema().GetName() == SchemaRunStep {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	if record, err, ok := a.ledger.Generated(c, s, now, nil, agentEntities()...); ok { // keeping and forgetting memories
		return record, err
	}
	var p struct {
		Agent, Goal, Ref, Reason, Model, Language string
		Act                                       bool
		Payload                                   json.RawMessage
	}
	json.Unmarshal(s.GetPayload(), &p)
	d := a.defs[p.Agent]
	id := s.GetTarget().GetId()
	run, known := platform.Get[AgentRunRecord](a.t.automation(AgentApp, c.Replaying), id)
	allowed := func() bool {
		switch s.GetSchema().GetName() {
		case SchemaRunStart:
			return d != nil && c.Roles[d.app] != "" // an agent of an app the member works in
		case SchemaRunCancel:
			return c.Roles[AgentApp] == AgentAdmin || known && run.OnBehalf == c.ID
		case SchemaEvalStart:
			return true // the catalog's role
		}
		return known && run.OnBehalf == c.ID // drafts are answered by whom the run is for
	}
	return a.ledger.Receive(c, s, now, allowed, func() (func(*pb.ChangeRecord), *kernel.Error) {
		switch s.GetSchema().GetName() {
		case SchemaRunStart:
			if known {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
			}
			if d == nil || strings.TrimSpace(p.Goal) == "" {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
			}
			run := a.create(id, p.Agent, p.Goal, p.Ref, c.ID, "", "", 0, now)
			run.Acts, run.Language = p.Act, p.Language
			return func(r *pb.ChangeRecord) { a.t.automation(AgentApp, c.Replaying).Put(r, run) }, nil
		case SchemaEvalStart:
			return a.startEvaluation(c, id, struct{ Agent, Model string }{p.Agent, p.Model})
		case SchemaRunCancel:
			if !known || run.State == "done" || run.State == "stopped" {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
			}
			return func(r *pb.ChangeRecord) {
				a.stop(c, r, &run, "stopped by "+c.ID, now)
				a.t.automation(AgentApp, c.Replaying).Put(r, run)
			}, nil
		case SchemaRunConfirm, SchemaRunReject:
			if len(run.Draft) == 0 || run.State != "waiting" {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
			}
			draft, proposed := run.Draft[0], (*Signal)(nil)
			step := &run.Steps[draft.Step]
			if s.GetSchema().GetName() == SchemaRunReject {
				step.Outcome += "\nrejected by " + c.ID + ": " + cmpOr(p.Reason, "no reason given")
				run.Signals = append(run.Signals, Signal{At: now, Kind: "rejected", By: c.ID, Detail: p.Reason})
				proposed = &run.Signals[len(run.Signals)-1]
			} else {
				payload, kind := draft.Payload, "confirmed"
				if len(p.Payload) > 0 && string(p.Payload) != "null" && !sameJSON(p.Payload, []byte(draft.Payload)) {
					payload, kind = string(p.Payload), "changed"
				}
				done, err := a.confirm(c, run, draft, payload, now)
				if err != nil {
					return nil, err // the draft stays; the person may change it or reject it
				}
				step.Outcome += "\n" + kind + " by " + c.ID + ": " + done
				signal := Signal{At: now, Kind: kind, By: c.ID}
				if kind == "changed" {
					signal.Value = payload
				}
				run.Signals = append(run.Signals, signal)
				if kind == "changed" {
					proposed = &run.Signals[len(run.Signals)-1]
				}
				run.ActionsUsed++
			}
			run.Draft, run.State, run.Task = nil, "running", ""
			return func(r *pb.ChangeRecord) {
				a.t.automation(AgentApp, c.Replaying).Put(r, run)
				if proposed != nil {
					a.propose(c, r, run, *proposed, now)
				}
			}, nil
		}
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
	})
}

// confirm does a drafted action as the person who confirmed it, correlated to the run.
func (a *Agents) confirm(c platform.Caller, run AgentRunRecord, d Draft, payload string, now time.Time) (string, *kernel.Error) {
	t := a.t
	key := fmt.Sprintf("agent:%s:%d:confirm", run.ID, d.Step+1)
	if d.Kind == "protocol" {
		protocol, schema, _ := strings.Cut(d.Action, "#")
		_, _, err := t.invoke(platform.NewCaller(runtime{t}, c.Member, a.defs[run.Agent].app, c.Replaying, false), protocol, schema, d.Target, []byte(payload), key, run.ID, now)
		if err != nil {
			return "", err
		}
		return "done: " + schema + " " + d.Target, nil
	}
	app := t.app(t.authorityOf(d.Type))
	if app == nil {
		return "", &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	record, err := app.Submit(t.caller(c.Member, app, c.Replaying), &pb.Submission{TenantId: t.ID, PrincipalId: c.ID, Authority: app.Manifest().ID, IdempotencyKey: key,
		Target: &pb.EntityRef{Type: d.Type, Id: d.Target}, Schema: &pb.SchemaRef{Name: d.Action, Version: 1}, Payload: []byte(payload), CorrelationId: run.ID}, now)
	if err != nil {
		return "", err
	}
	return "done: " + d.Action + " " + d.Target + " (" + record.GetChangeId() + ")", nil
}

func sameJSON(x, y []byte) bool {
	var a, b any
	return json.Unmarshal(x, &a) == nil && json.Unmarshal(y, &b) == nil && fmt.Sprint(a) == fmt.Sprint(b)
}

// signal records what a person made of a run's work, in another app's decision.
func (a *Agents) signal(c platform.Caller, r *pb.ChangeRecord, id string, sig Signal, now time.Time) {
	c = a.t.automation(AgentApp, c.Replaying)
	if run, ok := platform.Get[AgentRunRecord](c, id); ok {
		run.Signals = append(run.Signals, sig)
		c.Put(r, run)
		a.propose(c, r, run, sig, now)
	}
}

func (a *Agents) create(id, agent, goal, ref, onBehalf, flow, step string, token int, now time.Time) AgentRunRecord {
	title := goal
	if len(title) > 80 {
		title = title[:77] + "..."
	}
	run := AgentRunRecord{Record: platform.Record{ID: id}, Agent: agent, Title: title, Goal: goal, Ref: ref, OnBehalf: onBehalf, Flow: flow, Step: step, Token: token,
		State: "running", Steps: []RunStep{}}
	if typ, rid, ok := strings.Cut(ref, "/"); ok {
		if view, err := a.t.Context(a.reader(run), typ, rid, now); err == nil {
			raw, _ := json.Marshal(view)
			run.Seen = clip(string(raw), 6000)
		}
	}
	return run
}

// Read "agents": the declared agents, their tools and budgets; "runs": the
// runs on the caller's behalf.
func (a *Agents) Read(c platform.Caller, name string) (any, *kernel.Error) {
	if name == "memories" { // what agents remember about the caller
		about, _ := json.Marshal([]any{[]any{"for", "=", c.ID}, []any{"state", "!=", "forgotten"}})
		out, _, _ := platform.Find[Memory](a.t.automation(AgentApp, c.Replaying), platform.Query{Domain: about, Sort: []string{"-created"}, Limit: 100})
		return out, nil
	}
	if name == "runs" {
		mine, _ := json.Marshal([]any{[]any{"onBehalf", "=", c.ID}})
		out, _, _ := platform.Find[AgentRunRecord](a.t.automation(AgentApp, c.Replaying), platform.Query{Domain: mine, Sort: []string{"-created"}, Limit: 50})
		return out, nil
	}
	out := []AgentInfo{}
	for _, id := range slices.Sorted(maps.Keys(a.defs)) {
		d := a.defs[id]
		out = append(out, AgentInfo{ID: id, App: d.app, Title: d.Title, Instructions: d.Instructions, Tools: append(slices.Clone(d.Tools), "context", "search", "knowledge", "remember", "ask", "finish"), Budget: d.Budget})
	}
	return out, nil
}

// AgentInfo is a declared agent as people see it.
type AgentInfo struct {
	ID           string          `json:"id"`
	App          string          `json:"app"`
	Title        string          `json:"title"`
	Instructions string          `json:"instructions"`
	Tools        []string        `json:"tools"`
	Budget       platform.Budget `json:"budget"`
}
