package platformserver

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	build "platformserver/internal/host/buildapi"
	"platformserver/platform"
)

// An evaluation re-runs an agent's past runs dry against a candidate model
// (ADR-0054). Its reference is what people made of each run — the signals:
// a confirmed or accepted decision should come again, a rejected or corrected
// one should not. The candidate sees what the run saw (its context at the
// start, what its reads answered); reads it makes anew read the records as
// they are; its actions are probed, never taken. The report is journaled as
// an agent entry, so replay keeps it and never calls the model.
const (
	EvaluationType  = "agent.evaluation"
	SchemaEvalStart = "agent.evaluation.start"
	evaluationRuns  = 20 // the latest runs people answered
)

// Evaluation is one candidate's report on an agent's past runs.
type Evaluation struct {
	platform.Record
	Agent   string  `json:"agent" field:"readonly,search"`
	Model   string  `json:"model" field:"readonly,search" title:"Candidate model"`
	State   string  `json:"state" field:"readonly" choices:"queued,done"`
	Agrees  int     `json:"agrees" field:"readonly" title:"Agrees with accepted"`
	Differs int     `json:"differs" field:"readonly" title:"Differs from accepted"`
	Repeats int     `json:"repeats" field:"readonly" title:"Repeats a correction"`
	Avoids  int     `json:"avoids" field:"readonly" title:"Avoids a correction"`
	Asks    int     `json:"asks" field:"readonly"`
	Fails   int     `json:"fails" field:"readonly"`
	Score   float64 `json:"score" field:"readonly"` // agrees and avoids, of the cases
	Tokens  int     `json:"tokens" field:"readonly"`
	// Cost is the USD the providers reported for the whole evaluation; calls
	// whose cost was not reported are not counted here (ADR-0008).
	Cost  float64    `json:"cost,omitempty" field:"readonly" title:"Model cost, USD"`
	Cases []EvalCase `json:"cases" field:"readonly"`
	// Suite runs the agent's declared cases instead of its past runs (ADR-0008 D6).
	Suite            bool              `json:"suite,omitempty" field:"readonly" title:"Declared cases"`
	Candidate        string            `json:"candidate,omitempty" field:"readonly" title:"Release candidate"`
	DefinitionDigest string            `json:"definitionDigest,omitempty" field:"readonly" title:"Evaluated definition"`
	ModelConfig      string            `json:"modelConfig,omitempty" field:"readonly" title:"Evaluated model configuration"`
	Inputs           []EvaluationInput `json:"inputs,omitempty" field:"readonly"`
}

// EvaluationInput pins the historical run revision selected for a dry rerun.
type EvaluationInput struct {
	Run      string `json:"run"`
	Revision uint32 `json:"revision"`
}

const suiteRuns = 3 // each declared case, as models vary

// EvalCase is one past run and what the candidate made of it, or one of the
// agent's declared cases run three times (ADR-0008 D6).
type EvalCase struct {
	Case      string  `json:"case,omitempty"`   // the declared case's name
	Passes    int     `json:"passes,omitempty"` // of Runs
	Runs      int     `json:"runs,omitempty"`
	Run       string  `json:"run"`
	Signal    string  `json:"signal"` // what people made of the run
	Verdict   string  `json:"verdict"`
	Reference string  `json:"reference"` // the decision people accepted, or the one they corrected
	Candidate string  `json:"candidate"`
	Steps     int     `json:"steps"`
	Tokens    int     `json:"tokens"`
	Cost      float64 `json:"cost,omitempty"`
}

// decision is a run's outcome as the evaluation compares it: the actions it
// took or drafted, else its result.
func decision(d *agentDef, steps []RunStep, result string, value func(i int) string) (actions []string, bad []string, out string) {
	for i, s := range steps {
		tool, ok := d.tools[s.Tool]
		if !ok || tool.kind != "action" && tool.kind != "protocol" {
			continue
		}
		if !strings.HasPrefix(s.Outcome, "done:") && !strings.HasPrefix(s.Outcome, "drafted for") && !strings.HasPrefix(s.Outcome, "would do") {
			continue
		}
		var args map[string]any
		json.Unmarshal([]byte(s.Arguments), &args)
		target, _ := args["target"].(string)
		delete(args, "target")
		delete(args, "rationale")
		payload, _ := json.Marshal(args)
		if v := value(i); v != "" {
			payload = []byte(v)
		}
		act := tool.schema + " " + target + " " + normal(string(payload))
		if strings.Contains(s.Outcome, "\nrejected by ") {
			bad = append(bad, act)
			continue
		}
		actions = append(actions, act)
	}
	if len(actions) == 0 {
		out = normal(result)
	}
	return actions, bad, out
}

// normal is JSON with sorted keys, or the trimmed text.
func normal(s string) string {
	s = strings.TrimSpace(s)
	if i, j := strings.Index(s, "{"), strings.LastIndex(s, "}"); i >= 0 && j > i {
		var v any
		if json.Unmarshal([]byte(s[i:j+1]), &v) == nil {
			raw, _ := json.Marshal(v)
			return string(raw)
		}
	}
	return s
}

// evalBudget is what a whole evaluation of planned runs may spend: the
// definition's budget once per run, so a suite cannot run past what its runs
// were each allowed (ADR-0008, review AI-05).
func evalBudget(b platform.Budget, planned int) platform.Budget {
	b.Steps, b.Tokens, b.Cost = b.Steps*planned, b.Tokens*planned, b.Cost*float64(planned)
	return b
}

// budgetLeft is what remains of ceiling after spent. A zero cost ceiling means
// no cost cap, as it does for a run.
func budgetLeft(ceiling, spent platform.Budget) platform.Budget {
	left := platform.Budget{Steps: ceiling.Steps - spent.Steps, Tokens: ceiling.Tokens - spent.Tokens}
	if ceiling.Cost > 0 {
		left.Cost = ceiling.Cost - spent.Cost
	}
	return left
}

// minBudget is the tighter of two budgets: a run may spend what both allow.
func minBudget(a, b platform.Budget) platform.Budget {
	out := platform.Budget{Steps: min(a.Steps, b.Steps), Tokens: min(a.Tokens, b.Tokens)}
	if a.Cost > 0 && b.Cost > 0 {
		out.Cost = min(a.Cost, b.Cost)
	}
	return out
}

// Evaluate works through queued evaluations, one at a time, outside the
// tenant's lock but for each tool it probes; the host calls it apart from Think.
func (t *Tenant) Evaluate(now time.Time) {
	if t.agents == nil || t.ai == nil {
		return
	}
	for {
		t.mu.Lock()
		if t.quarantined() {
			t.mu.Unlock()
			return
		}
		evals, _, _ := platform.Find[Evaluation](t.automation(AgentApp, false), platform.Query{Domain: json.RawMessage(`[["state","=","queued"]]`), Sort: []string{"id"}, Limit: 1})
		t.mu.Unlock()
		if len(evals) == 0 {
			return
		}
		report := t.agents.EvaluateAgent(evals[0], now, func(ev *Evaluation) bool {
			revision := ev.Revision
			t.agentStep(stepBody{Run: ev.ID, Evaluation: ev}, now)
			t.mu.Lock()
			current, known := platform.Get[Evaluation](t.automation(AgentApp, false), ev.ID)
			t.mu.Unlock()
			if !known || current.Revision != revision+1 {
				return false
			}
			*ev = current
			return true
		})
		if report.State != "done" {
			return // a checkpoint was not accepted; do no further model work
		}
		t.agentStep(stepBody{Run: report.ID, Evaluation: &report}, now)
		t.mu.Lock()
		current, known := platform.Get[Evaluation](t.automation(AgentApp, false), report.ID)
		t.mu.Unlock()
		if !known || current.State == "queued" {
			return // retry on the next work cycle after an append failure
		}
	}
}

// evaluationVersion includes the declared inputs as well as the original
// owner's version. Check functions stay owned by that version, never serialized.
func (a *Agents) evaluationVersion(d *agentDef) string {
	cases := make([][]string, len(d.Cases))
	for i, c := range d.Cases {
		cases[i] = []string{c.Name, c.Goal, c.Ref}
	}
	digest, _ := canonicalDigest([]any{a.definitionVersion(d), cases})
	return digest
}

func (a *Agents) EvaluateAgent(ev Evaluation, now time.Time, checkpoint func(*Evaluation) bool) Evaluation {
	// An append error is not a new evaluation attempt. Retry the exact item
	// before any further model call, even if another queued ID now sorts first.
	a.mu.Lock()
	pending := a.pendingEvaluation
	a.mu.Unlock()
	if pending != nil {
		ev = *pending
		if !checkpoint(&ev) {
			return ev
		}
		a.mu.Lock()
		a.pendingEvaluation = nil
		a.mu.Unlock()
	}
	save := func(ev *Evaluation) bool {
		if checkpoint(ev) {
			return true
		}
		copy := *ev
		a.mu.Lock()
		a.pendingEvaluation = &copy
		a.mu.Unlock()
		return false
	}
	h := a.host
	endRead := h.BeginAgentRead()
	c := a.automation(AgentApp, false)
	d := a.def(ev.Agent)
	var definitionErr error
	digest := ""
	if ev.Candidate != "" {
		d, digest, definitionErr = a.evaluationDefinition(ev.Candidate, ev.Agent)
	} else if d != nil {
		digest = a.evaluationVersion(d)
	}
	if definitionErr == nil && ev.DefinitionDigest != "" && digest != ev.DefinitionDigest {
		definitionErr = fmt.Errorf("the evaluation's saved declaration differs")
	}
	model, pv, err := h.AgentModel(ev.Model)
	config := ""
	if err == nil {
		var problem error
		config, problem = canonicalDigest([]any{model, pv})
		if problem != nil || ev.ModelConfig != "" && config != ev.ModelConfig {
			definitionErr = fmt.Errorf("the evaluation's model configuration changed")
		}
	}
	who, _ := h.Member(ev.Created.By)
	// Select historical inputs once. Later runs and new human signals cannot
	// silently change the remaining comparison after a restart.
	initialize := ev.DefinitionDigest == "" || ev.ModelConfig == ""
	if !ev.Suite && ev.Inputs == nil {
		domain, _ := json.Marshal([]any{[]any{"agent", "=", ev.Agent}, []any{"state", "=", "done"}})
		past, _, _ := platform.Find[AgentRunRecord](c, platform.Query{Domain: domain, Sort: []string{"-created"}, Limit: 200})
		ev.Inputs = []EvaluationInput{}
		for _, run := range past {
			if len(run.Signals) != 0 {
				ev.Inputs = append(ev.Inputs, EvaluationInput{Run: run.ID, Revision: run.Revision})
				if len(ev.Inputs) == evaluationRuns {
					break
				}
			}
		}
		initialize = true
	}
	endRead()
	if definitionErr != nil || d == nil || err != nil {
		reason := "the agent is not declared, or the model " + ev.Model + " is not enabled"
		if definitionErr != nil {
			reason = definitionErr.Error()
		}
		ev.Cases = append(slices.Clone(ev.Cases), EvalCase{Verdict: "fails", Candidate: reason})
		ev.Fails++
		ev.Score, ev.State = 0, "done"
		return ev
	}
	if initialize {
		ev.DefinitionDigest, ev.ModelConfig = digest, config
		if !save(&ev) {
			return ev
		}
	}
	if ev.Suite {
		return a.suite(ev, d, model, pv, who, now, save)
	}
	for _, input := range ev.Inputs[len(ev.Cases):] {
		endRead := h.BeginAgentRead()
		run, known := platform.Get[AgentRunRecord](c, input.Run)
		endRead()
		x := EvalCase{Run: input.Run, Verdict: "fails", Candidate: "the evaluation's historical run revision changed"}
		if known && run.Revision == input.Revision {
			x = a.rerun(d, run, model, pv, who, now)
		}
		ev.Cases = append(ev.Cases, x)
		evaluationScore(&ev)
		if !save(&ev) {
			return ev
		}
	}
	if len(ev.Cases) == 0 {
		ev.Cases = []EvalCase{{Verdict: "nothing", Candidate: "No run of this agent was confirmed, changed or rejected by a person yet, so there is nothing to compare the candidate with. Let people judge its runs first, or run its declared cases."}}
	}
	ev.State = "done"
	return ev
}

// Rebuild the small report from its accepted items, including spend from an
// unfinished suite case. A resumed evaluation uses the same total budget.
func evaluationScore(ev *Evaluation) {
	ev.Agrees, ev.Differs, ev.Repeats, ev.Avoids, ev.Asks, ev.Fails = 0, 0, 0, 0, 0, 0
	ev.Tokens, ev.Cost, ev.Score = 0, 0, 0
	total, passed := 0, 0
	for _, x := range ev.Cases {
		ev.Tokens += x.Tokens
		ev.Cost += x.Cost
		if ev.Suite {
			total, passed = total+x.Runs, passed+x.Passes
		} else {
			total++
			if x.Verdict == "agrees" || x.Verdict == "avoids" {
				passed++
			}
		}
		switch x.Verdict {
		case "agrees", "passes":
			ev.Agrees++
		case "differs", "varies":
			ev.Differs++
		case "repeats":
			ev.Repeats++
		case "avoids":
			ev.Avoids++
		case "asks":
			ev.Asks++
		case "fails":
			ev.Fails++
		}
	}
	if total > 0 {
		ev.Score = float64(passed) / float64(total)
	}
}

// rerun runs one past run's goal dry with the candidate and judges it.
func (a *Agents) rerun(d *agentDef, run AgentRunRecord, model platform.AIModel, pv platform.AIProvider, who platform.Member, now time.Time) EvalCase {
	last := run.Signals[len(run.Signals)-1]
	x := EvalCase{Run: run.ID, Signal: last.Kind}
	changed := func(i int) string {
		if strings.Contains(run.Steps[i].Outcome, "\nchanged by ") {
			for _, s := range run.Signals {
				if s.Kind == "changed" {
					return s.Value
				}
			}
		}
		return ""
	}
	want, bad, wantResult := decision(d, run.Steps, run.Result, changed)
	good := slices.Contains([]string{"confirmed", "changed", "accepted", "approved"}, last.Kind)
	if good {
		x.Reference = strings.Join(append(want, wantResult), "; ")
	} else {
		bad = append(bad, want...)
		if len(want) == 0 {
			bad = append(bad, wantResult)
		}
		x.Reference = "not: " + strings.Join(bad, "; ")
	}
	dry, failed := a.dryRun(d, run, model, pv, who, now, d.Budget)
	x.Steps, x.Tokens, x.Cost = dry.StepsUsed, dry.TokensUsed, dry.Cost
	if failed != "" {
		x.Verdict, x.Candidate = "fails", failed
	} else if dry.State == "asked" {
		x.Candidate = dry.Result
	}
	switch {
	case x.Verdict != "":
	case dry.State == "asked":
		x.Verdict = "asks"
	case dry.State != "done":
		x.Verdict, x.Candidate = "fails", "over its budget"
	default:
		got, _, gotResult := decision(d, dry.Steps, dry.Result, func(int) string { return "" })
		x.Candidate = strings.Join(append(got, gotResult), "; ")
		repeats := slices.ContainsFunc(got, func(s string) bool { return slices.Contains(bad, s) }) || len(got) == 0 && slices.Contains(bad, gotResult)
		switch {
		case repeats:
			x.Verdict = "repeats"
		case !good:
			x.Verdict = "avoids"
		case slices.Equal(got, want) && gotResult == wantResult:
			x.Verdict = "agrees"
		default:
			x.Verdict = "differs"
		}
	}
	return x
}

// suite runs each of the agent's declared cases three times, dry, and counts
// the runs that pass (ADR-0008 D6): a case passes when every run does, varies
// when some do, and fails when none does. The score is the runs that pass.
func (a *Agents) suite(ev Evaluation, d *agentDef, model platform.AIModel, pv platform.AIProvider, who platform.Member, now time.Time, checkpoint func(*Evaluation) bool) Evaluation {
	ceiling, spent := evalBudget(d.Budget, len(d.Cases)*suiteRuns), platform.Budget{}
	for _, x := range ev.Cases {
		spent.Steps += x.Steps
		spent.Tokens += x.Tokens
		spent.Cost += x.Cost
	}
	for n, c := range d.Cases {
		if n == len(ev.Cases) {
			ev.Cases = append(ev.Cases, EvalCase{Case: c.Name})
		}
		for ev.Cases[n].Runs < suiteRuns {
			x := ev.Cases[n]
			i := x.Runs
			room := budgetLeft(ceiling, spent)
			miss := "over the evaluation's budget"
			if room.Steps > 0 && room.Tokens > 0 && (ceiling.Cost == 0 || room.Cost > 0) {
				run := AgentRunRecord{Record: platform.Record{ID: fmt.Sprintf("%s:%s:%d", ev.ID, c.Name, i+1)}, Agent: ev.Agent, Goal: c.Goal, Ref: c.Ref, OnBehalf: who.ID}
				dry, failed := a.dryRun(d, run, model, pv, who, now, minBudget(d.Budget, room))
				x.Steps, x.Tokens, x.Cost = x.Steps+dry.StepsUsed, x.Tokens+dry.TokensUsed, x.Cost+dry.Cost
				spent.Steps, spent.Tokens, spent.Cost = spent.Steps+dry.StepsUsed, spent.Tokens+dry.TokensUsed, spent.Cost+dry.Cost
				got, _, result := decision(d, dry.Steps, dry.Result, func(int) string { return "" })
				miss = failed
				switch {
				case miss != "":
				case dry.State != "done" && dry.State != "asked":
					miss = "over its budget"
				case c.Check != nil:
					miss = c.Check(platform.CaseRun{Actions: got, Result: result, Asked: dry.State == "asked"})
				}
				if i == 0 {
					x.Candidate = strings.Join(append(got, result), "; ")
				}
			}
			if miss == "" {
				x.Passes++
			} else {
				if x.Reference != "" {
					x.Reference += "; "
				}
				x.Reference += fmt.Sprintf("run %d: %s", i+1, miss)
			}
			x.Runs++
			if x.Runs == suiteRuns {
				x.Verdict = "varies"
				if x.Passes == suiteRuns {
					x.Verdict = "passes"
				}
				if x.Passes == 0 {
					x.Verdict = "fails"
				}
			}
			ev.Cases[n] = x
			evaluationScore(&ev)
			if !checkpoint(&ev) {
				return ev
			}
		}
	}
	ev.State = "done"
	return ev
}

// dryRun runs a goal with the candidate, dry: reads the run made answer as
// then, other reads read now, actions are probed and never taken. failed says
// why the model could not go on; an asked run's question is its Result.
func (a *Agents) dryRun(d *agentDef, run AgentRunRecord, model platform.AIModel, pv platform.AIProvider, who platform.Member, now time.Time, room platform.Budget) (AgentRunRecord, string) {
	h := a.host
	dry := AgentRunRecord{Record: run.Record, Agent: run.Agent, Goal: run.Goal, Ref: run.Ref, Seen: run.Seen, OnBehalf: run.OnBehalf, Steps: []RunStep{}}
	for dry.StepsUsed < room.Steps && dry.TokensUsed < room.Tokens {
		endRead := h.BeginAgentRead()
		req := a.prompt(a.automation(AgentApp, false), d, dry, model.Name(), now)
		endRead()
		req.Run = run.ID + ":evaluation"
		if why := h.ReserveAgentModel(who, model, now); why != "" {
			return dry, why
		}
		answer, failure := h.CallAgentModel(pv, model, who, req, now)
		if err := h.MeterAgentUsage(who, answer.Usage); err != nil {
			return dry, "the model usage was not accepted: " + err.Error()
		}
		dry.StepsUsed++
		dry.TokensUsed += answer.Usage.Input + answer.Usage.Output
		if answer.Usage.CostReported {
			dry.Cost += answer.Usage.Cost
		}
		if failure != nil {
			return dry, "the model failed: " + failure.Detail
		}
		if len(answer.ToolCalls) == 0 {
			dry.Result, dry.State = answer.Content, "done"
			break
		}
		calls, err := completeToolCalls(answer.ToolCalls, run.ID+":evaluation", dry.StepsUsed)
		if err != nil {
			return dry, err.Error()
		}
		for i, call := range calls {
			var args map[string]any
			json.Unmarshal(call.Arguments, &args)
			step := RunStep{At: now, Tool: call.Name, Arguments: string(call.Arguments), CallID: call.ID}
			if i == 0 {
				step.Calls, step.Content = calls, answer.Content
			}
			step.Rationale, _ = args["rationale"].(string)
			tool, ok := d.tools[call.Name]
			switch {
			case dry.State != "":
				step.Outcome = "not executed: the run " + dry.State
			case !ok:
				step.Outcome = "no tool " + call.Name
			case tool.kind == "finish":
				dry.Result, dry.State, step.Outcome = fmt.Sprint(args["result"]), "done", "finished"
			case tool.kind == "ask":
				dry.State, dry.Result, step.Outcome = "asked", "asks: "+fmt.Sprint(args["question"]), "asked"
			default:
				step.Outcome = clipObservation(a.dryUse(d, run, dry, tool, args, call.Arguments, now), outcomeLimit)
			}
			dry.Steps = append(dry.Steps, step)
		}
		if dry.State != "" {
			break
		}
	}
	return dry, ""
}

// dryUse answers a tool the candidate called: a read the run made gets the
// answer it got then; another read reads now; an action is probed, not taken.
func (a *Agents) dryUse(d *agentDef, run, dry AgentRunRecord, tool agentTool, args map[string]any, raw json.RawMessage, now time.Time) string {
	strip := func(s string) string {
		var m map[string]json.RawMessage
		json.Unmarshal([]byte(s), &m)
		delete(m, "rationale")
		out, _ := json.Marshal(m)
		return string(out)
	}
	if tool.kind == "read" || tool.kind == "context" || tool.kind == "search" || tool.kind == "knowledge" {
		for _, s := range run.Steps {
			if s.Tool == tool.tool.Name && strip(s.Arguments) == strip(string(raw)) {
				return s.Outcome
			}
		}
	}
	if tool.kind == "knowledge" { // a new question: searched now, outside the lock
		return string(a.ObserveAgentKnowledge(dry, raw, now))
	}
	h := a.host
	endRead := h.BeginAgentRead()
	defer endRead()
	c := a.automation(AgentApp, true) // replaying: no decision is made
	if tool.kind == "read" || tool.kind == "context" || tool.kind == "search" {
		out, _ := a.use(c, d, &dry, tool, args, raw, &RunStep{}, now)
		return out
	}
	target, _ := args["target"].(string)
	payload := map[string]json.RawMessage{}
	json.Unmarshal(raw, &payload)
	delete(payload, "target")
	delete(payload, "rationale")
	body, _ := json.Marshal(payload)
	if d.Guard != nil {
		if err := d.Guard(a.automation(d.app, true), a.run(dry), tool.schema, target, body); err != nil {
			return "refused by its guard: " + err.Error()
		}
	}
	if tool.kind == "action" {
		app := h.App(d.app)
		s := &pb.Submission{TenantId: a.tenant, Authority: a.authorityOf(tool.target), IdempotencyKey: "eval:" + run.ID,
			Target: &pb.EntityRef{Type: tool.target, Id: target}, Schema: &pb.SchemaRef{Name: tool.schema, Version: 1}, Payload: body}
		as := []platform.Caller{platform.ActingCaller(c, a.AgentPrincipal(run.Agent), d.app, true)}
		if m := a.reader(run); m != nil {
			as = append(as, h.Caller(platform.Caller{}, *m, app.Manifest().ID))
		}
		for _, who := range as {
			s.PrincipalId = who.ID
			err := platform.ProbeDecision(who, func(probe platform.Caller) *kernel.Error {
				_, err := platform.Decide(probe, app, s, now)
				return err
			})
			if err != nil && err.Code == pb.ErrorCode_ERROR_CODE_POLICY_DENIED { // the records may have moved on since; what it may do has not
				return "refused: " + who.ID + " may not (" + err.Error() + ")"
			}
		}
	}
	return "would do: " + tool.schema + " " + target
}

// startEvaluation queues a candidate's evaluation (an administrator's decision).
func (a *Agents) startEvaluation(c platform.Caller, id string, p struct {
	Agent, Model, Candidate string
	Suite                   bool
}) (func(*pb.ChangeRecord), *kernel.Error) {
	if _, known := platform.Get[Evaluation](c, id); known {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	d := a.def(p.Agent)
	digest, config := "", ""
	if p.Candidate != "" {
		if !p.Suite || !c.Holds(build.ID, build.Builder) {
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
		config, err = canonicalDigest([]any{model, provider})
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
	ev := Evaluation{Record: platform.Record{ID: id}, Agent: p.Agent, Model: p.Model, State: "queued", Cases: []EvalCase{}, Suite: p.Suite, Candidate: p.Candidate, DefinitionDigest: digest, ModelConfig: config}
	return func(r *pb.ChangeRecord) { a.host.Automation(c, AgentApp).Put(r, ev) }, nil
}
