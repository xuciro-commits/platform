package agents

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"google.golang.org/protobuf/proto"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/services/ai/evaluation"
	"slices"
	"strings"
	"time"
)

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
