package agents

import (
	"bytes"
	"cmp"
	"embed"
	"encoding/json"
	"fmt"
	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

	"google.golang.org/protobuf/proto"

	"platformserver/internal/agents"
	"platformserver/platform"
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

func (a *Agents) DeclareAgentApp(app platform.App) error {
	m := app.Manifest()
	for _, ag := range m.Agents {
		id := m.ID + "." + ag.Name
		if ag.Name == "" || ag.Title == "" || ag.Instructions == "" || a.defs[id] != nil {
			return fmt.Errorf("agent %s: a unique name, a title and instructions are required", id)
		}
		d := &platform.CompiledAgent{App: m.ID, Agent: ag, Toolset: map[string]platform.CompiledAgentTool{}}
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
			var tool platform.CompiledAgentTool
			switch {
			case strings.HasPrefix(name, "emit:"):
				kind := strings.TrimPrefix(name, "emit:")
				i := slices.IndexFunc(m.Emits, func(e platform.EffectKind) bool { return e.Name == kind })
				if i < 0 {
					return fmt.Errorf("agent %s emits %s, not an effect kind of %s", id, kind, m.ID)
				}
				tool = platform.CompiledAgentTool{Kind: "effect", Schema: kind, Tool: platform.Tool{Description: m.Emits[i].Title + ": " + m.Emits[i].Description + " Waits for the receiver's answer.",
					Properties: map[string]any{"message": map[string]any{"type": "string", "description": "What to ask or tell the receiver"}, "rationale": rationale()},
					Required:   []string{"message", "rationale"}}}
			case strings.HasPrefix(name, "query:"):
				query := strings.TrimPrefix(name, "query:")
				i := slices.IndexFunc(m.Queries, func(q platform.NamedQuery) bool { return q.Name == query })
				if i < 0 {
					return fmt.Errorf("agent %s runs %s, not a query of %s", id, query, m.ID)
				}
				q := m.Queries[i]
				props, required := map[string]any{"rationale": rationale()}, []string{"rationale"}
				if q.By != "" {
					props["for"] = map[string]any{"type": "string", "description": "The ID of the record it is run for"}
					required = append(required, "for")
				}
				tool = platform.CompiledAgentTool{Kind: "query", Schema: query, Query: q, Tool: platform.Tool{Description: q.Title + ": " + q.Description, Properties: props, Required: required}}
			case strings.HasPrefix(name, "read:"):
				read := strings.TrimPrefix(name, "read:")
				if !slices.Contains(m.Reads, read) {
					return fmt.Errorf("agent %s reads %s, not a read of %s", id, read, m.ID)
				}
				fields := m.ReadInputs[read]
				schema := platform.ActionInputSchema(platform.Action{Payload: fields}, nil, nil)
				tool = platform.CompiledAgentTool{Kind: "read", Schema: read, Inputs: fields, Tool: platform.Tool{Description: "Read " + read + " of " + m.ID + ".", Properties: map[string]any{"inputs": schema, "rationale": rationale()}, Required: []string{"inputs", "rationale"}}}
			default:
				var action platform.Action
				kind := "action"
				if protocol, schema, ok := strings.Cut(name, "#"); ok {
					p := slices.IndexFunc(m.Consumes, func(c platform.Consumption) bool { return c.Protocol == protocol })
					if p < 0 {
						return fmt.Errorf("agent %s uses %s of a protocol %s does not consume", id, name, m.ID)
					}
					provided, found := platform.Action{}, false
					for _, x := range a.host.ProtocolActions(protocol) {
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
				info, _ := a.host.Entity(action.Target)
				schema := platform.ActionInputSchema(action, &info, nil)
				props := map[string]any{"target": map[string]any{"type": "string", "description": "The ID of the " + cmp.Or(action.Target, "record") + " the action is about"}, "rationale": rationale(), "payload": schema}
				tool = platform.CompiledAgentTool{Kind: kind, Schema: name, Target: action.Target, Tool: platform.Tool{Description: action.Title + ": " + action.Description, Properties: props, Required: []string{"target", "rationale", "payload"}}}
			}
			tool.Tool.Name = agentToolName(name)
			d.Toolset[tool.Tool.Name] = tool
		}
		for _, b := range []platform.CompiledAgentTool{
			{Kind: "context", Tool: platform.Tool{Name: "context", Description: "Read a record with its history, the records it references and that reference it, its links across apps, the flows and tasks about it.",
				Properties: map[string]any{"type": map[string]any{"type": "string", "description": "The entity type, like core.material"}, "id": map[string]any{"type": "string"}, "rationale": rationale()}, Required: []string{"type", "id", "rationale"}}},
			{Kind: "search", Tool: platform.Tool{Name: "search", Description: "Search records of every type you may read by text; answers types, IDs and titles.",
				Properties: map[string]any{"query": map[string]any{"type": "string"}, "rationale": rationale()}, Required: []string{"query", "rationale"}}},
			{Kind: "knowledge", Tool: platform.Tool{Name: "knowledge", Description: "Search the tenant's documents and knowledge (house rules, manuals, FAQs) by what you need to know; answers passages with their sources, to cite.",
				Properties: map[string]any{"query": map[string]any{"type": "string"}, "rationale": rationale()}, Required: []string{"query", "rationale"}}},
			{Kind: "remember", Tool: platform.Tool{Name: "remember", Description: "Keep a short fact for your next runs: something people told or corrected that you will need again. People see and may forget it.",
				Properties: map[string]any{"fact": map[string]any{"type": "string"}, "about_person": map[string]any{"type": "boolean", "description": "Only for runs for the person you work for now"}, "rationale": rationale()},
				Required:   []string{"fact", "rationale"}}},
			{Kind: "ask", Tool: platform.Tool{Name: "ask", Description: "Ask a person when you are unsure or need a decision; the run waits for the answer.",
				Properties: map[string]any{"question": map[string]any{"type": "string"}, "answers": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "The answers to offer, if any"}, "rationale": rationale()},
				Required:   []string{"question", "rationale"}}},
			{Kind: "finish", Tool: platform.Tool{Name: "finish", Description: "End the run with its result, the answer to the goal.",
				Properties: map[string]any{"result": map[string]any{"type": "string", "description": "The result; JSON when the goal asks for it"}, "rationale": rationale()}, Required: []string{"result", "rationale"}}},
		} {
			d.Toolset[b.Tool.Name] = b
		}
		a.defs[id] = d
	}
	return nil
}

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

func (a *Agents) Submit(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if s.GetSchema().GetName() == platform.SchemaRunStep {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	if record, err, ok := a.ledger.Generated(c, s, now, nil, agentEntities()...); ok {
		return record, err
	}
	var p struct {
		Agent, Goal, Ref, Reason, Model, Language, Candidate string
		Owner, Action, Target, Rationale                     string
		Act, Suite                                           bool
		Payload                                              json.RawMessage
	}
	json.Unmarshal(s.GetPayload(), &p)
	d := a.def(p.Agent)
	id := s.GetTarget().GetId()
	run, known := platform.Get[platform.AgentRunRecord](a.host.Automation(c, platform.AgentApp), id)
	allowed := func() bool {
		switch s.GetSchema().GetName() {
		case platform.SchemaToolPropose:
			return true
		case platform.SchemaRunStart:
			return d != nil && c.Roles[d.App] != ""
		case platform.SchemaRunCancel:
			return c.Roles[platform.AgentApp] == platform.AgentAdmin || known && run.OnBehalf == c.ID
		case platform.SchemaEvalStart, platform.SchemaSuspend, platform.SchemaResume:
			return true
		}
		return known && !c.Agent && !c.Automation && (run.OnBehalf == c.ID || slices.Contains(run.Reviewers, c.ID))
	}
	return a.ledger.Receive(c, s, now, allowed, func() (func(*pb.ChangeRecord), *kernel.Error) {
		switch s.GetSchema().GetName() {
		case platform.SchemaRunStart:
			if known {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
			}
			if d == nil || strings.TrimSpace(p.Goal) == "" {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
			}
			if a.host.AgentSuspended("agent:"+p.Agent) && !c.Replaying {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The agent {agent} is suspended", p.Agent)
			}
			run, err := a.create(id, p.Agent, p.Goal, p.Ref, c.ID, "", "", 0, now, !c.Replaying && c.Staging())
			if err != nil {
				return nil, err
			}
			run.Acts, run.Language = p.Act && c.Replaying, p.Language
			run.Scopes = slices.Clone(c.Scopes)
			return func(r *pb.ChangeRecord) { a.host.Automation(c, platform.AgentApp).Put(r, run) }, nil
		case platform.SchemaToolPropose:
			if known {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The proposal already exists")
			}
			app := a.host.App(p.Owner)
			if app == nil {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The action owner is unavailable")
			}
			action, ok := app.Manifest().Actions.Action(p.Action)
			if !ok || platform.StaticControlAction(action) || action.Automation || strings.HasPrefix(p.Action, "agent.run.") || p.Action == platform.SchemaToolPropose {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "This tool cannot propose confirmation or runtime control")
			}
			run = platform.AgentRunRecord{Record: platform.Record{ID: id}, Agent: p.Owner + ".Tool", Title: action.Title, Goal: action.Title, Ref: action.Target + "/" + p.Target, OnBehalf: c.ID, Scopes: slices.Clone(c.Scopes), State: "waiting", ToolOnly: true, Steps: []platform.RunStep{{At: now, Tool: agentToolName(p.Action), Outcome: "awaiting confirmation"}}}
			draft, refusal := a.toolDraft(c, run, platform.Draft{Kind: "action", Owner: p.Owner, Action: p.Action, Type: action.Target, Target: p.Target, Payload: string(p.Payload), Rationale: p.Rationale}, now)
			if refusal != nil {
				return nil, refusal
			}
			run.Draft = []platform.Draft{draft}
			run.Proposals = append(run.Proposals, draft)
			return func(receipt *pb.ChangeRecord) {
				run.Proposals[len(run.Proposals)-1].Evidence = platform.AgentApp + "/" + receipt.GetChangeId()
				a.host.Automation(c, platform.AgentApp).Put(receipt, run)
				a.assignDraft(c, receipt, &run, action.Title, now)
				a.host.Automation(c, platform.AgentApp).Put(receipt, run)
			}, nil
		case platform.SchemaEvalStart:
			return a.startEvaluation(c, id, struct {
				Agent, Model, Candidate string
				Suite                   bool
			}{p.Agent, p.Model, p.Candidate, p.Suite})
		case platform.SchemaSuspend, platform.SchemaResume:
			return a.switchDecision(c, s, struct{ Reason string }{p.Reason})
		case platform.SchemaRunCancel:
			if !known || run.State == "done" || run.State == "stopped" {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
			}
			return func(r *pb.ChangeRecord) {
				a.stop(c, r, &run, "stopped by "+c.ID, now)
				a.host.Automation(c, platform.AgentApp).Put(r, run)
			}, nil
		case platform.SchemaRunConfirm, platform.SchemaRunReject:
			if len(run.Draft) == 0 || run.State != "waiting" {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
			}
			draft, proposed := run.Draft[0], (*platform.Signal)(nil)
			originalDraft := draft
			step := &run.Steps[draft.Step]
			if s.GetSchema().GetName() == platform.SchemaRunReject {
				step.Outcome += "\nrejected by " + c.ID + ": " + cmp.Or(p.Reason, "no reason given")
				run.Signals = append(run.Signals, platform.Signal{At: now, Kind: "rejected", By: c.ID, Detail: p.Reason})
				proposed = &run.Signals[len(run.Signals)-1]
			} else {
				payload, kind := draft.Payload, "confirmed"
				if len(p.Payload) > 0 && string(p.Payload) != "null" && !sameJSON(p.Payload, []byte(draft.Payload)) {
					payload, kind = string(p.Payload), "changed"
				}
				done, err := a.confirm(c, run, draft, payload, now)
				if err != nil {
					return nil, err
				}
				step.Outcome += "\n" + kind + " by " + c.ID + ": " + done
				signal := platform.Signal{At: now, Kind: kind, By: c.ID}
				if kind == "changed" {
					signal.Value = payload
				}
				run.Signals = append(run.Signals, signal)
				if kind == "changed" {
					proposed = &run.Signals[len(run.Signals)-1]
				}
				run.ActionsUsed++
				draft.Accepted, draft.Result = payload, done
				draft.ExpectedSources = map[string]uint32{}
				for ref := range draft.Sources {
					if value, ok := a.host.Record(ref); ok {
						raw, _ := json.Marshal(value)
						var header struct {
							Revision uint32 `json:"revision"`
						}
						_ = json.Unmarshal(raw, &header)
						draft.ExpectedSources[ref] = header.Revision
					}
				}
				if draft.Type != "" {
					if value, ok := a.host.Record(draft.Type + "/" + draft.Target); ok {
						raw, _ := json.Marshal(value)
						var header struct {
							Revision uint32 `json:"revision"`
						}
						_ = json.Unmarshal(raw, &header)
						draft.ExpectedSources[draft.Type+"/"+draft.Target] = header.Revision
					}
				}
				draft.Outcome = "accepted"
			}
			if s.GetSchema().GetName() == platform.SchemaRunReject {
				draft.Rejected, draft.Outcome = draft.Payload, "rejected"
			}
			if draft.Authorization != "" {
				draft.Authorization, _ = platform.CanonicalDigest([]any{draft.Authorization, c.ID, c.Tenant, c.Roles, c.Grants, c.Scopes, c.Status})
			}
			if draft.Accepted != "" && !sameJSON([]byte(draft.Accepted), []byte(originalDraft.Payload)) {
				draft.Rejected = originalDraft.Payload
			}
			if len(run.Proposals) == 0 {
				run.Proposals = append(run.Proposals, draft)
			} else {
				run.Proposals[len(run.Proposals)-1] = draft
			}
			oldTask := run.Task
			run.Draft, run.State, run.Task = nil, "running", ""
			if run.ToolOnly {
				run.State = "done"
			} else if draft.Kind == "effect" && draft.Outcome == "accepted" {
				run.State, run.Task = "waiting", fmt.Sprintf("effect:%s/%s:agent:%s:%d:confirm", draft.Owner, draft.Action, run.ID, draft.Step+1)
			}
			return func(r *pb.ChangeRecord) {
				run.Proposals[len(run.Proposals)-1].Evidence = platform.AgentApp + "/" + r.GetChangeId()
				if oldTask != "" {
					a.host.AgentTasks().Close(c, r, oldTask)
				}
				a.host.Automation(c, platform.AgentApp).Put(r, run)
				if proposed != nil {
					a.propose(c, r, run, *proposed, now)
				}
			}, nil
		}
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
	})
}

func (a *Agents) confirm(c platform.Caller, run platform.AgentRunRecord, d platform.Draft, payload string, now time.Time) (string, *kernel.Error) {
	if !run.ToolOnly && a.changedDefinition(run, a.def(run.Agent)) {
		return "", platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The agent definition changed; start a new run")
	}
	h := a.host
	if d.Owner == "" {
		d.Owner = a.authorityOf(d.Type)
		if d.Kind == "protocol" {
			d.Owner = a.def(run.Agent).App
		}
	}
	if d.Definition != "" {
		current, refusal := a.toolDraft(c, run, d, now)
		if refusal != nil {
			return "", refusal
		}
		if d.Authorization != "" && current.Authorization != d.Authorization {
			return "", platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The proposal authorization changed; create a new proposal")
		}
		if current.Definition != d.Definition {
			return "", platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The proposed action definition changed; create a new proposal")
		}
		for ref, revision := range d.Sources {
			if current.Sources[ref] != revision {
				return "", platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The proposal source changed; create a new proposal")
			}
		}
	}
	key := fmt.Sprintf("agent:%s:%d:confirm", run.ID, d.Step+1)
	if d.Kind == "effect" {
		sender := platform.ActingCaller(c, c.Member, d.Owner, false)
		endRun := h.ScopeAgentRun(run.ID)
		n, err := sender.Emit(d.Action, key, run.Ref, json.RawMessage(payload), now)
		endRun()
		if err != nil {
			return "", err
		}
		if n == 0 {
			return "", platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "No endpoint receives this effect")
		}
		return "sent: " + d.Action, nil
	}
	if d.Kind == "protocol" {
		protocol, schema, _ := strings.Cut(d.Action, "#")
		_, _, err := h.InvokeDecision(platform.ActingCaller(c, c.Member, d.Owner, false), protocol, schema, d.Target, []byte(payload), key, run.ID, now)
		if err != nil {
			return "", err
		}
		return "done: " + schema + " " + d.Target, nil
	}
	app := h.App(a.authorityOf(d.Type))
	if app == nil {
		return "", &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	record, err := platform.Decide(c, app, &pb.Submission{TenantId: a.tenant, PrincipalId: c.ID, Authority: app.Manifest().ID, IdempotencyKey: key,
		Target: &pb.EntityRef{Type: d.Type, Id: d.Target}, Schema: &pb.SchemaRef{Name: d.Action, Version: 1}, Payload: []byte(payload), CorrelationId: run.ID}, now)
	if err != nil {
		return "", err
	}
	return "done: " + d.Action + " " + d.Target + " (" + record.GetChangeId() + ")", nil
}

func sameJSON(x, y []byte) bool {
	a, aerr := agents.NormalizeStepJSON(x)
	b, berr := agents.NormalizeStepJSON(y)
	return aerr == nil && berr == nil && bytes.Equal(a, b)
}

func (a *Agents) RecordAgentSignal(c platform.Caller, r *pb.ChangeRecord, id string, sig platform.Signal, now time.Time) {
	c = a.host.Automation(c, platform.AgentApp)
	if run, ok := platform.Get[platform.AgentRunRecord](c, id); ok {
		run.Signals = append(run.Signals, sig)
		c.Put(r, run)
		a.propose(c, r, run, sig, now)
	}
}

func (a *Agents) create(id, agent, goal, ref, onBehalf, flow, step string, token int, now time.Time, captureBinding bool) (platform.AgentRunRecord, *kernel.Error) {
	title := goal
	if len(title) > 80 {
		title = title[:77] + "..."
	}
	run := platform.AgentRunRecord{Record: platform.Record{ID: id}, Agent: agent, Title: title, Goal: goal, Ref: ref, OnBehalf: onBehalf, Flow: flow, Step: step, Token: token,
		State: "running", Steps: []platform.RunStep{}}
	recorded := captureBinding
	if recorded {
		name := a.automation(platform.AgentApp, false).Setting(platform.SettingAgentModel)
		options := platform.ModelOptions{}
		if d := a.def(agent); d != nil {
			if d.Model != "" {
				name = d.Model
			}
			options = d.ModelOptions
		}
		binding, _, err := a.host.ResolveAgentModel(name, options, platform.AIStageRuntime)
		if err != nil && name != "" {
			return run, err
		}
		run.ModelBinding = &binding
		run.Model = binding.Model
		run.Release = a.host.ActiveRelease()
	}
	if d := a.def(agent); d != nil && recorded {
		run.DefinitionVersion = a.definitionVersion(d)
	}
	if flow != "" && recorded {
		if row, ok := a.host.Record("flow.instance/" + flow); ok {
			raw, _ := json.Marshal(row)
			var parent struct {
				Release string `json:"release" source:"record"`
			}
			if json.Unmarshal(raw, &parent) == nil {
				run.Release = parent.Release
			}
		}
	}
	if typ, rid, ok := strings.Cut(ref, "/"); ok {
		if view, err := a.host.AgentContext(a.readsAs(run), typ, rid, now); err == nil {
			run.Seen = agents.Clip(string(view.Body), 6000)
		}
	}
	if recorded {
		owner, name, ok := strings.Cut(agent, ".")
		if ok {
			bindings, err := a.host.ApplicationBindings(platform.AssetRef{App: owner, Kind: platform.AssetAgent, Name: name}, run.Release, "")
			if err != nil {
				return run, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Application runtime dependencies are unavailable")
			}
			run.Applications = bindings
		}
	}
	return run, nil
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

func (a *Agents) toolDraft(c platform.Caller, run platform.AgentRunRecord, draft platform.Draft, now time.Time) (platform.Draft, *kernel.Error) {
	h := a.host
	member := a.AgentPrincipal(run.Agent)
	if run.OnBehalf != "" {
		var ok bool
		member, ok = h.Member(run.OnBehalf)
		member.Scopes = slices.Clone(run.Scopes)
		if !ok {
			return draft, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The proposal recipient is unavailable")
		}
	}

	app := h.App(draft.Owner)
	if app == nil {
		return draft, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The action owner is unavailable")
	}
	actor := platform.ActingCaller(c, member, draft.Owner, false)
	if run.OnBehalf == "" {
		actor = platform.AgentCaller(c, a.AgentPrincipal(run.Agent), draft.Owner, run.ID)
	}
	var declaration any
	switch draft.Kind {
	case "protocol":
		protocol, schema, _ := strings.Cut(draft.Action, "#")
		provider, provided, ok := h.ProtocolProvider(draft.Action)
		if !ok {
			return draft, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The protocol provider is unavailable")
		}
		action, _ := provider.Manifest().Actions.Action(provided)
		if platform.StaticControlAction(action) {
			return draft, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "AI tools cannot change static platform controls")
		}
		declaration = []any{draft.Owner, draft.Action, action, action.Roles, action.NeedsApproval}
		if refusal := platform.ProbeDecision(actor, func(probe platform.Caller) *kernel.Error {
			_, _, err := h.InvokeDecision(probe, protocol, schema, draft.Target, []byte(draft.Payload), "probe:"+run.ID, run.ID, now)
			return err
		}); refusal != nil {
			return draft, refusal
		}
	case "effect":
		index := slices.IndexFunc(app.Manifest().Emits, func(kind platform.EffectKind) bool { return kind.Name == draft.Action })
		if index < 0 {
			return draft, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The effect declaration is unavailable")
		}
		declaration = app.Manifest().Emits[index]
	default:
		action, ok := app.Manifest().Actions.Action(draft.Action)
		if !ok {
			return draft, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The proposed action is unavailable")
		}
		if platform.StaticControlAction(action) {
			return draft, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "AI tools cannot change static platform controls")
		}
		declaration = []any{draft.Owner, action, action.Roles, action.NeedsApproval}
		submission := &pb.Submission{TenantId: a.tenant, PrincipalId: member.ID, Authority: draft.Owner, IdempotencyKey: "probe:" + run.ID, Target: &pb.EntityRef{Type: draft.Type, Id: draft.Target}, Schema: &pb.SchemaRef{Name: draft.Action, Version: 1}, Payload: []byte(draft.Payload)}
		if refusal := platform.ProbeDecision(actor, func(probe platform.Caller) *kernel.Error {
			_, err := platform.Decide(probe, app, submission, now)
			return err
		}); refusal != nil {
			return draft, refusal
		}
	}
	draft.Definition, _ = platform.CanonicalDigest(declaration)
	draft.Authorization, _ = platform.CanonicalDigest([]any{member.ID, member.Tenant, member.Roles, member.Grants, member.Scopes, member.Status})
	draft.Simulation = "accepted"
	draft.Outcome = "proposed"
	refs := run.SourceRefs()
	if draft.Type != "" && draft.Target != "" {
		refs = append(refs, draft.Type+"/"+draft.Target)
	}
	draft.Sources = map[string]uint32{}
	draft.ExpectedSources = map[string]uint32{}
	for _, ref := range refs {
		if value, ok := h.Record(strings.Split(ref, "#")[0]); ok {
			raw, _ := json.Marshal(value)
			var row struct {
				Revision uint32 `json:"revision"`
			}
			_ = json.Unmarshal(raw, &row)
			draft.Sources[strings.Split(ref, "#")[0]] = row.Revision
			draft.ExpectedSources[strings.Split(ref, "#")[0]] = row.Revision
		}
	}
	return draft, nil
}

func (a *Agents) assignDraft(c platform.Caller, receipt *pb.ChangeRecord, run *platform.AgentRunRecord, title string, now time.Time) {
	recipients := a.recipients(c, a.def(run.Agent), *run)
	if len(recipients) == 0 {
		run.Handoff = "No eligible recipient is configured"
		return
	}
	key := fmt.Sprintf("agent:%s:%d:draft", run.ID, len(run.Steps))
	rc := a.reviewers(c, *run)
	run.Reviewers = rc.Notify(platform.Notification{Title: "Confirm the agent's draft: " + title, Ref: platform.RunType + "/" + run.ID, Key: key}, now, recipients...)
	run.Reviewers = slices.DeleteFunc(run.Reviewers, func(id string) bool {
		member, ok := a.host.Member(id)
		return !ok || member.Agent || member.Status == platform.MemberLeft || member.Status == platform.MemberSuspended
	})
	if len(run.Reviewers) == 0 {
		run.Handoff = "No eligible recipient is configured"
		a.host.Automation(c, platform.AgentApp).Put(receipt, *run)
		return
	}
	if err := rc.Assign(receipt, platform.Assignment{Title: "Confirm the agent's draft: " + title, Body: "Review the simulated proposal before applying the business action.", Ref: platform.RunType + "/" + run.ID, To: recipients, Key: key}); err != nil {
		run.Handoff = err.Error()
		return
	}
	run.Task, run.Handoff = platform.AgentApp+":"+key, "assigned"
	a.host.Automation(c, platform.AgentApp).Put(receipt, *run)
}

func (a *Agents) evaluationDefinition(candidate, agent string) (*platform.CompiledAgent, string, error) {
	owner, ok := a.host.(platform.AgentEvaluationDefinitions)
	if !ok {
		return nil, "", fmt.Errorf("candidate agent declarations are unavailable")
	}
	app, digest, err := owner.AgentEvaluationApp(candidate, agent)
	if err != nil {
		return nil, "", err
	}
	compiler := New(a.tenant)
	compiler.host = a.host
	if err := compiler.DeclareAgentApp(app); err != nil {
		return nil, "", err
	}
	d := compiler.defs[agent]
	if d == nil || len(d.Cases) == 0 {
		return nil, "", fmt.Errorf("the candidate agent needs declared evaluation cases")
	}
	return d, digest, nil
}

func switchActions() []platform.Action {
	admin := []string{platform.AgentAdmin}
	return []platform.Action{
		{Schema: platform.SchemaSuspend, Target: platform.SwitchType, New: true, Capability: "switch", Title: "Suspend agent", Roles: admin,
			Description: "Stop an agent (target its member ID, such as agent:assistant): its runs stop at their next step, new ones are refused, and an outside agent's decisions and model calls are refused.",
			Payload:     []platform.Field{{Name: "reason", Type: "string", Description: "Why, for the audit and whom its runs were for"}}},
		{Schema: platform.SchemaResume, Target: platform.SwitchType, Capability: "switch", Title: "Resume agent", Roles: admin,
			Description: "Let a suspended agent work again.", Payload: []platform.Field{}},
	}
}

func (a *Agents) switchDecision(c platform.Caller, s *pb.Submission, p struct{ Reason string }) (func(*pb.ChangeRecord), *kernel.Error) {
	id := s.GetTarget().GetId()
	if id == "" {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	app := a.host.Automation(c, platform.AgentApp)
	sw, _ := platform.Get[platform.Switch](app, id)
	suspend := s.GetSchema().GetName() == platform.SchemaSuspend
	if sw.Suspended == suspend {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	sw.ID, sw.Suspended, sw.By, sw.Reason = id, suspend, c.ID, strings.TrimSpace(p.Reason)
	return func(r *pb.ChangeRecord) { app.Put(r, sw) }, nil
}

func (a *Agents) AgentOverview() []platform.AgentOverview {
	c := a.automation(platform.AgentApp, false)
	rows := map[string]*platform.AgentOverview{}
	a.each(func(id string, d *platform.CompiledAgent) {
		rows["agent:"+id] = &platform.AgentOverview{Member: "agent:" + id, Title: d.Title, Judged: map[string]int{}}
	})
	for _, m := range a.host.ExternalAgentMembers() {
		if rows[m.ID] == nil {
			rows[m.ID] = &platform.AgentOverview{Member: m.ID, Title: m.ID, Outside: true, Judged: map[string]int{}}
		}
	}

	runs, _, _ := platform.Find[platform.AgentRunRecord](c, platform.Query{})
	for _, r := range runs {
		row := rows["agent:"+r.Agent]
		if row == nil {
			continue
		}
		row.Runs++
		row.Actions += r.ActionsUsed
		if r.State == "running" || r.State == "waiting" {
			row.Live++
		}
		for _, s := range r.Signals {
			row.Judged[s.Kind]++
		}
	}
	for _, member := range a.host.AgentDecisionMembers() {
		if row := rows[member]; row != nil && row.Outside {
			row.Actions++
		}
	}
	{
		for _, u := range a.host.AgentUsage() {
			if row := rows[u.Member]; row != nil {
				row.Calls, row.Tokens, row.Cost = row.Calls+1, row.Tokens+u.Input+u.Output, row.Cost+u.Cost
			}
		}
	}
	out := []platform.AgentOverview{}
	for _, id := range slices.Sorted(maps.Keys(rows)) {
		row := rows[id]
		row.Suspended = a.host.AgentSuspended(id)
		out = append(out, *row)
	}
	return out
}

func (a *Agents) DueAgentTurns(now time.Time) []platform.AgentTurn {
	h := a.host
	endRead := h.BeginAgentRead()
	defer endRead()
	c := a.automation(platform.AgentApp, false)
	var out []platform.AgentTurn
	for _, run := range a.runs(c, "running") {
		if a.busy[run.ID] {
			continue
		}
		x := platform.AgentTurn{Run: run}
		d := a.def(run.Agent)
		reader := a.reader(run)
		name := c.Setting(platform.SettingAgentModel)
		if run.ModelBinding != nil {
			name = run.ModelBinding.Model
		}

		switch {
		case run.Acts:
			x.Stop = "This legacy automatic run requires a new confirmed proposal"
		case d == nil:
			x.Stop = "the agent is no longer declared"
		case a.changedDefinition(run, d):
			x.Stop = "the agent definition changed; start a new run"
		case run.OnBehalf != "" && (reader.Tenant != a.tenant || reader.Roles[d.App] == ""):
			x.Stop = "the person no longer has access to this agent's app"
		case name == "" && len(run.Pending) == 0:
			x.Stop = "no model is set for agents"
		case len(run.Pending) == 0 && agents.SpentOut(d.Budget, run.StepsUsed, run.TokensUsed, run.Cost):
			x.Stop = agents.BudgetReason(d.Budget, run.StepsUsed, run.TokensUsed, run.Cost)
		case h.AgentSuspended("agent:" + run.Agent):
			sw, _ := h.Record(platform.SwitchType + "/agent:" + run.Agent)
			x.Stop = "suspended by " + sw.(platform.Switch).By
		default:
			if len(run.Pending) > 0 {
				break
			}
			model, pv, err := h.AgentModel(name)
			if err != nil {
				x.Stop = "the model " + name + " is not enabled"
				break
			}
			if run.ModelBinding == nil || run.ModelBinding.Stage != platform.AIStageRuntime {
				x.Stop = "the run has no trusted execution stage; start a new run"
				break
			}
			if !h.AgentProviderAvailable(pv.ID, now) {
				x.Stop = "The model provider failed repeatedly; human takeover is required"
				break
			}
			x.Model, x.Provider, x.Request = model, pv, a.prompt(c, d, run, name, now)
			x.Request.Run = run.ID
			x.Request.Call = fmt.Sprintf("agent:%s:%d", run.ID, run.StepsUsed)
			x.Request.RunTokens = d.Budget.Tokens - run.TokensUsed
			x.Request.Binding = run.ModelBinding
		}
		a.busy[run.ID] = true
		out = append(out, x)
	}
	return out
}

func (a *Agents) ApplyAgentStep(b platform.AgentResultStep, now time.Time, replaying bool) *kernel.Error {
	return a.ApplyAgentStepCaller(a.automation(platform.AgentApp, replaying), b, now)
}

func (a *Agents) ApplyAgentStepCaller(c platform.Caller, b platform.AgentResultStep, now time.Time) *kernel.Error {
	h := a.host
	if ev := b.Evaluation; ev != nil {
		key := ev.ID + ":report"
		if ev.State == "queued" {
			key = fmt.Sprintf("%s:checkpoint:%d", ev.ID, ev.Revision+1)
		}
		s := &pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: platform.AgentApp, IdempotencyKey: key,
			Target: &pb.EntityRef{Type: platform.EvaluationType, Id: ev.ID}, Schema: &pb.SchemaRef{Name: platform.SchemaRunStep, Version: 1}, Payload: []byte("{}")}
		_, err := a.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
			if _, known := platform.Get[platform.Evaluation](c, ev.ID); !known {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
			}
			return func(r *pb.ChangeRecord) { c.Put(r, *ev) }, nil
		})
		return err
	}
	run, known := platform.Get[platform.AgentRunRecord](c, b.Run)
	if !known {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	if !c.Staging() {
		h.ReplayAgentUsage(b.Usage)
	}
	s := &pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: platform.AgentApp, IdempotencyKey: fmt.Sprintf("%s:%d", run.ID, run.Revision+1),
		Target: &pb.EntityRef{Type: platform.RunType, Id: run.ID}, Schema: &pb.SchemaRef{Name: platform.SchemaRunStep, Version: 1}, Payload: []byte("{}")}
	_, err := a.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		return a.take(c, run, b, now), nil
	})
	return err
}

func (a *Agents) take(c platform.Caller, run platform.AgentRunRecord, b platform.AgentResultStep, now time.Time) func(*pb.ChangeRecord) {
	continuing := b.CallID != ""
	if len(b.Calls) > 0 {
		call := b.Calls[0]
		b.Tool, b.Arguments, b.CallID = call.Name, call.Arguments, call.ID
		run.Pending = slices.Clone(b.Calls[1:])
	} else if continuing {
		if len(run.Pending) == 0 || run.Pending[0].ID != b.CallID {
			return func(*pb.ChangeRecord) {}
		}
		run.Pending = slices.Clone(run.Pending[1:])
	}
	if len(b.Arguments) > 0 {
		if normalized, err := agents.NormalizeStepJSON(b.Arguments); err == nil {
			b.Arguments = normalized
		}
	}
	if len(b.Observation) > 0 {
		if normalized, err := agents.NormalizeStepJSON(b.Observation); err == nil {
			b.Observation = normalized
		}
	}
	if len(b.Protocol) > 0 {
		if run.Protocol == nil {
			run.Protocol = map[int]json.RawMessage{}
		}
		run.Protocol[len(run.Steps)] = b.Protocol
	}
	d := a.def(run.Agent)
	step := platform.RunStep{At: now, Tool: b.Tool, Arguments: string(b.Arguments), Tokens: b.Usage.Input + b.Usage.Output,
		Calls: b.Calls, CallID: b.CallID, Content: b.Content}
	if len(b.Calls) > 0 {
		step.Sources = run.SourceRefs()
	}
	if !continuing {
		run.StepsUsed++
	}
	run.TokensUsed += step.Tokens
	if b.Usage.CostReported {
		run.Cost += b.Usage.Cost
	}
	run.Model = cmp.Or(b.Usage.Model, run.Model)
	var args map[string]any
	json.Unmarshal(b.Arguments, &args)
	step.Rationale, _ = args["rationale"].(string)
	var then func(r *pb.ChangeRecord)
	stop := func(why string) {
		then = func(r *pb.ChangeRecord) { a.stop(c, r, &run, why, now) }
	}
	switch {
	case b.Stop != "":
		step.Outcome = "stopped: " + b.Stop
		run.StepsUsed--
		stop(b.Stop)
	case b.Failure != "":
		step.Outcome = "the model failed: " + b.Failure
		failures := 0
		for i := len(run.Steps) - 1; i >= 0 && strings.HasPrefix(run.Steps[i].Outcome, "the model failed"); i-- {
			failures++
		}
		if failures+1 >= modelFailures {
			stop(fmt.Sprintf("the model failed %d times", modelFailures))
		}
	case d == nil:
		step.Outcome = "the agent is no longer declared"
		stop(step.Outcome)
	case a.changedDefinition(run, d):
		step.Outcome = "the agent definition changed; start a new run"
		stop(step.Outcome)
	case run.StepsUsed > d.Budget.Steps || run.TokensUsed > d.Budget.Tokens || d.Budget.Cost > 0 && run.Cost > d.Budget.Cost:
		step.Outcome = "over budget"
		stop(agents.BudgetReason(d.Budget, run.StepsUsed, run.TokensUsed, run.Cost))
	case b.Tool == "":
		step.Outcome = "finished"
		run.Result, run.State = b.Content, "done"
	default:
		tool, ok := d.Toolset[b.Tool]
		if !ok {
			step.Outcome = "no tool " + b.Tool
			break
		}
		if tool.Kind == "knowledge" {
			var found []platform.Passage
			json.Unmarshal(b.Observation, &found)
			for _, p := range found {
				run.Citations = append(run.Citations, platform.Citation{Document: p.Document, Title: p.Title, Chunk: p.Chunk, Step: len(run.Steps)})
			}
			for _, p := range found {
				step.Sources = append(step.Sources, p.Document)
			}
			step.Outcome = cmp.Or(string(b.Observation), "[]")
			break
		}
		step.Outcome, then = a.use(c, d, &run, tool, args, b.Arguments, &step, now)
	}
	if step.CallID == "" {
		step.Outcome = agents.LegacyOutcome(step.Outcome, outcomeLimit)
	} else {
		step.Outcome = agents.Observation(step.Outcome, outcomeLimit)
	}
	run.Steps = append(run.Steps, step)
	return func(r *pb.ChangeRecord) {
		if then != nil {
			then(r)
		}
		if run.State == "done" || run.State == "stopped" {
			finishPendingCalls(&run, now)
		}
		if len(run.Proposals) > 0 && run.Proposals[len(run.Proposals)-1].Evidence == "" {
			run.Proposals[len(run.Proposals)-1].Evidence = platform.AgentApp + "/" + r.GetChangeId()
		}
		c.Put(r, run)
		if run.State == "done" {
			a.ended(c, r, run, now)
		}
	}
}

func finishPendingCalls(run *platform.AgentRunRecord, now time.Time) {
	sources := run.SourceRefs()
	for _, call := range run.Pending {
		run.Steps = append(run.Steps, platform.RunStep{At: now, Tool: call.Name, Arguments: string(call.Arguments), CallID: call.ID,
			Outcome: "not executed: the run " + run.State, Sources: sources})
	}
	run.Pending = nil
}

func (a *Agents) use(c platform.Caller, d *platform.CompiledAgent, run *platform.AgentRunRecord, tool platform.CompiledAgentTool, args map[string]any, raw json.RawMessage, step *platform.RunStep, now time.Time) (string, func(*pb.ChangeRecord)) {
	h := a.host
	str := func(k string) string { s, _ := args[k].(string); return s }
	switch tool.Kind {
	case "finish":
		run.Result, run.State = str("result"), "done"
		return "finished", nil
	case "ask":
		var answers []string
		if list, ok := args["answers"].([]any); ok {
			for _, x := range list {
				if s, ok := x.(string); ok {
					answers = append(answers, s)
				}
			}
		}
		to := a.recipients(c, d, *run)
		key := fmt.Sprintf("agent:%s:%d", run.ID, len(run.Steps)+1)
		run.State, run.Task = "waiting", platform.AgentApp+":"+key
		return "asked: " + str("question"), func(r *pb.ChangeRecord) {
			c.Assign(r, platform.Assignment{Title: str("question"), Body: fmt.Sprintf("Asked by the agent %s for: %s", run.Agent, run.Goal),
				Ref: platform.RunType + "/" + run.ID, To: to, Key: key, Answers: answers})
		}
	case "context":
		view, err := h.AgentContext(a.readsAs(*run), str("type"), str("id"), now)
		if err != nil {
			return "refused: " + err.Error(), nil
		}
		if step.CallID == "" {
			step.Sources = nil
		}
		step.Sources = append(step.Sources, str("type")+"/"+str("id"))
		step.Sources = append(step.Sources, view.Sources...)
		return string(view.Body), nil
	case "search":
		view := h.AgentSearch(a.readsAs(*run), str("query"), now)
		step.Sources = append(step.Sources, view.Sources...)
		return string(view.Body), nil
	case "query":
		reader := a.reader(*run)
		if reader == nil {
			return "refused: a query reads as a member", nil
		}
		view, err := h.AgentQuery(*reader, tool.Query, str("for"), now)
		if err != nil {
			return "refused: " + err.Error(), nil
		}
		step.Sources = append(step.Sources, view.Sources...)
		return string(view.Body), nil
	case "remember":
		about, _ := args["about_person"].(bool)
		return a.remember(c, run, str("fact"), about && run.OnBehalf != "", now)
	case "read":
		declared := args
		if inputs, ok := args["inputs"].(map[string]any); ok {
			declared = inputs
		}
		values := map[string]any{}
		for _, field := range tool.Inputs {
			if value, ok := declared[field.Name]; ok {
				values[field.Name] = value
			}
		}
		inputs, _ := json.Marshal(values)
		var view platform.AgentObservation
		var err *kernel.Error
		if m := a.reader(*run); m != nil {
			view, err = h.AgentRead(*m, tool.Schema, inputs)
		} else {
			var out any
			out, err = platform.CallRead(h.App(d.App), h.Automation(c, d.App), tool.Schema, inputs)
			view.Body, _ = json.Marshal(out)
			view.Sources = h.AgentSources(out)
		}
		if err != nil {
			return "refused: " + err.Error(), nil
		}
		if step.CallID == "" {
			step.Sources = nil
		}
		step.Sources = append(step.Sources, view.Sources...)
		step.Sources = append(step.Sources, "read:"+tool.Schema)
		return string(view.Body), nil
	}
	if c.Replaying {
		return a.replayLegacyAction(c, d, run, tool, args, raw, step, now)
	}
	if run.ActionsUsed >= d.Budget.Actions {
		return fmt.Sprintf("refused: its budget of %d actions is spent", d.Budget.Actions), nil
	}
	if tool.Kind == "effect" {
		payload, _ := json.Marshal(map[string]string{"message": str("message"), "run": run.ID, "agent": run.Agent})
		draft, refusal := a.toolDraft(c, *run, platform.Draft{Kind: "effect", Owner: d.App, Action: tool.Schema, Target: run.Ref, Payload: string(payload), Rationale: str("rationale"), Step: len(run.Steps)}, now)
		if refusal != nil {
			return "refused: " + refusal.Error(), nil
		}
		run.Draft, run.State = []platform.Draft{draft}, "waiting"
		run.Proposals = append(run.Proposals, draft)
		return "drafted for confirmation: " + tool.Schema, func(r *pb.ChangeRecord) { a.assignDraft(c, r, run, tool.Schema, now) }
	}
	target := str("target")
	body := agents.ActionPayload(raw, step.CallID != "")
	if d.Guard != nil {
		if err := d.Guard(h.Automation(c, d.App), a.run(*run), tool.Schema, target, body); err != nil {
			return "refused by its guard: " + err.Error(), nil
		}
	}
	draft, refusal := a.toolDraft(c, *run, platform.Draft{Kind: tool.Kind, Owner: d.App, Action: tool.Schema, Type: tool.Target, Target: target, Payload: string(body), Rationale: str("rationale"), Step: len(run.Steps)}, now)
	if refusal != nil {
		return "refused: " + refusal.Error(), nil
	}
	run.Draft, run.State = []platform.Draft{draft}, "waiting"
	run.Proposals = append(run.Proposals, draft)
	return "drafted for confirmation: " + tool.Schema + " " + target, func(r *pb.ChangeRecord) { a.assignDraft(c, r, run, tool.Tool.Description, now) }

}

func (a *Agents) reviewers(c platform.Caller, run platform.AgentRunRecord) platform.Caller {
	if d := a.def(run.Agent); d != nil && run.OnBehalf == "" && d.App != "" {
		return platform.RouteCaller(c, d.App)
	}
	return c
}

func (a *Agents) recipients(c platform.Caller, d *platform.CompiledAgent, run platform.AgentRunRecord) []platform.Recipient {
	if run.OnBehalf != "" {
		return []platform.Recipient{{Member: run.OnBehalf}}
	}
	if d != nil && d.To != nil {
		return d.To(a.host.Automation(c, d.App), a.run(run))
	}
	return nil
}

func (a *Agents) stop(c platform.Caller, r *pb.ChangeRecord, run *platform.AgentRunRecord, why string, now time.Time) {
	run.State, run.Stopped, run.Draft = "stopped", why, nil
	finishPendingCalls(run, now)
	if run.Task != "" {
		a.host.AgentTasks().Close(c, r, run.Task)
		run.Task = ""
	}
	if run.Flow != "" {
		a.ended(c, r, *run, now)
		return
	}
	recipients := a.recipients(c, a.def(run.Agent), *run)
	if len(recipients) == 0 {
		run.Handoff = "No eligible recipient is configured"
		return
	}
	key := "agent:" + run.ID + ":stopped"
	rc := a.reviewers(c, *run)
	people := rc.Notify(platform.Notification{Title: "Take over from the agent: " + run.Title, Ref: platform.RunType + "/" + run.ID, Key: key}, now, recipients...)
	people = slices.DeleteFunc(people, func(id string) bool {
		member, ok := a.host.Member(id)
		return !ok || member.Agent || member.Status == platform.MemberLeft || member.Status == platform.MemberSuspended
	})
	if len(people) == 0 {
		run.Handoff = "No eligible recipient is configured"
		return
	}
	if err := rc.Assign(r, platform.Assignment{Title: "Take over from the agent: " + run.Title, Body: "The agent " + run.Agent + " stopped: " + why + ".", Ref: platform.RunType + "/" + run.ID, To: recipients, Key: key}); err != nil {
		run.Handoff = err.Error()
		return
	}
	run.Task, run.Handoff = platform.AgentApp+":"+key, "assigned"

}

func (a *Agents) ended(c platform.Caller, r *pb.ChangeRecord, run platform.AgentRunRecord, now time.Time) {
	processes := a.host.AgentFlows()
	if run.Flow != "" && processes != nil {
		processes.RunEnded(c, platform.RunEnd{ID: run.ID, Agent: run.Agent, Flow: run.Flow, State: run.State, Result: run.Result, Stopped: run.Stopped, Token: run.Token}, now)
	}
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

func (a *Agents) replayLegacyAction(c platform.Caller, d *platform.CompiledAgent, run *platform.AgentRunRecord, tool platform.CompiledAgentTool, args map[string]any, raw json.RawMessage, step *platform.RunStep, now time.Time) (string, func(*pb.ChangeRecord)) {
	h := a.host
	str := func(name string) string { value, _ := args[name].(string); return value }
	if run.ActionsUsed >= d.Budget.Actions {
		return fmt.Sprintf("refused: its budget of %d actions is spent", d.Budget.Actions), nil
	}
	if tool.Kind == "effect" {
		key := fmt.Sprintf("%s:%d", run.ID, len(run.Steps)+1)
		sender := platform.ActingCaller(c, a.AgentPrincipal(run.Agent), d.App, true)
		endRun := h.ScopeAgentRun(run.ID)
		n, err := sender.Emit(tool.Schema, key, cmp.Or(run.Ref, platform.RunType+"/"+run.ID), map[string]string{"message": str("message"), "run": run.ID, "agent": run.Agent}, now)
		endRun()
		switch {
		case err != nil:
			return "refused: " + err.Error(), nil
		case n == 0:
			return "refused: no endpoint receives " + d.App + "/" + tool.Schema, nil
		}
		run.ActionsUsed++
		run.State, run.Task = "waiting", "effect:"+d.App+"/"+tool.Schema+":"+key
		return fmt.Sprintf("sent %s/%s to %d receivers; waiting for the answer", d.App, tool.Schema, n), nil
	}
	target := str("target")
	trackTarget := func() {
		if tool.Target == "" || target == "" {
			return
		}
		ref := tool.Target + "/" + target
		if _, exists := h.Record(ref); exists && !slices.Contains(step.Sources, ref) {
			step.Sources = append(step.Sources, ref)
		}
	}
	trackTarget()
	body := agents.ActionPayload(raw, step.CallID != "")
	if d.Guard != nil {
		if err := d.Guard(h.Automation(c, d.App), a.run(*run), tool.Schema, target, body); err != nil {
			return "refused by its guard: " + err.Error(), nil
		}
	}
	key := fmt.Sprintf("agent:%s:%d", run.ID, len(run.Steps)+1)
	agent := platform.AgentCaller(c, a.AgentPrincipal(run.Agent), d.App, run.ID)
	person := a.reader(*run)
	mustConfirm := slices.Contains(d.Checkpoints, tool.Schema)
	if tool.Kind == "action" {
		if declared, ok := h.App(d.App).Manifest().Actions.Action(tool.Schema); ok && declared.Approval != nil {
			mustConfirm = true
		}
	}
	if mustConfirm && person == nil {
		return "refused: this checkpoint waits for a person", nil
	}
	draft := func(typ string) (string, func(*pb.ChangeRecord)) {
		run.Draft = []platform.Draft{{Kind: tool.Kind, Action: tool.Schema, Target: target, Type: typ, Payload: string(body), Rationale: str("rationale"), Step: len(run.Steps)}}
		run.State = "waiting"
		title, _, _ := strings.Cut(tool.Tool.Description, ":")
		return "drafted for " + person.ID + " to confirm: " + tool.Schema + " " + target + " " + string(body), func(*pb.ChangeRecord) {
			c.Notify(platform.Notification{Title: "Confirm the agent's draft: " + title + " " + target, Body: str("rationale"), Ref: platform.RunType + "/" + run.ID,
				Key: key + ":draft"}, now, platform.Recipient{Member: person.ID})
		}
	}
	if tool.Kind == "protocol" {
		protocol, schema, _ := strings.Cut(tool.Schema, "#")
		if person != nil {
			owner, provided, ok := h.ProtocolProvider(tool.Schema)
			if !ok || !owner.Manifest().Actions.PermitsAny(person.RolesIn(owner.Manifest().ID), provided) {
				return "refused: " + person.ID + " may not " + schema + " at the provider", nil
			}
			if !run.Acts || mustConfirm {
				return draft("")
			}
		}
		endRun := h.ScopeAgentRun(run.ID)
		_, err := platform.Attempt(c, func() (*pb.ChangeRecord, *kernel.Error) {
			_, record, err := h.InvokeDecision(agent, protocol, schema, target, body, key, run.ID, now)
			return record, err
		})
		endRun()
		if err != nil {
			return "refused: " + err.Error(), nil
		}
		trackTarget()
		run.ActionsUsed++
		return "done: " + schema + " " + target, nil
	}
	app := h.App(d.App)
	s := &pb.Submission{TenantId: a.tenant, PrincipalId: agent.ID, Authority: a.authorityOf(tool.Target), IdempotencyKey: key,
		Target: &pb.EntityRef{Type: tool.Target, Id: target}, Schema: &pb.SchemaRef{Name: tool.Schema, Version: 1}, Payload: body, CorrelationId: run.ID}
	if person != nil {
		request := proto.Clone(s).(*pb.Submission)
		request.PrincipalId = person.ID
		asked := platform.ActingCaller(c, *person, app.Manifest().ID, false)
		err := platform.ProbeDecision(asked, func(probe platform.Caller) *kernel.Error {
			_, err := platform.Decide(probe, app, request, now)
			return err
		})
		if err != nil {
			return "refused: " + person.ID + " may not do this (" + err.Error() + ")", nil
		}
		if !run.Acts || mustConfirm {
			return draft(tool.Target)
		}
	}
	endRun := h.ScopeAgentRun(run.ID)
	record, err := platform.Attempt(c, func() (*pb.ChangeRecord, *kernel.Error) {
		return platform.Decide(agent, app, s, now)
	})
	endRun()
	if err != nil {
		return "refused: " + err.Error(), nil
	}
	trackTarget()
	run.ActionsUsed++
	return "done: " + tool.Schema + " " + target + " (" + record.GetChangeId() + ")", nil
}

const (
	evaluationRuns = 20
)

const (
	memoryDays   = 90
	proposalDays = 14
	memoriesUsed = 8
)

func (a *Agents) evaluationVersion(d *platform.CompiledAgent) string {
	cases := make([][]string, len(d.Cases))
	for i, c := range d.Cases {
		cases[i] = []string{c.Name, c.Goal, c.Ref}
	}
	digest, _ := platform.CanonicalDigest([]any{a.definitionVersion(d), cases})
	return digest
}

func (a *Agents) startEvaluation(c platform.Caller, id string, p struct {
	Agent, Model, Candidate string
	Suite                   bool
}) (func(*pb.ChangeRecord), *kernel.Error) {
	if _, known := platform.Get[platform.Evaluation](c, id); known {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	d := a.def(p.Agent)
	digest, config := "", ""
	if p.Candidate != "" {
		if !p.Suite || !c.Holds("build", "builder") {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "A builder and declared cases are required for candidate agent evaluation")
		}
		var err error
		d, digest, err = a.evaluationDefinition(p.Candidate, p.Agent)
		if err != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
		}
		model, provider, refusal := a.host.AgentModel(p.Model)
		if refusal != nil {
			return nil, refusal
		}
		binding, _, refusal := a.host.ResolveAgentModel(p.Model, d.ModelOptions, platform.AIStageDesign)
		if refusal != nil {
			return nil, refusal
		}
		config, err = a.host.AgentModelConfigurationDigest(model, provider, &binding)
		if err != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
		}
	}
	if d == nil || p.Model == "" {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if p.Suite && len(d.Cases) == 0 {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The agent {agent} declares no cases", p.Agent)
	}
	binding, _, refusal := a.host.ResolveAgentModel(p.Model, d.ModelOptions, platform.AIStageDesign)
	if refusal != nil {
		return nil, refusal
	}
	if config == "" {
		model, pv, _ := a.host.AgentModel(p.Model)
		config, _ = a.host.AgentModelConfigurationDigest(model, pv, &binding)
	}
	ev := platform.Evaluation{Binding: &binding, Record: platform.Record{ID: id}, Agent: p.Agent, Model: p.Model, State: "queued", Cases: []platform.EvalCase{}, Suite: p.Suite, Candidate: p.Candidate, DefinitionDigest: digest, ModelConfig: config}
	return func(r *pb.ChangeRecord) { a.host.Automation(c, platform.AgentApp).Put(r, ev) }, nil
}

func (a *Agents) AgentEvaluationRuntime() platform.AgentEvaluationRuntime {
	return platform.AgentEvaluationRuntime{Tenant: a.tenant, Host: a.host, Definition: a.def, Compile: a.evaluationDefinition, Version: a.evaluationVersion, Prompt: a.prompt, Principal: a.AgentPrincipal, Reader: a.reader, Automation: a.automation, View: a.run, Use: a.use, ObserveKnowledge: a.ObserveAgentKnowledge}
}
