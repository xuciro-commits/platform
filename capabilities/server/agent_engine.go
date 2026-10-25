package platformserver

import (
	"cmp"
	"encoding/json"
	"fmt"
	"platformserver/internal/agents"
	"slices"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/host"
	"platformserver/platform"
)

// stepBody is an agent entry: what the model chose at one step.
type stepBody struct {
	Run       string           `json:"run"`
	Calls     []ToolCall       `json:"calls,omitempty"`  // complete new model response; Tool remains the legacy single-call encoding
	CallID    string           `json:"callId,omitempty"` // continuing an already accepted response
	Tool      string           `json:"tool,omitempty"`
	Arguments json.RawMessage  `json:"arguments,omitempty"`
	Content   string           `json:"content,omitempty"` // text the model gave instead of, or beside, a tool call
	Usage     platform.AIUsage `json:"usage"`
	Failure   string           `json:"failure,omitempty"` // the model did not answer
	Stop      string           `json:"stop,omitempty"`    // the host stopped the run before calling a model
	// Observation is what a knowledge search found, made outside the lock like
	// the model's answer and applied from here on replay.
	Observation json.RawMessage `json:"observation,omitempty"`
	// Evaluation is a checkpoint or final report; Run is then its ID.
	Evaluation *Evaluation `json:"evaluation,omitempty"`
}

type AgentTurn struct {
	Run      AgentRunRecord
	Model    platform.AIModel
	Provider platform.AIProvider
	Request  ChatRequest
	Stop     string
}
type turn = AgentTurn

// Think gives each running agent its next model turn. The host
// wakes it with owned work, with a periodic fallback: model calls are slow and
// happen outside the tenant's lock; each answer is then journaled and applied.
func (t *Tenant) Think(now time.Time) { onLane(t.turns(now)) }

// turns are the agents' due turns, to run on the I/O lane: each
// calls its model outside the tenant's lock.
func (t *Tenant) turns(now time.Time) []func() {
	if t.agents == nil || t.quarantined() {
		return nil
	}
	var out []func()
	for _, x := range t.agents.DueAgentTurns(now) {
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
// ends the run and the late answer must not become a step. It
// also clears the turn's busy mark, so a run stopped in flight is no longer
// held out of the scheduler.
func (t *Tenant) stillRunning(id string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	run, known := platform.Get[AgentRunRecord](t.automation(AgentApp, false), id)
	if !known || run.State != "running" {
		t.agents.IdleAgentTurn(id)
		return false
	}
	return true
}

// take takes one run's turn: its model's answer becomes the run's next step.
func (t *Tenant) take(x turn, now time.Time) {
	{
		if x.Stop != "" {
			t.agentStep(stepBody{Run: x.Run.ID, Stop: x.Stop}, now)
			return
		}
		if x.Run.OnBehalf != "" {
			if !t.agents.AllowsAgentRun(x.Run) {
				t.agentStep(stepBody{Run: x.Run.ID, Stop: "the person no longer has access to this agent's app"}, now)
				return
			}
		}
		if len(x.Run.Pending) > 0 {
			t.continueAgentCalls(x.Run, now)
			return
		}
		answer, failure := t.call(x.Provider, x.Model, t.agents.AgentPrincipal(x.Run.Agent), x.Request, now)
		answer.Usage = agentRunUsage(answer.Usage, x.Run)
		if t.quarantined() {
			return // do not apply a late model answer to a tenant under recovery
		}
		// The call above is the one place a run waits on the outside, and the
		// tenant's lock is not held while it does: the run may have been
		// cancelled, stopped or suspended in the meantime. A late answer must
		// not run its tool then. What it cost is
		// still journaled and metered, so spend stays accounted for.
		if !t.stillRunning(x.Run.ID) {
			t.meter(t.agents.AgentPrincipal(x.Run.Agent), answer.Usage)
			return
		}
		body := stepBody{Run: x.Run.ID, Content: answer.Content, Usage: answer.Usage}
		if failure != nil {
			body.Failure = failure.Detail
		} else if len(answer.ToolCalls) > 0 {
			var err error
			body.Calls, err = agents.CompleteToolCalls(answer.ToolCalls, x.Run.ID, x.Run.StepsUsed+1)
			if err != nil {
				body.Failure = err.Error()
			} else if body.Calls[0].Name == "knowledge" {
				body.Observation = t.agents.ObserveAgentKnowledge(x.Run, body.Calls[0].Arguments, now)
			}
		}
		t.agentStep(body, now)
	}
}

// Consecutive knowledge reads can wait on providers together. Their observed
// values are still accepted in call order, one result at a time. Writes and
// human checkpoints never join a parallel group.
func (t *Tenant) continueAgentCalls(run AgentRunRecord, now time.Time) {
	defer func() {
		t.mu.Lock()
		t.agents.IdleAgentTurn(run.ID)
		t.mu.Unlock()
		wakeWork()
	}()
	calls := run.Pending[:1]
	if calls[0].Name == "knowledge" {
		for len(calls) < len(run.Pending) && run.Pending[len(calls)].Name == "knowledge" {
			calls = run.Pending[:len(calls)+1]
		}
	}
	observations := make([]json.RawMessage, len(calls))
	if calls[0].Name == "knowledge" {
		jobs := make(chan int, len(calls))
		for i := range calls {
			jobs <- i
		}
		close(jobs)
		var workers sync.WaitGroup
		for range min(4, len(calls)) {
			workers.Go(func() {
				for i := range jobs {
					observations[i] = t.agents.ObserveAgentKnowledge(run, calls[i].Arguments, now)
				}
			})
		}
		workers.Wait()
	}
	for i, call := range calls {
		if !t.applyAgentStep(stepBody{Run: run.ID, CallID: call.ID, Tool: call.Name, Arguments: call.Arguments,
			Observation: observations[i]}, now, false) {
			break
		}
	}
}

// look searches knowledge for a run, as whom it reads, outside the tenant's lock.
func (a *Agents) ObserveAgentKnowledge(run AgentRunRecord, args json.RawMessage, now time.Time) json.RawMessage {
	var p struct{ Query string }
	json.Unmarshal(args, &p)
	app := ""
	if d := a.def(run.Agent); d != nil {
		app = d.app
	}
	return a.host.AgentKnowledge(a.reader(run), app, p.Query, 4, now)
}

// due collects the running runs' next turns, under the tenant's lock.
func (a *Agents) DueAgentTurns(now time.Time) []AgentTurn {
	h := a.host
	endRead := h.BeginAgentRead()
	defer endRead()
	c := a.automation(AgentApp, false)
	var out []AgentTurn
	for _, run := range a.runs(c, "running") {
		if a.busy[run.ID] {
			continue
		}
		x := AgentTurn{Run: run}
		d := a.def(run.Agent)
		reader := a.reader(run)
		name := c.Setting(SettingAgentModel)
		switch {
		case d == nil:
			x.Stop = "the agent is no longer declared"
		case a.changedDefinition(run, d):
			x.Stop = "the agent definition changed; start a new run"
		case run.OnBehalf != "" && (reader.Tenant != a.tenant || reader.Roles[d.app] == ""):
			x.Stop = "the person no longer has access to this agent's app"
		case name == "" && len(run.Pending) == 0:
			x.Stop = "no model is set for agents"
		case len(run.Pending) == 0 && agents.SpentOut(d.Budget, run.StepsUsed, run.TokensUsed, run.Cost):
			// A run at its budget is not given another model call: the reply
			// that crossed the budget already happened, and one more would
			// only spend more.
			x.Stop = agents.BudgetReason(d.Budget, run.StepsUsed, run.TokensUsed, run.Cost)
		case h.AgentSuspended("agent:" + run.Agent): // the off switch
			sw, _ := h.Record(SwitchType + "/agent:" + run.Agent)
			x.Stop = "suspended by " + sw.(Switch).By
		default:
			if len(run.Pending) > 0 {
				break // accepted tool calls do not reserve another model call or spend a model turn
			}
			model, pv, err := h.AgentModel(name)
			if err != nil {
				x.Stop = "the model " + name + " is not enabled"
				break
			}
			if why := h.ReserveAgentModel(a.AgentPrincipal(run.Agent), model, now); why != "" { // the door every call passes
				x.Stop = why
				break
			}
			if !h.AgentProviderAvailable(pv.ID, now) {
				continue // its provider's breaker is open: the run waits
			}
			x.Model, x.Provider, x.Request = model, pv, a.prompt(c, d, run, name, now)
			x.Request.Run = run.ID
		}
		a.busy[run.ID] = true
		out = append(out, x)
	}
	return out
}

// prompt gathers observations through the run's current reader. Conversation
// shaping never receives a reader or any host callback.
func (a *Agents) prompt(c platform.Caller, d *agentDef, run AgentRunRecord, model string, now time.Time) ChatRequest {
	input := agents.Prompt{Goal: run.Goal, OnBehalf: run.OnBehalf, Ref: run.Ref,
		Instructions: d.Instructions, Meaning: a.host.AgentMeaning(d.app),
		Glossary: a.host.AgentGlossary(d.app), Language: a.host.AgentLanguage(run.Language)}
	// Historical Seen bytes remain immutable recovery evidence. New model calls
	// use today's reader, including after a person's grants changed.
	if typ, id, ok := strings.Cut(run.Ref, "/"); ok {
		if view, err := a.host.AgentContext(a.readsAs(run), typ, id, now); err == nil {
			input.Context = string(view.Body)
			input.HasContext = true
		}
	}
	for _, memory := range a.memories(c, run, now) {
		input.Memories = append(input.Memories, memory.Fact)
	}
	for _, step := range run.Steps {
		input.History = append(input.History, agents.HistoryStep{Calls: step.Calls, Content: step.Content,
			Tool: step.Tool, CallID: step.CallID, Outcome: step.Outcome, Rationale: step.Rationale, Arguments: step.Arguments})
	}
	input.Tools = make(map[string]platform.Tool, len(d.tools))
	for name, tool := range d.tools {
		input.Tools[name] = tool.tool
	}
	return input.Request(model)
}

// reader is the person a run works for, or nil for an app's automation. It
// answers about authority: a removed person must not silently become
// automation, and an app's automation drafts nothing.
func (a *Agents) reader(run AgentRunRecord) *platform.Member {
	if run.OnBehalf != "" {
		if m, ok := a.host.Member(run.OnBehalf); ok {
			return &m
		}
		return &platform.Member{ID: run.OnBehalf} // no tenant or grants: derived reads refuse it
	}
	return nil
}

// readsAs is who a run's reads happen as: the person it runs for, or its own
// app when no person started it (a flow's agent). An app's automation reads
// its own app's records only — never the host's view of every app, which the
// reads used to fall back to.
func (a *Agents) readsAs(run AgentRunRecord) *platform.Member {
	if m := a.reader(run); m != nil {
		return m
	}
	app := ""
	if d := a.def(run.Agent); d != nil {
		app = d.app
	}
	if app == "" {
		return &platform.Member{ID: "app:" + AgentApp, Tenant: a.tenant} // no grants: nothing is readable
	}
	return a.host.AgentAppReader(app)
}

// agentStep journals a step, then applies it.
func (t *Tenant) agentStep(b stepBody, now time.Time) {
	t.applyAgentStep(b, now, true)
}

func (t *Tenant) applyAgentStep(b stepBody, now time.Time, releaseTurn bool) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.quarantined() {
		return false
	}
	// The step's journal clock is a PostgreSQL timestamptz. Make the live
	// decision use its durable precision too; otherwise a full-journal retry
	// recreates a different run history from the same saved model step.
	if t.recordsAcceptedResults() {
		now = now.Truncate(time.Microsecond)
	}
	if releaseTurn {
		t.agents.IdleAgentTurn(b.Run)
	}
	run, known := platform.Get[AgentRunRecord](t.automation(AgentApp, false), b.Run)
	if b.Evaluation != nil {
		// A report is applied only while its evaluation still waits for one;
		// an evaluation already reported must not be written twice.
		ev, waited := platform.Get[Evaluation](t.automation(AgentApp, false), b.Evaluation.ID)
		if !waited || ev.State != "queued" || ev.Revision != b.Evaluation.Revision {
			return false
		}
		run.Agent = b.Evaluation.Agent
	} else if !known || run.State != "running" {
		// The fence at the end of the outside call, and here in the step's own
		// decision: a model answer that arrives after the run was cancelled or
		// stopped never runs its tool.
		return false
	}
	if b.CallID != "" && (len(run.Pending) == 0 || run.Pending[0].ID != b.CallID) {
		return false // a completed call's durable result is reused after restart or a late response
	}
	if b.Stop == "" && run.OnBehalf != "" {
		if !t.agents.AllowsAgentRun(run) {
			b.Stop = "the person no longer has access to this agent's app"
			b.Tool, b.Arguments, b.Observation, b.Calls, b.CallID = "", nil, nil, nil, ""
		}
	}
	t.acceptAgentStep(b, now)
	return !t.quarantined()
}

// apply takes one journaled step as a decision of the agent app.
func (a *Agents) ApplyAgentStep(b stepBody, now time.Time, replaying bool) *kernel.Error {
	return a.ApplyAgentStepCaller(a.automation(AgentApp, replaying), b, now)
}

func (a *Agents) ApplyAgentStepCaller(c platform.Caller, b stepBody, now time.Time) *kernel.Error {
	h := a.host
	if ev := b.Evaluation; ev != nil {
		key := ev.ID + ":report"
		if ev.State == "queued" {
			key = fmt.Sprintf("%s:checkpoint:%d", ev.ID, ev.Revision+1)
		}
		s := &pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: AgentApp, IdempotencyKey: key,
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
	if !c.Staging() {
		h.ReplayAgentUsage(b.Usage)
	}
	s := &pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: AgentApp, IdempotencyKey: fmt.Sprintf("%s:%d", run.ID, run.Revision+1),
		Target: &pb.EntityRef{Type: RunType, Id: run.ID}, Schema: &pb.SchemaRef{Name: SchemaRunStep, Version: 1}, Payload: []byte("{}")}
	_, err := a.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		return a.take(c, run, b, now), nil
	})
	return err
}

// take runs the step's tool and returns how the run changes.
func (a *Agents) take(c platform.Caller, run AgentRunRecord, b stepBody, now time.Time) func(*pb.ChangeRecord) {
	continuing := b.CallID != ""
	if len(b.Calls) > 0 {
		call := b.Calls[0]
		b.Tool, b.Arguments, b.CallID = call.Name, call.Arguments, call.ID
		run.Pending = slices.Clone(b.Calls[1:])
	} else if continuing {
		if len(run.Pending) == 0 || run.Pending[0].ID != b.CallID {
			return func(*pb.ChangeRecord) {} // historical duplicate; no tool is run twice
		}
		run.Pending = slices.Clone(run.Pending[1:])
	}
	// PostgreSQL JSONB may reorder nested object keys differently from Go's
	// encoder. Normalize on both live application and replay, not only before
	// journaling, because these raw bytes become strings in the run history.
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
	d := a.def(run.Agent)
	step := RunStep{At: now, Tool: b.Tool, Arguments: string(b.Arguments), Tokens: b.Usage.Input + b.Usage.Output,
		Calls: b.Calls, CallID: b.CallID, Content: b.Content}
	if len(b.Calls) > 0 {
		step.Sources = sourcesOf(run)
	}
	if !continuing {
		run.StepsUsed++
	}
	run.TokensUsed += step.Tokens
	if b.Usage.CostReported { // a cost nobody reported is not 0, it is unknown
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
		// The reply that crossed the budget: what it cost is kept, and no
		// further call is made for this run.
		step.Outcome = "over budget"
		stop(agents.BudgetReason(d.Budget, run.StepsUsed, run.TokensUsed, run.Cost))
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
	if step.CallID == "" { // the retained single-call journal format keeps its original result bytes
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
		c.Put(r, run) // before its flow goes on, which may keep a signal on it
		if run.State == "done" {
		}
	}
}

// Every accepted model call retains a result, including calls deliberately
// skipped because an earlier call finished or a person stopped the run.
func finishPendingCalls(run *AgentRunRecord, now time.Time) {
	sources := sourcesOf(*run)
	for _, call := range run.Pending {
		run.Steps = append(run.Steps, RunStep{At: now, Tool: call.Name, Arguments: string(call.Arguments), CallID: call.ID,
			Outcome: "not executed: the run " + run.State, Sources: sources})
	}
	run.Pending = nil
}

// use runs one tool; it may return what the decision does besides storing the run.
func (a *Agents) use(c platform.Caller, d *agentDef, run *AgentRunRecord, tool agentTool, args map[string]any, raw json.RawMessage, step *RunStep, now time.Time) (string, func(*pb.ChangeRecord)) {
	h := a.host
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
		view, err := h.AgentContext(a.readsAs(*run), str("type"), str("id"), now)
		if err != nil {
			return "refused: " + err.Error(), nil
		}
		if step.CallID == "" {
			step.Sources = nil // retained legacy steps recorded only this observation's sources
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
		view, err := h.AgentQuery(*reader, tool.query, str("for"), now)
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
		for _, field := range tool.inputs {
			if value, ok := declared[field.Name]; ok {
				values[field.Name] = value
			}
		}
		inputs, _ := json.Marshal(values)
		var view host.AgentObservation
		var err *kernel.Error
		if m := a.reader(*run); m != nil {
			view, err = h.AgentRead(*m, tool.schema, inputs)
		} else {
			var out any
			out, err = readApp(h.App(d.app), h.Automation(c, d.app), tool.schema, inputs)
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
		step.Sources = append(step.Sources, "read:"+tool.schema)
		return string(view.Body), nil
	}
	// An action: the declared tool, the person's own grants when it runs for one, the guard, the budget.
	if run.ActionsUsed >= d.Budget.Actions {
		return fmt.Sprintf("refused: its budget of %d actions is spent", d.Budget.Actions), nil
	}
	if tool.kind == "effect" { // sent to the endpoints bound to it, an external agent's answer awaited
		key := fmt.Sprintf("%s:%d", run.ID, len(run.Steps)+1)
		sender := platform.ActingCaller(c, a.AgentPrincipal(run.Agent), d.app, true)
		endRun := h.ScopeAgentRun(run.ID)
		n, err := sender.Emit(tool.schema, key, cmp.Or(run.Ref, RunType+"/"+run.ID), map[string]string{"message": str("message"), "run": run.ID, "agent": run.Agent}, now)
		endRun()
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
	trackTarget := func() {
		if tool.target == "" || target == "" {
			return
		}
		ref := tool.target + "/" + target
		if _, exists := h.Record(ref); exists && !slices.Contains(step.Sources, ref) {
			step.Sources = append(step.Sources, ref)
		}
	}
	// Some owners accept a logical target rather than a stored row ID. Only
	// real rows are record dependencies, before or after the nested decision.
	trackTarget()
	body := agents.ActionPayload(raw, step.CallID != "")
	if d.Guard != nil {
		if err := d.Guard(h.Automation(c, d.app), a.run(*run), tool.schema, target, body); err != nil {
			return "refused by its guard: " + err.Error(), nil
		}
	}
	key := fmt.Sprintf("agent:%s:%d", run.ID, len(run.Steps)+1)
	agent := platform.AgentCaller(c, a.AgentPrincipal(run.Agent), d.app, run.ID)
	person := a.reader(*run)
	mustConfirm := slices.Contains(d.Checkpoints, tool.schema)
	if tool.kind == "action" {
		if declared, ok := h.App(d.app).Manifest().Actions.Action(tool.schema); ok && declared.Approval != nil {
			mustConfirm = true
		}
	}
	if mustConfirm && person == nil {
		return "refused: this checkpoint waits for a person", nil
	}
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
			owner, provided, ok := h.ProtocolProvider(tool.schema)
			if !ok || !owner.Manifest().Actions.PermitsAny(person.RolesIn(owner.Manifest().ID), provided) {
				return "refused: " + person.ID + " may not " + schema + " at the provider", nil
			}
			if !run.Acts || mustConfirm {
				return draft("")
			}
		}
		endRun := h.ScopeAgentRun(run.ID) // effects retain their originating run
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
	app := h.App(d.app)
	s := &pb.Submission{TenantId: a.tenant, PrincipalId: agent.ID, Authority: a.authorityOf(tool.target), IdempotencyKey: key,
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
		if !run.Acts || mustConfirm {
			return draft(tool.target)
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
	return "done: " + tool.schema + " " + target + " (" + record.GetChangeId() + ")", nil
}

func (a *Agents) recipients(c platform.Caller, d *agentDef, run AgentRunRecord) []platform.Recipient {
	if run.OnBehalf != "" {
		return []platform.Recipient{{Member: run.OnBehalf}}
	}
	if d != nil && d.To != nil {
		return d.To(a.host.Automation(c, d.app), a.run(run))
	}
	return nil
}

// stop ends a run that cannot go on: a flow's run goes back to its flow; any
// other goes to a person as a task.
func (a *Agents) stop(c platform.Caller, r *pb.ChangeRecord, run *AgentRunRecord, why string, now time.Time) {
	run.State, run.Stopped, run.Draft = "stopped", why, nil
	finishPendingCalls(run, now)
	if run.Task != "" {
		a.host.AgentTasks().Close(c, r, run.Task)
		run.Task = ""
	}
	if run.Flow != "" {
		return
	}
	c.Assign(r, platform.Assignment{Title: "Take over from the agent: " + run.Title, Body: "The agent " + run.Agent + " stopped: " + why + ".",
		Ref: RunType + "/" + run.ID, To: a.recipients(c, a.def(run.Agent), *run), Key: "agent:" + run.ID + ":stopped"})
}

// Interested and Listen take the events agents wait on (internal/host.Listener).
func (a *Agents) Interested(_ []string, e platform.Event) bool { return a.interested(e) }

func (a *Agents) Listen(c platform.Caller, e platform.Event, _ []string, now time.Time) *kernel.Error {
	return a.handle(c, e, now)
}

// interested: an answer to a run's question.
func (a *Agents) interested(e platform.Event) bool {
	s := e.Record.GetSubmission()
	return e.App == platform.WorkApp && s.GetSchema().GetName() == "work.task.complete" && strings.HasPrefix(s.GetTarget().GetId(), AgentApp+":")
}

// handle resumes the run a person answered.
func (a *Agents) handle(c platform.Caller, e platform.Event, now time.Time) *kernel.Error {
	id := e.Record.GetSubmission().GetTarget().GetId()
	for _, run := range a.runs(c, "waiting") {
		if run.Task != id {
			continue
		}
		task, _ := platform.Get[platform.WorkTask](a.host.Automation(c, platform.WorkApp), id)
		s := &pb.Submission{TenantId: a.tenant, PrincipalId: c.ID, Authority: AgentApp, IdempotencyKey: fmt.Sprintf("%s:%d", run.ID, run.Revision+1),
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
func (a *Agents) AgentEffectEnded(c platform.Caller, r *pb.ChangeRecord, x platform.Effect, o platform.Outcome, now time.Time) {
	task := "effect:" + x.Event + ":" + x.Key
	for _, run := range a.runs(a.host.Automation(c, AgentApp), "waiting") {
		if run.Task != task {
			continue
		}
		last := &run.Steps[len(run.Steps)-1]
		switch o.Result {
		case "delivered":
			last.Outcome += "\nanswered: " + agents.Clip(answerOf(o), outcomeLimit)
		case "discarded":
			last.Outcome += "\n" + o.Detail
		default:
			last.Outcome += "\nthe receiver " + x.State + " it: " + o.Detail
		}
		run.State, run.Task = "running", ""
		if r != nil {
			a.host.Automation(c, AgentApp).Put(r, run)
			return
		}
		ac := a.host.Automation(c, AgentApp)
		s := &pb.Submission{TenantId: a.tenant, PrincipalId: ac.ID, Authority: AgentApp, IdempotencyKey: fmt.Sprintf("%s:%d", run.ID, run.Revision+1),
			Target: &pb.EntityRef{Type: RunType, Id: run.ID}, Schema: &pb.SchemaRef{Name: SchemaRunStep, Version: 1}, Payload: []byte("{}")}
		a.ledger.Receive(ac, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
			return func(r *pb.ChangeRecord) { ac.Put(r, run) }, nil
		})
		return
	}
}
