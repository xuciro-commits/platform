package authoring

import (
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/agents"
	"platformserver/platform"
	"slices"
	"strings"
	"sync"
	"time"
)

type AgentEvaluator struct {
	mu                sync.Mutex
	pendingEvaluation *platform.Evaluation
	context           platform.AgentEvaluationRuntime
}

func NewAgentEvaluator() platform.AgentEvaluator      { return &AgentEvaluator{} }
func ownerOf(h platform.AgentHost, typ string) string { owner, _ := h.OwnerOf(typ); return owner }

const evaluationRuns = 20
const outcomeLimit = 4000
const suiteRuns = 3

func decision(d *platform.CompiledAgent, steps []platform.RunStep, result string, value func(i int) string) (actions []string, bad []string, out string) {
	for i, s := range steps {
		tool, ok := d.Toolset[s.Tool]
		if !ok || tool.Kind != "action" && tool.Kind != "protocol" {
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
		act := tool.Schema + " " + target + " " + normal(string(payload))
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

func evalBudget(b platform.Budget, planned int) platform.Budget {
	b.Steps, b.Tokens, b.Cost = b.Steps*planned, b.Tokens*planned, b.Cost*float64(planned)
	return b
}

func budgetLeft(ceiling, spent platform.Budget) platform.Budget {
	left := platform.Budget{Steps: ceiling.Steps - spent.Steps, Tokens: ceiling.Tokens - spent.Tokens}
	if ceiling.Cost > 0 {
		left.Cost = ceiling.Cost - spent.Cost
	}
	return left
}

func minBudget(a, b platform.Budget) platform.Budget {
	out := platform.Budget{Steps: min(a.Steps, b.Steps), Tokens: min(a.Tokens, b.Tokens)}
	if a.Cost > 0 && b.Cost > 0 {
		out.Cost = min(a.Cost, b.Cost)
	}
	return out
}

func (a *AgentEvaluator) Evaluate(ctx platform.AgentEvaluationRuntime, ev platform.Evaluation, now time.Time, checkpoint func(*platform.Evaluation) bool) platform.Evaluation {
	a.context = ctx
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
	save := func(ev *platform.Evaluation) bool {
		if checkpoint(ev) {
			return true
		}
		copy := *ev
		a.mu.Lock()
		a.pendingEvaluation = &copy
		a.mu.Unlock()
		return false
	}
	h := a.context.Host
	endRead := h.BeginAgentRead()
	c := a.context.Automation(platform.AgentApp, false)
	d := a.context.Definition(ev.Agent)
	var definitionErr error
	digest := ""
	if ev.Candidate != "" {
		d, digest, definitionErr = a.context.Compile(ev.Candidate, ev.Agent)
	} else if d != nil {
		digest = a.context.Version(d)
	}
	if ev.Binding == nil || ev.Binding.Stage != platform.AIStageDesign {
		definitionErr = fmt.Errorf("the evaluation has no trusted execution stage; start a new evaluation")
	}
	if definitionErr == nil && ev.DefinitionDigest != "" && digest != ev.DefinitionDigest {
		definitionErr = fmt.Errorf("the evaluation's saved declaration differs")
	}
	model, pv, err := h.AgentModel(ev.Model)
	config := ""
	if err == nil {
		var problem error
		if ev.Binding != nil {
			options := platform.ModelOptions{}
			if d != nil {
				options = d.ModelOptions
			}
			current, _, refusal := h.ResolveAgentModel(ev.Model, options, platform.AIStageDesign)
			if refusal != nil {
				definitionErr = fmt.Errorf("the evaluation model options are invalid")
			}
			config, problem = a.context.Host.AgentModelConfigurationDigest(model, pv, &current)
			model.Defaults = ev.Binding.Options
		} else {
			config, problem = a.context.Host.AgentModelConfigurationDigest(model, pv, nil)
		}
		if problem != nil || ev.ModelConfig != "" && config != ev.ModelConfig {
			definitionErr = fmt.Errorf("the evaluation's model configuration changed")
		}
	}
	who, _ := h.Member(ev.Created.By)
	initialize := ev.DefinitionDigest == "" || ev.ModelConfig == ""
	if !ev.Suite && ev.Inputs == nil {
		domain, _ := json.Marshal([]any{[]any{"agent", "=", ev.Agent}, []any{"state", "=", "done"}})
		past, _, _ := platform.Find[platform.AgentRunRecord](c, platform.Query{Domain: domain, Sort: []string{"-created"}, Limit: 200})
		ev.Inputs = []platform.EvaluationInput{}
		for _, run := range past {
			if len(run.Signals) != 0 {
				ev.Inputs = append(ev.Inputs, platform.EvaluationInput{Run: run.ID, Revision: run.Revision})
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
		ev.Cases = append(slices.Clone(ev.Cases), platform.EvalCase{Verdict: "fails", Candidate: reason})
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
		run, known := platform.Get[platform.AgentRunRecord](c, input.Run)
		endRead()
		x := platform.EvalCase{Run: input.Run, Verdict: "fails", Candidate: "the evaluation's historical run revision changed"}
		if known && run.Revision == input.Revision {
			x = a.rerun(ev.ID, d, run, model, pv, who, now)
		}
		ev.Cases = append(ev.Cases, x)
		evaluationScore(&ev)
		if !save(&ev) {
			return ev
		}
	}
	if len(ev.Cases) == 0 {
		ev.Cases = []platform.EvalCase{{Verdict: "nothing", Candidate: "No run of this agent was confirmed, changed or rejected by a person yet, so there is nothing to compare the candidate with. Let people judge its runs first, or run its declared cases."}}
	}
	ev.State = "done"
	return ev
}

func evaluationScore(ev *platform.Evaluation) {
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

func (a *AgentEvaluator) rerun(evaluation string, d *platform.CompiledAgent, run platform.AgentRunRecord, model platform.AIModel, pv platform.AIProvider, who platform.Member, now time.Time) platform.EvalCase {
	last := run.Signals[len(run.Signals)-1]
	x := platform.EvalCase{Run: run.ID, Signal: last.Kind}
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
	dry, failed := a.dryRun(evaluation, d, run, model, pv, who, now, d.Budget)
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

func (a *AgentEvaluator) suite(ev platform.Evaluation, d *platform.CompiledAgent, model platform.AIModel, pv platform.AIProvider, who platform.Member, now time.Time, checkpoint func(*platform.Evaluation) bool) platform.Evaluation {
	ceiling, spent := evalBudget(d.Budget, len(d.Cases)*suiteRuns), platform.Budget{}
	for _, x := range ev.Cases {
		spent.Steps += x.Steps
		spent.Tokens += x.Tokens
		spent.Cost += x.Cost
	}
	for n, c := range d.Cases {
		if n == len(ev.Cases) {
			ev.Cases = append(ev.Cases, platform.EvalCase{Case: c.Name})
		}
		for ev.Cases[n].Runs < suiteRuns {
			x := ev.Cases[n]
			i := x.Runs
			room := budgetLeft(ceiling, spent)
			miss := "over the evaluation's budget"
			if room.Steps > 0 && room.Tokens > 0 && (ceiling.Cost == 0 || room.Cost > 0) {
				run := platform.AgentRunRecord{Record: platform.Record{ID: fmt.Sprintf("%s:%s:%d", ev.ID, c.Name, i+1)}, Agent: ev.Agent, Goal: c.Goal, Ref: c.Ref, OnBehalf: who.ID}
				dry, failed := a.dryRun(ev.ID, d, run, model, pv, who, now, minBudget(d.Budget, room))
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

func (a *AgentEvaluator) dryRun(evaluation string, d *platform.CompiledAgent, run platform.AgentRunRecord, model platform.AIModel, pv platform.AIProvider, who platform.Member, now time.Time, room platform.Budget) (platform.AgentRunRecord, string) {
	h := a.context.Host
	dry := platform.AgentRunRecord{Protocol: map[int]json.RawMessage{}, Record: run.Record, Agent: run.Agent, Goal: run.Goal, Ref: run.Ref, Seen: run.Seen, OnBehalf: run.OnBehalf, Steps: []platform.RunStep{}}
	priorTokens := 0
	for _, usage := range h.AgentUsage() {
		if usage.Run == evaluation+":"+run.ID+":evaluation" {
			priorTokens += usage.Input + usage.Output
		}
	}
	for dry.StepsUsed < room.Steps && dry.TokensUsed < room.Tokens {
		endRead := h.BeginAgentRead()
		req := a.context.Prompt(a.context.Automation(platform.AgentApp, false), d, dry, model.Name(), now)
		endRead()
		req.Run = evaluation + ":" + run.ID + ":evaluation"
		binding, _, problem := h.ResolveAgentModel(model.Name(), model.Defaults, platform.AIStageDesign)
		if problem != nil {
			return dry, problem.Error()
		}
		req.Binding = &binding
		attempt := 0
		for _, usage := range h.AgentUsage() {
			if usage.Run == req.Run {
				attempt++
			}
		}
		req.Call = fmt.Sprintf("evaluation:%s:%d:%d", req.Run, dry.StepsUsed, attempt)
		req.RunTokens = room.Tokens - priorTokens - dry.TokensUsed
		if req.RunTokens <= 0 {
			return dry, "The model request exceeds the remaining run token budget"
		}
		answer, failure := h.CallAgentModel(pv, model, who, req, now)
		if answer.Usage.Model == "" && failure != nil {
			return dry, failure.Detail
		}
		if err := h.MeterAgentUsage(who, answer.Usage); err != nil {
			return dry, "the model usage was not accepted: " + err.Error()
		}
		if len(answer.Protocol) > 0 {
			dry.Protocol[len(dry.Steps)] = answer.Protocol
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
		calls, err := agents.CompleteToolCalls(answer.ToolCalls, run.ID+":evaluation", dry.StepsUsed)
		if err != nil {
			return dry, err.Error()
		}
		for i, call := range calls {
			var args map[string]any
			json.Unmarshal(call.Arguments, &args)
			step := platform.RunStep{At: now, Tool: call.Name, Arguments: string(call.Arguments), CallID: call.ID}
			if i == 0 {
				step.Calls, step.Content = calls, answer.Content
			}
			step.Rationale, _ = args["rationale"].(string)
			tool, ok := d.Toolset[call.Name]
			switch {
			case dry.State != "":
				step.Outcome = "not executed: the run " + dry.State
			case !ok:
				step.Outcome = "no tool " + call.Name
			case tool.Kind == "finish":
				dry.Result, dry.State, step.Outcome = fmt.Sprint(args["result"]), "done", "finished"
			case tool.Kind == "ask":
				dry.State, dry.Result, step.Outcome = "asked", "asks: "+fmt.Sprint(args["question"]), "asked"
			default:
				step.Outcome = agents.Observation(a.dryUse(d, run, dry, tool, args, call.Arguments, now), outcomeLimit)
			}
			dry.Steps = append(dry.Steps, step)
		}
		if dry.State != "" {
			break
		}
	}
	return dry, ""
}

func (a *AgentEvaluator) dryUse(d *platform.CompiledAgent, run, dry platform.AgentRunRecord, tool platform.CompiledAgentTool, args map[string]any, raw json.RawMessage, now time.Time) string {
	strip := func(s string) string {
		var m map[string]json.RawMessage
		json.Unmarshal([]byte(s), &m)
		delete(m, "rationale")
		out, _ := json.Marshal(m)
		return string(out)
	}
	if tool.Kind == "read" || tool.Kind == "query" || tool.Kind == "context" || tool.Kind == "search" || tool.Kind == "knowledge" {
		for _, s := range run.Steps {
			if s.Tool == tool.Tool.Name && strip(s.Arguments) == strip(string(raw)) {
				return s.Outcome
			}
		}
	}
	if tool.Kind == "knowledge" {
		return string(a.context.ObserveKnowledge(dry, raw, now))
	}
	h := a.context.Host
	endRead := h.BeginAgentRead()
	defer endRead()
	c := a.context.Automation(platform.AgentApp, true)
	if tool.Kind == "read" || tool.Kind == "query" || tool.Kind == "context" || tool.Kind == "search" {
		out, _ := a.context.Use(c, d, &dry, tool, args, raw, &platform.RunStep{}, now)
		return out
	}
	target, _ := args["target"].(string)
	payload := map[string]json.RawMessage{}
	json.Unmarshal(raw, &payload)
	delete(payload, "target")
	delete(payload, "rationale")
	body, _ := json.Marshal(payload)
	if d.Guard != nil {
		if err := d.Guard(a.context.Automation(d.App, true), a.context.View(dry), tool.Schema, target, body); err != nil {
			return "refused by its guard: " + err.Error()
		}
	}
	if tool.Kind == "action" {
		app := h.App(d.App)
		s := &pb.Submission{TenantId: a.context.Tenant, Authority: ownerOf(a.context.Host, tool.Target), IdempotencyKey: "eval:" + run.ID,
			Target: &pb.EntityRef{Type: tool.Target, Id: target}, Schema: &pb.SchemaRef{Name: tool.Schema, Version: 1}, Payload: body}
		as := []platform.Caller{platform.ActingCaller(c, a.context.Principal(run.Agent), d.App, true)}
		if m := a.context.Reader(run); m != nil {
			as = append(as, h.Caller(platform.Caller{}, *m, app.Manifest().ID))
		}
		for _, who := range as {
			s.PrincipalId = who.ID
			err := platform.ProbeDecision(who, func(probe platform.Caller) *kernel.Error {
				_, err := platform.Decide(probe, app, s, now)
				return err
			})
			if err != nil && err.Code == pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
				return "refused: " + who.ID + " may not (" + err.Error() + ")"
			}
		}
	}
	return "would do: " + tool.Schema + " " + target
}
