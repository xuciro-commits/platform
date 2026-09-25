package platformserver

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
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
	Agent   string     `json:"agent" field:"readonly,search"`
	Model   string     `json:"model" field:"readonly,search" title:"Candidate model"`
	State   string     `json:"state" field:"readonly" choices:"queued,done"`
	Agrees  int        `json:"agrees" field:"readonly" title:"Agrees with accepted"`
	Differs int        `json:"differs" field:"readonly" title:"Differs from accepted"`
	Repeats int        `json:"repeats" field:"readonly" title:"Repeats a correction"`
	Avoids  int        `json:"avoids" field:"readonly" title:"Avoids a correction"`
	Asks    int        `json:"asks" field:"readonly"`
	Fails   int        `json:"fails" field:"readonly"`
	Score   float64    `json:"score" field:"readonly"` // agrees and avoids, of the cases
	Tokens  int        `json:"tokens" field:"readonly"`
	Cases   []EvalCase `json:"cases" field:"readonly"`
}

// EvalCase is one past run and what the candidate made of it.
type EvalCase struct {
	Run       string `json:"run"`
	Signal    string `json:"signal"` // what people made of the run
	Verdict   string `json:"verdict"`
	Reference string `json:"reference"` // the decision people accepted, or the one they corrected
	Candidate string `json:"candidate"`
	Steps     int    `json:"steps"`
	Tokens    int    `json:"tokens"`
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
	model, pv, err := t.ai.model(ev.Model)
	who, _ := t.member(ev.Created.By)
	t.mu.Unlock()
	ev.State = "done"
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
		ev.Tokens += x.Tokens
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
	}
	return ev
}

// rerun runs one past run's goal dry with the candidate and judges it.
func (a *Agents) rerun(d *agentDef, run AgentRunRecord, model Model, pv Provider, who platform.Member, now time.Time) EvalCase {
	t := a.t
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
	dry := AgentRunRecord{Record: run.Record, Agent: run.Agent, Goal: run.Goal, Ref: run.Ref, Seen: run.Seen, OnBehalf: run.OnBehalf, Steps: []RunStep{}}
	for dry.StepsUsed < d.Budget.Steps && dry.TokensUsed < d.Budget.Tokens {
		t.mu.Lock()
		req := a.prompt(t.automation(AgentApp, false), d, dry, model.Name(), now)
		t.mu.Unlock()
		req.run = run.ID + ":evaluation"
		answer, failure := t.call(pv, model, who, req, now)
		t.meter(who, answer.Usage)
		dry.StepsUsed++
		dry.TokensUsed += answer.Usage.Input + answer.Usage.Output
		if failure != nil {
			x.Verdict, x.Candidate = "fails", "the model failed: "+failure.Detail
			break
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
			dry.State, x.Candidate = "asked", "asks: "+fmt.Sprint(args["question"])
		default:
			step.Outcome = a.dryUse(d, run, dry, tool, args, call.Arguments, now)
		}
		dry.Steps = append(dry.Steps, step)
		if dry.State != "" {
			break
		}
	}
	x.Steps, x.Tokens = dry.StepsUsed, dry.TokensUsed
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
		out, _ := a.use(c, d, &dry, tool, args, raw, now)
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
			t.probing = true
			_, err := app.Submit(who, s, now)
			t.probing = false
			if err != nil && err.Code == pb.ErrorCode_ERROR_CODE_POLICY_DENIED { // the records may have moved on since; what it may do has not
				return "refused: " + who.ID + " may not (" + err.Error() + ")"
			}
		}
	}
	return "would do: " + tool.schema + " " + target
}

// startEvaluation queues a candidate's evaluation (an administrator's decision).
func (a *Agents) startEvaluation(c platform.Caller, id string, p struct{ Agent, Model string }) (func(*pb.ChangeRecord), *kernel.Error) {
	if _, known := platform.Get[Evaluation](c, id); known {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	if a.defs[p.Agent] == nil || p.Model == "" {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	ev := Evaluation{Record: platform.Record{ID: id}, Agent: p.Agent, Model: p.Model, State: "queued", Cases: []EvalCase{}}
	return func(r *pb.ChangeRecord) { a.t.automation(AgentApp, c.Replaying).Put(r, ev) }, nil
}
