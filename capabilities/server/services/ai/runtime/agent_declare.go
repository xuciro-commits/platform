package agents

import (
	"cmp"
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/services/ai/evaluation"
	"slices"
	"strings"
	"time"
)

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
				Properties: map[string]any{"type": map[string]any{"type": "string", "description": "The entity type, like build.erpitem"}, "id": map[string]any{"type": "string"}, "rationale": rationale()}, Required: []string{"type", "id", "rationale"}}},
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
		}); refusal != nil && !(draft.Kind == "function" && platform.AwaitsDecision(refusal)) {
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
