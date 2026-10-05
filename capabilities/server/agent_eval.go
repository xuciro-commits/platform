package platformserver

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/ai"
	"platformserver/platform"
)

// An evaluation re-runs an agent's past runs dry against a candidate model
// (ADR-0021 D8). Its reference is what people made of each run — the signals:
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
	// whose cost was not reported are not counted here (ADR-0050 D7).
	Cost  float64    `json:"cost,omitempty" field:"readonly" title:"Model cost, USD"`
	Cases []EvalCase `json:"cases" field:"readonly"`
	// Suite runs the agent's declared cases instead of its past runs (ADR-0029 D6).
	Suite bool `json:"suite,omitempty" field:"readonly" title:"Declared cases"`
}

const suiteRuns = 3 // each declared case, as models vary

// EvalCase is one past run and what the candidate made of it, or one of the
// agent's declared cases run three times (ADR-0029 D6).
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
// were each allowed (ADR-0050 D9, review AI-05).
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
		evals, _, _ := platform.Find[Evaluation](t.automation(AgentApp, false), platform.Query{Domain: json.RawMessage(`[["state","=","queued"]]`), Sort: []string{"id"}, Limit: 1})
		t.mu.Unlock()
		if len(evals) == 0 {
			return
		}
		report := t.agents.evaluate(evals[0], now)
		t.agentStep(stepBody{Run: evals[0].ID, Evaluation: &report}, now)
	}
}

func (a *Agents) evaluate(ev Evaluation, now time.Time) Evaluation {
	t := a.t
	t.mu.Lock()
	c := t.automation(AgentApp, false)
	d := a.defs[ev.Agent]
	domain, _ := json.Marshal([]any{[]any{"agent", "=", ev.Agent}, []any{"state", "=", "done"}})
	past, _, _ := platform.Find[AgentRunRecord](c, platform.Query{Domain: domain, Sort: []string{"-created"}, Limit: 200})
	model, pv, err := t.ai.Model(ev.Model)
	who, _ := t.member(ev.Created.By)
	t.mu.Unlock()
	ev.State = "done"
	if d != nil && err == nil && ev.Suite {
		return a.suite(ev, d, model, pv, who, now)
	}
	if d == nil || err != nil {
		ev.Cases = []EvalCase{{Verdict: "fails", Candidate: "the agent is not declared, or the model " + ev.Model + " is not enabled"}}
		ev.Fails = 1
		return ev
	}
	for _, run := range past {
		if len(run.Signals) == 0 {
			continue
		}
		if len(ev.Cases) == evaluationRuns {
			break
		}
		x := a.rerun(d, run, model, pv, who, now)
		ev.Cases = append(ev.Cases, x)
		ev.Tokens, ev.Cost = ev.Tokens+x.Tokens, ev.Cost+x.Cost
		switch x.Verdict {
		case "agrees":
			ev.Agrees++
		case "differs":
			ev.Differs++
		case "repeats":
			ev.Repeats++
		case "avoids":
			ev.Avoids++
		case "asks":
			ev.Asks++
		default:
			ev.Fails++
		}
	}
	if n := len(ev.Cases); n > 0 {
		ev.Score = float64(ev.Agrees+ev.Avoids) / float64(n)
	} else { // nothing to compare: say so, rather than a report of nothing (the owner's testing)
		ev.Cases = []EvalCase{{Verdict: "nothing", Candidate: "No run of this agent was confirmed, changed or rejected by a person yet, so there is nothing to compare the candidate with. Let people judge its runs first, or run its declared cases."}}
	}
	return ev
}

// rerun runs one past run's goal dry with the candidate and judges it.
func (a *Agents) rerun(d *agentDef, run AgentRunRecord, model ai.Model, pv ai.Provider, who platform.Member, now time.Time) EvalCase {
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
// the runs that pass (ADR-0029 D6): a case passes when every run does, varies
// when some do, and fails when none does. The score is the runs that pass.
func (a *Agents) suite(ev Evaluation, d *agentDef, model ai.Model, pv ai.Provider, who platform.Member, now time.Time) Evaluation {
	// The whole evaluation has a ceiling too: each case runs suiteRuns times,
	// so it may spend the definition's budget that many times over, checked
	// before each run is started rather than after it spent
	// (ADR-0050 D9, review AI-05).
	ceiling, spent := evalBudget(d.Budget, len(d.Cases)*suiteRuns), platform.Budget{}
	total, passed := 0, 0
	for _, c := range d.Cases {
		x := EvalCase{Case: c.Name, Runs: suiteRuns}
		var why []string
		for i := range suiteRuns {
			room := budgetLeft(ceiling, spent)
			if room.Steps <= 0 || room.Tokens <= 0 || ceiling.Cost > 0 && room.Cost <= 0 {
				why = append(why, fmt.Sprintf("run %d: over the evaluation's budget", i+1))
				continue
			}
			run := AgentRunRecord{Record: platform.Record{ID: fmt.Sprintf("%s:%s:%d", ev.ID, c.Name, i+1)}, Agent: ev.Agent, Goal: c.Goal, Ref: c.Ref, OnBehalf: who.ID}
			dry, failed := a.dryRun(d, run, model, pv, who, now, minBudget(d.Budget, room))
			x.Steps, x.Tokens = x.Steps+dry.StepsUsed, x.Tokens+dry.TokensUsed
			x.Cost += dry.Cost
			spent.Steps, spent.Tokens, spent.Cost = spent.Steps+dry.StepsUsed, spent.Tokens+dry.TokensUsed, spent.Cost+dry.Cost
			got, _, result := decision(d, dry.Steps, dry.Result, func(int) string { return "" })
			miss := failed
			switch {
			case miss != "":
			case dry.State != "done" && dry.State != "asked":
				miss = "over its budget"
			case c.Check != nil:
				miss = c.Check(platform.CaseRun{Actions: got, Result: result, Asked: dry.State == "asked"})
			}
			if miss == "" {
				x.Passes++
			} else {
				why = append(why, fmt.Sprintf("run %d: %s", i+1, miss))
			}
			if i == 0 {
				x.Candidate = strings.Join(append(got, result), "; ")
			}
		}
		x.Verdict = map[bool]string{true: "passes", false: "varies"}[x.Passes == suiteRuns]
		if x.Passes == 0 {
			x.Verdict = "fails"
		}
		x.Reference = strings.Join(why, "; ")
		total, passed = total+suiteRuns, passed+x.Passes
		ev.Tokens, ev.Cost = ev.Tokens+x.Tokens, ev.Cost+x.Cost
		ev.Cases = append(ev.Cases, x)
		switch x.Verdict {
		case "passes":
			ev.Agrees++
		case "varies":
			ev.Differs++
		default:
			ev.Fails++
		}
	}
	if total > 0 {
		ev.Score = float64(passed) / float64(total)
	}
	return ev
}

// dryRun runs a goal with the candidate, dry: reads the run made answer as
// then, other reads read now, actions are probed and never taken. failed says
// why the model could not go on; an asked run's question is its Result.
func (a *Agents) dryRun(d *agentDef, run AgentRunRecord, model ai.Model, pv ai.Provider, who platform.Member, now time.Time, room platform.Budget) (AgentRunRecord, string) {
	t := a.t
	dry := AgentRunRecord{Record: run.Record, Agent: run.Agent, Goal: run.Goal, Ref: run.Ref, Seen: run.Seen, OnBehalf: run.OnBehalf, Steps: []RunStep{}}
	for dry.StepsUsed < room.Steps && dry.TokensUsed < room.Tokens {
		t.mu.Lock()
		req := a.prompt(t.automation(AgentApp, false), d, dry, model.Name(), now)
		t.mu.Unlock()
		req.run = run.ID + ":evaluation"
		if why := t.reserve(who, model, now); why != "" {
			return dry, why
		}
		answer, failure := t.call(pv, model, who, req, now)
		t.meter(who, answer.Usage)
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
		call := answer.ToolCalls[0]
		var args map[string]any
		json.Unmarshal(call.Arguments, &args)
		step := RunStep{At: now, Tool: call.Name, Arguments: string(call.Arguments)}
		step.Rationale, _ = args["rationale"].(string)
		tool, ok := d.tools[call.Name]
		switch {
		case !ok:
			step.Outcome = "no tool " + call.Name
		case tool.kind == "finish":
			dry.Result, dry.State = fmt.Sprint(args["result"]), "done"
		case tool.kind == "ask":
			dry.State, dry.Result = "asked", "asks: "+fmt.Sprint(args["question"])
		default:
			step.Outcome = a.dryUse(d, run, dry, tool, args, call.Arguments, now)
		}
		dry.Steps = append(dry.Steps, step)
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
		var m map[string]any
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
		return string(a.look(dry, raw, now))
	}
	t := a.t
	t.mu.Lock()
	defer t.mu.Unlock()
	c := t.automation(AgentApp, true) // replaying: no decision is made
	if tool.kind == "read" || tool.kind == "context" || tool.kind == "search" {
		out, _ := a.use(c, d, &dry, tool, args, raw, &RunStep{}, now)
		return out
	}
	target, _ := args["target"].(string)
	payload := map[string]any{}
	json.Unmarshal(raw, &payload)
	delete(payload, "target")
	delete(payload, "rationale")
	body, _ := json.Marshal(payload)
	if d.Guard != nil {
		if err := d.Guard(t.automation(d.app, true), a.run(dry), tool.schema, target, body); err != nil {
			return "refused by its guard: " + err.Error()
		}
	}
	if tool.kind == "action" {
		app := t.app(d.app)
		s := &pb.Submission{TenantId: t.ID, Authority: t.authorityOf(tool.target), IdempotencyKey: "eval:" + run.ID,
			Target: &pb.EntityRef{Type: tool.target, Id: target}, Schema: &pb.SchemaRef{Name: tool.schema, Version: 1}, Payload: body}
		as := []platform.Caller{platform.NewCaller(runtime{t}, a.member(run.Agent), d.app, false, true)}
		if m := a.reader(run); m != nil {
			as = append(as, t.caller(*m, app, false))
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
	Agent, Model string
	Suite        bool
}) (func(*pb.ChangeRecord), *kernel.Error) {
	if _, known := platform.Get[Evaluation](c, id); known {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	if a.defs[p.Agent] == nil || p.Model == "" {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if p.Suite && len(a.defs[p.Agent].Cases) == 0 {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The agent {agent} declares no cases", p.Agent)
	}
	ev := Evaluation{Record: platform.Record{ID: id}, Agent: p.Agent, Model: p.Model, State: "queued", Cases: []EvalCase{}, Suite: p.Suite}
	return func(r *pb.ChangeRecord) { a.t.automated(c, AgentApp).Put(r, ev) }, nil
}
