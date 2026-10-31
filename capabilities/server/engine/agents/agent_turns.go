package agents

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/agents"
	"platformserver/platform"
	"slices"
	"strings"
	"time"
)

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
		tool, ok := a.tool(run, d, b.Tool)
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
	case "objects", "aggregate":
		view, err := a.objects(*run, tool, args, now)
		if err != nil {
			return "refused: " + err.Error(), nil
		}
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
			return "refused: " + reason(refusal), nil
		}
		run.Draft, run.State = []platform.Draft{draft}, "waiting"
		run.Proposals = append(run.Proposals, draft)
		return "drafted for confirmation: " + tool.Schema, func(r *pb.ChangeRecord) { a.assignDraft(c, r, run, tool.Schema, now) }
	}
	target := str("target")
	body := agents.ActionPayload(raw, step.CallID != "")
	if tool.Preset != nil {
		call := maps.Clone(tool.Preset)
		for _, key := range []string{"source", "question"} {
			if value, ok := args[key]; ok {
				call[key] = value
			}
		}
		target, body = fmt.Sprintf("%s:f%d", run.ID, len(run.Steps)+1), nil
		body, _ = json.Marshal(call)
	}
	if d.Guard != nil {
		if err := d.Guard(h.Automation(c, d.App), a.run(*run), tool.Schema, target, body); err != nil {
			return "refused by its guard: " + err.Error(), nil
		}
	}
	draft, refusal := a.toolDraft(c, *run, platform.Draft{Kind: tool.Kind, Owner: cmp.Or(tool.Owner, d.App), Action: tool.Schema, Type: tool.Target, Target: target, Payload: string(body), Rationale: str("rationale"), Step: len(run.Steps)}, now)
	if refusal != nil {
		return "refused: " + reason(refusal), nil
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
