package platformserver

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// stepBody is an agent entry: what the model chose at one step (ADR-0021 D3).
type stepBody struct {
	Run       string          `json:"run"`
	Tool      string          `json:"tool,omitempty"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
	Content   string          `json:"content,omitempty"` // text the model gave instead of, or beside, a tool call
	Usage     Usage           `json:"usage"`
	Failure   string          `json:"failure,omitempty"` // the model did not answer
	Stop      string          `json:"stop,omitempty"`    // the host stopped the run before calling a model
	// Observation is what a knowledge search found, made outside the lock like
	// the model's answer and applied from here on replay (ADR-0022 D4).
	Observation json.RawMessage `json:"observation,omitempty"`
	// Evaluation is a finished evaluation's report; Run is then its ID.
	Evaluation *Evaluation `json:"evaluation,omitempty"`
}

const preamble = `You are an agent of a business platform. You act only through the tools given: each action is a decision recorded with your name, and what cannot be undone waits for a person. Give a one-sentence rationale with every tool call. Read before you act; ask a person when you are unsure; end with finish and the result the goal asks for.`

type turn struct {
	run   AgentRunRecord
	model Model
	pv    Provider
	req   ChatRequest
	stop  string
}

// Think gives each running agent its next model turn (ADR-0021). The host
// calls it every second, apart from other owned work: model calls are slow and
// happen outside the tenant's lock; each answer is then journaled and applied.
func (t *Tenant) Think(now time.Time) {
	if t.agents == nil {
		return
	}
	for _, x := range t.agents.due(now) {
		if x.stop != "" {
			t.agentStep(stepBody{Run: x.run.ID, Stop: x.stop}, now)
			continue
		}
		answer, failure := t.call(x.pv, x.model, t.agents.member(x.run.Agent), x.req, now)
		body := stepBody{Run: x.run.ID, Content: answer.Content, Usage: answer.Usage}
		if failure != nil {
			body.Failure = failure.Detail
		} else if len(answer.ToolCalls) > 0 {
			body.Tool, body.Arguments = answer.ToolCalls[0].Name, answer.ToolCalls[0].Arguments // one step at a time
			if body.Tool == "knowledge" {
				body.Observation = t.agents.look(x.run, body.Arguments, now)
			}
		}
		t.agentStep(body, now)
	}
}

// look searches knowledge for a run, as whom it reads, outside the tenant's lock.
func (a *Agents) look(run AgentRunRecord, args json.RawMessage, now time.Time) json.RawMessage {
	var p struct{ Query string }
	json.Unmarshal(args, &p)
	app := ""
	if d := a.defs[run.Agent]; d != nil {
		app = d.app
	}
	found := a.t.Knowledge(a.reader(run), app, p.Query, 4, now)
	for i := range found {
		found[i].Text = clip(found[i].Text, 700)
	}
	raw, _ := json.Marshal(found)
	return raw
}

// due collects the running runs' next turns, under the tenant's lock.
func (a *Agents) due(now time.Time) []turn {
	t := a.t
	t.mu.Lock()
	defer t.mu.Unlock()
	c := t.automation(AgentApp, false)
	var out []turn
	for _, run := range a.runs(c, "running") {
		if a.busy[run.ID] {
			continue
		}
		x := turn{run: run}
		d := a.defs[run.Agent]
		name := t.setting(c, SettingAgentModel)
		daily, _ := strconv.Atoi(t.setting(c, SettingAgentDaily))
		switch {
		case d == nil:
			x.stop = "the agent is no longer declared"
		case name == "":
			x.stop = "no model is set for agents"
		case daily > 0 && t.ai != nil && t.ai.spent(a.member(run.Agent).ID, now) >= daily:
			x.stop = fmt.Sprintf("the agent used its %d tokens for today", daily)
		default:
			model, pv, err := t.ai.model(name)
			if err != nil {
				x.stop = "the model " + name + " is not enabled"
				break
			}
			x.model, x.pv, x.req = model, pv, a.prompt(c, d, run, name)
			x.req.run = run.ID
		}
		a.busy[run.ID] = true
		out = append(out, x)
	}
	return out
}

// prompt is the conversation so far: instructions, the goal with its record's
// context, then each step as the tool call it was and what came of it.
func (a *Agents) prompt(c platform.Caller, d *agentDef, run AgentRunRecord, model string) ChatRequest {
	goal := "Goal: " + run.Goal
	if run.OnBehalf != "" {
		goal += "\nOn behalf of: " + run.OnBehalf
	}
	if run.Seen != "" {
		goal += "\nAbout " + run.Ref + ":\n" + run.Seen
	}
	req := ChatRequest{Model: model, Messages: []Message{{Role: "system", Content: preamble + "\n\n" + d.Instructions}, {Role: "user", Content: goal}}}
	for i, s := range run.Steps {
		if s.Tool == "" {
			continue // a failed model call or a text-only answer
		}
		id := fmt.Sprintf("s%d", i+1)
		req.Messages = append(req.Messages, Message{Role: "assistant", Content: s.Rationale, ToolCalls: []ToolCall{{ID: id, Name: s.Tool, Arguments: json.RawMessage(cmpOr(s.Arguments, "{}"))}}},
			Message{Role: "tool", ToolCallID: id, Content: s.Outcome})
	}
	for _, name := range slices.Sorted(func(yield func(string) bool) {
		for n := range d.tools {
			if !yield(n) {
				return
			}
		}
	}) {
		req.Tools = append(req.Tools, d.tools[name].tool)
	}
	return req
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// reader is who an agent reads as: the person it runs for, or the tenant's
// records as the app's automation (a flow's agent reads what its app may).
func (a *Agents) reader(run AgentRunRecord) *platform.Member {
	if run.OnBehalf != "" {
		if m, ok := a.t.member(run.OnBehalf); ok {
			return &m
		}
	}
	return nil
}

// agentStep journals a step, then applies it.
func (t *Tenant) agentStep(b stepBody, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.agents.busy, b.Run)
	run, _ := platform.Get[AgentRunRecord](t.automation(AgentApp, false), b.Run)
	if b.Evaluation != nil {
		run.Agent = b.Evaluation.Agent
	}
	body, _ := json.Marshal(b)
	t.record(t.agents, "agent", t.agents.member(run.Agent), body, now)
	t.agents.apply(b, now, false)
	t.enqueue(now)
}

// apply takes one journaled step as a decision of the agent app.
func (a *Agents) apply(b stepBody, now time.Time, replaying bool) *kernel.Error {
	t := a.t
	c := t.automation(AgentApp, replaying)
	if ev := b.Evaluation; ev != nil {
		s := &pb.Submission{TenantId: t.ID, PrincipalId: c.ID, Authority: AgentApp, IdempotencyKey: ev.ID + ":report",
			Target: &pb.EntityRef{Type: EvaluationType, Id: ev.ID}, Schema: &pb.SchemaRef{Name: SchemaRunStep, Version: 1}, Payload: []byte("{}")}
		_, err := a.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
			if _, known := platform.Get[Evaluation](c, ev.ID); !known {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
			}
			return func(r *pb.ChangeRecord) { c.Put(r, *ev) }, nil
		})
		return err
	}
	run, known := platform.Get[AgentRunRecord](c, b.Run)
	if !known {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	if t.ai != nil && b.Usage.Model != "" {
		t.ai.meter(b.Usage)
	}
	s := &pb.Submission{TenantId: t.ID, PrincipalId: c.ID, Authority: AgentApp, IdempotencyKey: fmt.Sprintf("%s:%d", run.ID, run.Revision+1),
		Target: &pb.EntityRef{Type: RunType, Id: run.ID}, Schema: &pb.SchemaRef{Name: SchemaRunStep, Version: 1}, Payload: []byte("{}")}
	_, err := a.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		return a.take(c, run, b, now), nil
	})
	return err
}

// take runs the step's tool and returns how the run changes.
func (a *Agents) take(c platform.Caller, run AgentRunRecord, b stepBody, now time.Time) func(*pb.ChangeRecord) {
	d := a.defs[run.Agent]
	step := RunStep{At: now, Tool: b.Tool, Arguments: string(b.Arguments), Tokens: b.Usage.Input + b.Usage.Output}
	run.StepsUsed++
	run.TokensUsed += step.Tokens
	run.Cost += b.Usage.Cost
	run.Model = cmpOr(b.Usage.Model, run.Model)
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
	case run.StepsUsed > d.Budget.Steps || run.TokensUsed > d.Budget.Tokens:
		step.Outcome = "over budget"
		stop(fmt.Sprintf("over its budget (%d steps, %d tokens)", run.StepsUsed, run.TokensUsed))
	case b.Tool == "": // a text answer ends the run with it
		step.Outcome = "finished"
		run.Result, run.State = b.Content, "done"
	default:
		tool, ok := d.tools[b.Tool]
		if !ok {
			step.Outcome = "no tool " + b.Tool
			break
		}
		if tool.kind == "knowledge" { // found outside the lock, journaled with the step
			var found []Passage
			json.Unmarshal(b.Observation, &found)
			for _, p := range found {
				run.Citations = append(run.Citations, Citation{Document: p.Document, Title: p.Title, Chunk: p.Chunk, Step: len(run.Steps)})
			}
			step.Outcome = cmpOr(string(b.Observation), "[]")
			break
		}
		step.Outcome, then = a.use(c, d, &run, tool, args, b.Arguments, now)
	}
	step.Outcome = clip(step.Outcome, outcomeLimit)
	run.Steps = append(run.Steps, step)
	return func(r *pb.ChangeRecord) {
		if then != nil {
			then(r)
		}
		if run.State == "done" {
			a.ended(c, r, run, now)
		}
		c.Put(r, run)
	}
}

// use runs one tool; it may return what the decision does besides storing the run.
func (a *Agents) use(c platform.Caller, d *agentDef, run *AgentRunRecord, tool agentTool, args map[string]any, raw json.RawMessage, now time.Time) (string, func(*pb.ChangeRecord)) {
	t := a.t
	str := func(k string) string { s, _ := args[k].(string); return s }
	switch tool.kind {
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
		run.State, run.Task = "waiting", AgentApp+":"+key
		return "asked: " + str("question"), func(r *pb.ChangeRecord) {
			c.Assign(r, platform.Assignment{Title: str("question"), Body: fmt.Sprintf("Asked by the agent %s for: %s", run.Agent, run.Goal),
				Ref: RunType + "/" + run.ID, To: to, Key: key, Answers: answers})
		}
	case "context":
		view, err := t.Context(a.reader(*run), str("type"), str("id"), now)
		if err != nil {
			return "refused: " + err.Error(), nil
		}
		out, _ := json.Marshal(view)
		return string(out), nil
	case "search":
		out, _ := json.Marshal(t.Search(a.reader(*run), str("query"), now))
		return string(out), nil
	case "read":
		var out any
		var err *kernel.Error
		if m := a.reader(*run); m != nil {
			out, err = t.Read(*m, tool.schema)
		} else {
			out, err = t.app(d.app).Read(t.automation(d.app, c.Replaying), tool.schema)
		}
		if err != nil {
			return "refused: " + err.Error(), nil
		}
		raw, _ := json.Marshal(out)
		return string(raw), nil
	}
	// An action: the declared tool, the person's own grants when it runs for one, the guard, the budget.
	if run.ActionsUsed >= d.Budget.Actions {
		return fmt.Sprintf("refused: its budget of %d actions is spent", d.Budget.Actions), nil
	}
	target := str("target")
	var payload map[string]any
	json.Unmarshal(raw, &payload)
	delete(payload, "target")
	delete(payload, "rationale")
	body, _ := json.Marshal(payload)
	if d.Guard != nil {
		if err := d.Guard(t.automation(d.app, c.Replaying), a.run(*run), tool.schema, target, body); err != nil {
			return "refused by its guard: " + err.Error(), nil
		}
	}
	key := fmt.Sprintf("agent:%s:%d", run.ID, len(run.Steps)+1)
	agent := platform.NewCaller(runtime{t}, a.member(run.Agent), d.app, c.Replaying, true)
	person := a.reader(*run)
	// For a person, it drafts and the person confirms (D6); a flow's agent acts itself.
	draft := func(typ string) (string, func(*pb.ChangeRecord)) {
		run.Draft = []Draft{{Kind: tool.kind, Action: tool.schema, Target: target, Type: typ, Payload: string(body), Rationale: str("rationale"), Step: len(run.Steps)}}
		run.State = "waiting"
		title, _, _ := strings.Cut(tool.tool.Description, ":")
		return "drafted for " + person.ID + " to confirm: " + tool.schema + " " + target + " " + string(body), func(*pb.ChangeRecord) {
			c.Notify(platform.Notification{Title: "Confirm the agent's draft: " + title + " " + target, Body: str("rationale"), Ref: RunType + "/" + run.ID,
				Key: key + ":draft"}, now, platform.Recipient{Member: person.ID})
		}
	}
	if tool.kind == "protocol" {
		protocol, schema, _ := strings.Cut(tool.schema, "#")
		if person != nil {
			owner, provided, ok := t.provider(tool.schema)
			if !ok || !owner.Manifest().Actions.Permits(person.Roles[owner.Manifest().ID], provided) {
				return "refused: " + person.ID + " may not " + schema + " at the provider", nil
			}
			return draft("")
		}
		_, _, err := agent.Invoke(protocol, schema, target, body, key, run.ID, now)
		if err != nil {
			return "refused: " + err.Error(), nil
		}
		run.ActionsUsed++
		return "done: " + schema + " " + target, nil
	}
	app := t.app(d.app)
	s := &pb.Submission{TenantId: t.ID, PrincipalId: agent.ID, Authority: t.authorityOf(tool.target), IdempotencyKey: key,
		Target: &pb.EntityRef{Type: tool.target, Id: target}, Schema: &pb.SchemaRef{Name: tool.schema, Version: 1}, Payload: body, CorrelationId: run.ID}
	if person != nil { // D2: never more than the person it runs for
		t.probing = true
		_, err := app.Submit(t.caller(*person, app, c.Replaying), s, now)
		t.probing = false
		if err != nil {
			return "refused: " + person.ID + " may not do this (" + err.Error() + ")", nil
		}
		return draft(tool.target)
	}
	record, err := app.Submit(agent, s, now)
	if err != nil {
		return "refused: " + err.Error(), nil
	}
	run.ActionsUsed++
	return "done: " + tool.schema + " " + target + " (" + record.GetChangeId() + ")", nil
}

func (a *Agents) recipients(c platform.Caller, d *agentDef, run AgentRunRecord) []platform.Recipient {
	if run.OnBehalf != "" {
		return []platform.Recipient{{Member: run.OnBehalf}}
	}
	if d != nil && d.To != nil {
		return d.To(a.t.automation(d.app, c.Replaying), a.run(run))
	}
	return nil
}

// stop ends a run that cannot go on: a flow's run goes back to its flow; any
// other goes to a person as a task (ADR-0021 D6).
func (a *Agents) stop(c platform.Caller, r *pb.ChangeRecord, run *AgentRunRecord, why string, now time.Time) {
	run.State, run.Stopped, run.Draft = "stopped", why, nil
	if run.Task != "" {
		a.t.closeTask(c, r, run.Task)
		run.Task = ""
	}
	if run.Flow != "" {
		a.ended(c, r, *run, now)
		return
	}
	c.Assign(r, platform.Assignment{Title: "Take over from the agent: " + run.Title, Body: "The agent " + run.Agent + " stopped: " + why + ".",
		Ref: RunType + "/" + run.ID, To: a.recipients(c, a.defs[run.Agent], *run), Key: "agent:" + run.ID + ":stopped"})
}

// ended hands a flow's run back to its flow.
func (a *Agents) ended(c platform.Caller, r *pb.ChangeRecord, run AgentRunRecord, now time.Time) {
	if run.Flow != "" && a.t.flows != nil {
		a.t.flows.agentEnded(c, run, now)
	}
}

// interested: an answer to a run's question.
func (a *Agents) interested(e platform.Event) bool {
	s := e.Record.GetSubmission()
	return e.App == WorkApp && s.GetSchema().GetName() == "work.task.complete" && strings.HasPrefix(s.GetTarget().GetId(), AgentApp+":")
}

// handle resumes the run a person answered.
func (a *Agents) handle(c platform.Caller, e platform.Event, now time.Time) *kernel.Error {
	id := e.Record.GetSubmission().GetTarget().GetId()
	for _, run := range a.runs(c, "waiting") {
		if run.Task != id {
			continue
		}
		task, _ := platform.Get[WorkTask](a.t.automation(WorkApp, c.Replaying), id)
		s := &pb.Submission{TenantId: a.t.ID, PrincipalId: c.ID, Authority: AgentApp, IdempotencyKey: fmt.Sprintf("%s:%d", run.ID, run.Revision+1),
			Target: &pb.EntityRef{Type: RunType, Id: run.ID}, Schema: &pb.SchemaRef{Name: SchemaRunStep, Version: 1}, Payload: []byte("{}")}
		_, err := a.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
			last := &run.Steps[len(run.Steps)-1]
			last.Outcome += fmt.Sprintf("\nanswered by %s: %s", task.Assignee, cmpOr(task.Answer, "done"))
			run.State, run.Task = "running", ""
			return func(r *pb.ChangeRecord) { c.Put(r, run) }, nil
		})
		return err
	}
	return nil
}
