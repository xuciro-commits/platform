package platformserver

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/ai"
	"platformserver/apps/work"
	"platformserver/internal/host"
	"platformserver/platform"
)

// stepBody is an agent entry: what the model chose at one step (ADR-0021 D3).
type stepBody struct {
	Run       string          `json:"run"`
	Tool      string          `json:"tool,omitempty"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
	Content   string          `json:"content,omitempty"` // text the model gave instead of, or beside, a tool call
	Usage     ai.Usage        `json:"usage"`
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
	model ai.Model
	pv    ai.Provider
	req   ChatRequest
	stop  string
}

// Think gives each running agent its next model turn (ADR-0021). The host
// calls it every second, apart from other owned work: model calls are slow and
// happen outside the tenant's lock; each answer is then journaled and applied.
func (t *Tenant) Think(now time.Time) { onLane(t.turns(now)) }

// turns are the agents' due turns, to run on the I/O lane (ADR-0027 D2): each
// calls its model outside the tenant's lock.
func (t *Tenant) turns(now time.Time) []func() {
	if t.agents == nil || t.quarantined() {
		return nil
	}
	var out []func()
	for _, x := range t.agents.due(now) {
		out = append(out, func() {
			if !t.quarantined() {
				t.take(x, now)
			}
		})
	}
	return out
}

// stillRunning reports whether this run is still the running turn that was
// snapshotted before its model call: a cancel, or a stop from any other cause,
// ends the run and the late answer must not become a step (ADR-0050 D3). It
// also clears the turn's busy mark, so a run stopped in flight is no longer
// held out of the scheduler.
func (t *Tenant) stillRunning(id string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	run, known := platform.Get[AgentRunRecord](t.automation(AgentApp, false), id)
	if !known || run.State != "running" {
		delete(t.agents.busy, id)
		return false
	}
	return true
}

// take takes one run's turn: its model's answer becomes the run's next step.
func (t *Tenant) take(x turn, now time.Time) {
	{
		if x.stop != "" {
			t.agentStep(stepBody{Run: x.run.ID, Stop: x.stop}, now)
			return
		}
		if x.run.OnBehalf != "" {
			reader := t.agents.reader(x.run)
			definition := t.agents.defs[x.run.Agent]
			if definition == nil || reader.Tenant != t.ID || reader.Roles[definition.app] == "" {
				t.agentStep(stepBody{Run: x.run.ID, Stop: "the person no longer has access to this agent's app"}, now)
				return
			}
		}
		answer, failure := t.call(x.pv, x.model, t.agents.member(x.run.Agent), x.req, now)
		if t.quarantined() {
			return // do not apply a late model answer to a tenant under recovery
		}
		// The call above is the one place a run waits on the outside, and the
		// tenant's lock is not held while it does: the run may have been
		// cancelled, stopped or suspended in the meantime. A late answer must
		// not run its tool then (ADR-0050 D3, review AI-02). What it cost is
		// still journaled and metered, so spend stays accounted for.
		if !t.stillRunning(x.run.ID) {
			t.meter(t.agents.member(x.run.Agent), answer.Usage)
			return
		}
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
		reader := a.reader(run)
		name := t.setting(c, SettingAgentModel)
		switch {
		case d == nil:
			x.stop = "the agent is no longer declared"
		case run.OnBehalf != "" && (reader.Tenant != t.ID || reader.Roles[d.app] == ""):
			x.stop = "the person no longer has access to this agent's app"
		case name == "":
			x.stop = "no model is set for agents"
		case d.spentOut(run):
			// A run at its budget is not given another model call: the reply
			// that crossed the budget already happened, and one more would
			// only spend more (ADR-0050 D6, review AI-05).
			x.stop = d.budgetReason(run)
		case t.suspended("agent:" + run.Agent): // the off switch (ADR-0029 D4)
			sw, _ := t.Held(SwitchType + "/agent:" + run.Agent)
			x.stop = "suspended by " + sw.(Switch).By
		default:
			model, pv, err := t.ai.Model(name)
			if err != nil {
				x.stop = "the model " + name + " is not enabled"
				break
			}
			if why := t.reserve(a.member(run.Agent), model, now); why != "" { // the door every call passes (ADR-0029 D1)
				x.stop = why
				break
			}
			if !t.breakers.allow("ai:"+pv.ID, now) {
				continue // its provider's breaker is open: the run waits
			}
			x.model, x.pv, x.req = model, pv, a.prompt(c, d, run, name, now)
			x.req.run = run.ID
		}
		a.busy[run.ID] = true
		out = append(out, x)
	}
	return out
}

// spentOut reports whether a run may not make another model call: what its
// budget allows is spent. Steps and tokens are counted as they happen; cost,
// only when the provider reports it (ADR-0050 D6, review AI-05).
func (d *agentDef) spentOut(run AgentRunRecord) bool {
	return run.StepsUsed >= d.Budget.Steps || d.Budget.Tokens > 0 && run.TokensUsed >= d.Budget.Tokens ||
		d.Budget.Cost > 0 && run.Cost >= d.Budget.Cost
}

// budgetReason says what a run ran out of, for the person who takes it over.
func (d *agentDef) budgetReason(run AgentRunRecord) string {
	reason := fmt.Sprintf("%d steps, %d tokens", run.StepsUsed, run.TokensUsed)
	if d.Budget.Cost > 0 {
		reason += fmt.Sprintf(", %.4f USD", run.Cost)
	}
	return "over its budget (" + reason + ")"
}

// prompt is the conversation so far: instructions, the goal with its record's
// context, then each step as the tool call it was and what came of it.
func (a *Agents) prompt(c platform.Caller, d *agentDef, run AgentRunRecord, model string, now time.Time) ChatRequest {
	goal := "Goal: " + run.Goal
	if run.OnBehalf != "" {
		goal += "\nOn behalf of: " + run.OnBehalf
	}
	// Historical Seen bytes remain immutable recovery evidence. A new model
	// call grounds itself through today's reader, including after ADR-0050
	// narrowed an old flow agent's scope or a member's grants changed.
	if typ, id, ok := strings.Cut(run.Ref, "/"); ok {
		if view, err := a.t.Context(a.readsAs(run), typ, id, now); err == nil {
			raw, _ := json.Marshal(view)
			goal += "\nAbout " + run.Ref + ":\n" + clip(string(raw), 6000)
		}
	}
	system := preamble + "\n\n" + d.Instructions
	if meaning := a.t.meaning(d.app); meaning != "" {
		system += "\n\nWhat the records of your app mean:\n" + meaning
	}
	if terms := a.t.glossary(d.app); terms != "" {
		system += "\n\nThis organisation's own words (its glossary; the records above keep their meaning):\n" + terms
	}
	if name := languageNames[run.Language]; name != "" {
		system += "\n\nWrite your rationale, questions, drafts' free text and result in " + name + ", the language of the person you work for; keep IDs, codes and field names as they are."
	}
	if ms := a.memories(c, run, now); len(ms) > 0 {
		system += "\n\nWhat you remember from earlier runs:"
		for _, m := range ms {
			system += "\n- " + m.Fact
		}
	}
	req := ChatRequest{Model: model, Messages: []Message{{Role: "system", Content: system}, {Role: "user", Content: goal}}}
	for i, s := range run.Steps {
		if s.Tool == "" {
			continue // a failed model call or a text-only answer
		}
		id := fmt.Sprintf("s%d", i+1)
		req.Messages = append(req.Messages, Message{Role: "assistant", Content: s.Rationale, ToolCalls: []ToolCall{{ID: id, Name: s.Tool, Arguments: json.RawMessage(cmp.Or(s.Arguments, "{}"))}}},
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

// reader is the person a run works for, or nil for an app's automation. It
// answers about authority: a removed person must not silently become
// automation, and an app's automation drafts nothing.
func (a *Agents) reader(run AgentRunRecord) *platform.Member {
	if run.OnBehalf != "" {
		if m, ok := a.t.member(run.OnBehalf); ok {
			return &m
		}
		return &platform.Member{ID: run.OnBehalf} // no tenant or grants: derived reads refuse it
	}
	return nil
}

// readsAs is who a run's reads happen as: the person it runs for, or its own
// app when no person started it (a flow's agent). An app's automation reads
// its own app's records only — never the host's view of every app, which the
// reads used to fall back to (ADR-0050 D2, review AI-01).
func (a *Agents) readsAs(run AgentRunRecord) *platform.Member {
	if m := a.reader(run); m != nil {
		return m
	}
	app := ""
	if d := a.defs[run.Agent]; d != nil {
		app = d.app
	}
	if app == "" {
		return &platform.Member{ID: "app:" + AgentApp, Tenant: a.t.ID} // no grants: nothing is readable
	}
	return a.t.appReader(app)
}

// agentStep journals a step, then applies it.
func (t *Tenant) agentStep(b stepBody, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.quarantined() {
		return
	}
	// The step's journal clock is a PostgreSQL timestamptz. Make the live
	// decision use its durable precision too; otherwise a full-journal retry
	// recreates a different run history from the same saved model step.
	if t.Record != nil {
		now = now.Truncate(time.Microsecond)
	}
	delete(t.agents.busy, b.Run)
	run, known := platform.Get[AgentRunRecord](t.automation(AgentApp, false), b.Run)
	if b.Evaluation != nil {
		// A report is applied only while its evaluation still waits for one;
		// an evaluation already reported must not be written twice.
		ev, waited := platform.Get[Evaluation](t.automation(AgentApp, false), b.Evaluation.ID)
		if !waited || ev.State != "queued" {
			return
		}
		run.Agent = b.Evaluation.Agent
	} else if !known || run.State != "running" {
		// The fence at the end of the outside call, and here in the step's own
		// decision: a model answer that arrives after the run was cancelled or
		// stopped never runs its tool (ADR-0050 D3, review AI-02).
		return
	}
	if b.Stop == "" && run.OnBehalf != "" {
		reader, definition := t.agents.reader(run), t.agents.defs[run.Agent]
		if definition == nil || reader.Tenant != t.ID || reader.Roles[definition.app] == "" {
			b.Stop = "the person no longer has access to this agent's app"
			b.Tool, b.Arguments, b.Observation = "", nil, nil
		}
	}
	body, _ := json.Marshal(b)
	t.record(t.agents, "agent", t.agents.member(run.Agent), body, now)
	t.agents.apply(b, now, false)
	t.enqueue(now)
}

func normalizedStepJSON(raw json.RawMessage) (json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("step JSON has trailing data")
	}
	return json.Marshal(value)
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
		t.ai.Meter(b.Usage)
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
	// PostgreSQL JSONB may reorder nested object keys differently from Go's
	// encoder. Normalize on both live application and replay, not only before
	// journaling, because these raw bytes become strings in the run history.
	if len(b.Arguments) > 0 {
		if normalized, err := normalizedStepJSON(b.Arguments); err == nil {
			b.Arguments = normalized
		}
	}
	if len(b.Observation) > 0 {
		if normalized, err := normalizedStepJSON(b.Observation); err == nil {
			b.Observation = normalized
		}
	}
	d := a.defs[run.Agent]
	step := RunStep{At: now, Tool: b.Tool, Arguments: string(b.Arguments), Tokens: b.Usage.Input + b.Usage.Output}
	run.StepsUsed++
	run.TokensUsed += step.Tokens
	if b.Usage.CostReported { // a cost nobody reported is not 0, it is unknown (ADR-0050 D7)
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
	case run.StepsUsed > d.Budget.Steps || run.TokensUsed > d.Budget.Tokens || d.Budget.Cost > 0 && run.Cost > d.Budget.Cost:
		// The reply that crossed the budget: what it cost is kept, and no
		// further call is made for this run (ADR-0050 D6, review AI-05).
		step.Outcome = "over budget"
		stop(d.budgetReason(run))
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
			for _, p := range found {
				step.Sources = append(step.Sources, p.Document)
			}
			step.Outcome = cmp.Or(string(b.Observation), "[]")
			break
		}
		step.Outcome, then = a.use(c, d, &run, tool, args, b.Arguments, &step, now)
	}
	step.Outcome = clip(step.Outcome, outcomeLimit)
	run.Steps = append(run.Steps, step)
	return func(r *pb.ChangeRecord) {
		if then != nil {
			then(r)
		}
		c.Put(r, run) // before its flow goes on, which may keep a signal on it
		if run.State == "done" {
			a.ended(c, r, run, now)
		}
	}
}

// use runs one tool; it may return what the decision does besides storing the run.
func (a *Agents) use(c platform.Caller, d *agentDef, run *AgentRunRecord, tool agentTool, args map[string]any, raw json.RawMessage, step *RunStep, now time.Time) (string, func(*pb.ChangeRecord)) {
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
		view, err := t.Context(a.readsAs(*run), str("type"), str("id"), now)
		if err != nil {
			return "refused: " + err.Error(), nil
		}
		step.Sources = append([]string{str("type") + "/" + str("id")}, t.refsIn(view)...)
		out, _ := json.Marshal(view)
		return string(out), nil
	case "search":
		found := t.Search(a.readsAs(*run), str("query"), now)
		for _, hit := range found {
			step.Sources = append(step.Sources, hit.Type+"/"+hit.ID)
		}
		out, _ := json.Marshal(found)
		return string(out), nil
	case "query":
		reader := a.reader(*run)
		if reader == nil {
			return "refused: a query reads as a member", nil
		}
		page, err := t.RunQuery(*reader, d.app, tool.schema, str("for"), now)
		if err != nil {
			return "refused: " + err.Error(), nil
		}
		q, _ := t.namedQuery(d.app, tool.schema)
		raw, _ := json.Marshal(page.Records)
		var ids []struct{ ID string }
		_ = json.Unmarshal(raw, &ids)
		for _, x := range ids {
			step.Sources = append(step.Sources, q.Object+"/"+x.ID)
		}
		out, _ := json.Marshal(page)
		return string(out), nil
	case "remember":
		about, _ := args["about_person"].(bool)
		return a.remember(c, run, str("fact"), about && run.OnBehalf != "", now)
	case "read":
		var out any
		var err *kernel.Error
		if m := a.reader(*run); m != nil {
			out, err = t.Read(*m, tool.schema)
		} else {
			out, err = t.app(d.app).Read(t.automated(c, d.app), tool.schema)
		}
		if err != nil {
			return "refused: " + err.Error(), nil
		}
		// The read's own authority is a source too: what it answers with is not
		// always records to check one by one (ADR-0033 D1).
		step.Sources = append(t.refsIn(out), "read:"+tool.schema)
		raw, _ := json.Marshal(out)
		return string(raw), nil
	}
	// An action: the declared tool, the person's own grants when it runs for one, the guard, the budget.
	if run.ActionsUsed >= d.Budget.Actions {
		return fmt.Sprintf("refused: its budget of %d actions is spent", d.Budget.Actions), nil
	}
	if tool.kind == "effect" { // sent to the endpoints bound to it, an external agent's answer awaited (ADR-0022 D7)
		key := fmt.Sprintf("%s:%d", run.ID, len(run.Steps)+1)
		sender := platform.ActingCaller(c, a.member(run.Agent), d.app, true)
		t.agentRun = run.ID
		n, err := sender.Emit(tool.schema, key, cmp.Or(run.Ref, RunType+"/"+run.ID), map[string]string{"message": str("message"), "run": run.ID, "agent": run.Agent}, now)
		t.agentRun = ""
		switch {
		case err != nil:
			return "refused: " + err.Error(), nil
		case n == 0:
			return "refused: no endpoint receives " + d.app + "/" + tool.schema, nil
		}
		run.ActionsUsed++
		run.State, run.Task = "waiting", "effect:"+d.app+"/"+tool.schema+":"+key
		return fmt.Sprintf("sent %s/%s to %d receivers; waiting for the answer", d.app, tool.schema, n), nil
	}
	target := str("target")
	if tool.target != "" && target != "" {
		step.Sources = append(step.Sources, tool.target+"/"+target)
	}
	var payload map[string]any
	json.Unmarshal(raw, &payload)
	delete(payload, "target")
	delete(payload, "rationale")
	body, _ := json.Marshal(payload)
	if d.Guard != nil {
		if err := d.Guard(t.automated(c, d.app), a.run(*run), tool.schema, target, body); err != nil {
			return "refused by its guard: " + err.Error(), nil
		}
	}
	key := fmt.Sprintf("agent:%s:%d", run.ID, len(run.Steps)+1)
	agent := platform.ActingCaller(c, a.member(run.Agent), d.app, true)
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
			if !run.Acts {
				return draft("")
			}
		}
		t.agentRun = run.ID // effects it causes name the run (discarded, a signal)
		_, _, err := t.invoke(agent, protocol, schema, target, body, key, run.ID, now)
		t.agentRun = ""
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
		if !run.Acts {
			return draft(tool.target)
		}
	}
	t.agentRun = run.ID
	record, err := platform.Decide(agent, app, s, now)
	t.agentRun = ""
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
		return d.To(a.t.automated(c, d.app), a.run(run))
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
	if run.Flow != "" && a.t.procs != nil {
		a.t.procs.RunEnded(c, host.RunEnd{ID: run.ID, Agent: run.Agent, Flow: run.Flow, State: run.State, Result: run.Result, Stopped: run.Stopped, Token: run.Token}, now)
	}
}

// Start, Signal and Finished serve flows' agent steps (internal/host.Runs).
func (a *Agents) Start(c platform.Caller, r *pb.ChangeRecord, s host.RunStart, now time.Time) {
	a.t.automated(c, AgentApp).Put(r, a.create(s.ID, s.Agent, s.Goal, s.Ref, "", s.Flow, s.Step, s.Token, now))
}

func (a *Agents) Signal(c platform.Caller, r *pb.ChangeRecord, run string, s host.RunSignal, now time.Time) {
	a.signal(c, r, run, Signal{At: s.At, Kind: s.Kind, By: s.By, Detail: s.Detail}, now)
}

func (a *Agents) Finished(c platform.Caller, flow, step string) []string {
	where := []any{[]any{"flow", "=", flow}, []any{"state", "=", "done"}}
	if step != "" {
		where = append(where, []any{"step", "=", step})
	}
	domain, _ := json.Marshal(where)
	runs, _, _ := platform.Find[AgentRunRecord](a.t.automated(c, AgentApp), platform.Query{Domain: domain, Sort: []string{"created", "id"}})
	var out []string
	for _, run := range runs {
		out = append(out, run.ID)
	}
	return out
}

// Interested and Listen take the events agents wait on (internal/host.Listener).
func (a *Agents) Interested(_ []string, e platform.Event) bool { return a.interested(e) }

func (a *Agents) Listen(c platform.Caller, e platform.Event, _ []string, now time.Time) *kernel.Error {
	return a.handle(c, e, now)
}

// interested: an answer to a run's question.
func (a *Agents) interested(e platform.Event) bool {
	s := e.Record.GetSubmission()
	return e.App == work.ID && s.GetSchema().GetName() == "work.task.complete" && strings.HasPrefix(s.GetTarget().GetId(), AgentApp+":")
}

// handle resumes the run a person answered.
func (a *Agents) handle(c platform.Caller, e platform.Event, now time.Time) *kernel.Error {
	id := e.Record.GetSubmission().GetTarget().GetId()
	for _, run := range a.runs(c, "waiting") {
		if run.Task != id {
			continue
		}
		task, _ := platform.Get[work.WorkTask](a.t.automated(c, work.ID), id)
		s := &pb.Submission{TenantId: a.t.ID, PrincipalId: c.ID, Authority: AgentApp, IdempotencyKey: fmt.Sprintf("%s:%d", run.ID, run.Revision+1),
			Target: &pb.EntityRef{Type: RunType, Id: run.ID}, Schema: &pb.SchemaRef{Name: SchemaRunStep, Version: 1}, Payload: []byte("{}")}
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

// effectEnded resumes the run that sent an effect, with what came of it: the
// receiver's answer, its refusal, or a person discarding it. r is the decision
// it happens in, or nil for one of its own.
func (a *Agents) effectEnded(c platform.Caller, r *pb.ChangeRecord, x platform.Effect, o platform.Outcome, now time.Time) {
	task := "effect:" + x.Event + ":" + x.Key
	for _, run := range a.runs(a.t.automated(c, AgentApp), "waiting") {
		if run.Task != task {
			continue
		}
		last := &run.Steps[len(run.Steps)-1]
		switch o.Result {
		case "delivered":
			last.Outcome += "\nanswered: " + clip(answerOf(o), outcomeLimit)
		case "discarded":
			last.Outcome += "\n" + o.Detail
		default:
			last.Outcome += "\nthe receiver " + x.State + " it: " + o.Detail
		}
		run.State, run.Task = "running", ""
		if r != nil {
			a.t.automated(c, AgentApp).Put(r, run)
			return
		}
		ac := a.t.automated(c, AgentApp)
		s := &pb.Submission{TenantId: a.t.ID, PrincipalId: ac.ID, Authority: AgentApp, IdempotencyKey: fmt.Sprintf("%s:%d", run.ID, run.Revision+1),
			Target: &pb.EntityRef{Type: RunType, Id: run.ID}, Schema: &pb.SchemaRef{Name: SchemaRunStep, Version: 1}, Payload: []byte("{}")}
		a.ledger.Receive(ac, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
			return func(r *pb.ChangeRecord) { ac.Put(r, run) }, nil
		})
		return
	}
}

// languageNames name the languages agents answer in (ADR-0023 D6).
var languageNames = map[string]string{"zh-CN": "Simplified Chinese (简体中文)", "zh-TW": "Traditional Chinese (繁體中文)", "ja": "Japanese", "de": "German", "fr": "French", "es": "Spanish"}
