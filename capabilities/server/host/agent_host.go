package host

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"platformserver/services/ai/gateway"
	"platformserver/services/data/transform"
	controlowner "platformserver/services/security/control"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

	"platformserver/platform"
	"platformserver/services/ai/evaluation"
	"platformserver/services/resources/authoring"
	"slices"
	"strings"
	"sync"
	"time"
)

func (h hostView) ProtocolActions(protocol string) []platform.Action {
	return h.t.protocolActions(protocol)
}
func (h hostView) ProtocolProvider(schema string) (platform.App, string, bool) {
	return h.t.provider(schema)
}
func (h hostView) InvokeDecision(c platform.Caller, protocol, action, id string, raw []byte, key, correlation string, now time.Time) (*pb.EntityRef, *pb.ChangeRecord, *kernel.Error) {
	return h.t.invoke(c, protocol, action, id, raw, key, correlation, now)
}
func (h hostView) AgentContext(reader *platform.Member, typ, id string, now time.Time) (platform.AgentObservation, *kernel.Error) {
	view, err := h.t.contextRead(aiContextReader(reader), typ, id, now)
	if err != nil {
		return platform.AgentObservation{}, err
	}
	raw, _ := json.Marshal(view)
	return platform.AgentObservation{Body: raw, Sources: h.t.refsIn(view)}, nil
}
func (h hostView) AgentSearch(reader *platform.Member, q string, now time.Time) platform.AgentObservation {
	found := h.t.searchRead(aiContextReader(reader), q, now)
	out := platform.AgentObservation{}
	for _, hit := range found {
		out.Sources = append(out.Sources, hit.Type+"/"+hit.ID)
	}
	out.Body, _ = json.Marshal(found)
	return out
}
func (h hostView) AgentQuery(reader platform.Member, query platform.NamedQuery, of string, now time.Time) (platform.AgentObservation, *kernel.Error) {
	reader.AIContext = true
	page, err := h.t.runDeclaredQueryFrom(h.t.Projection, reader, query, of, now)
	if err != nil {
		return platform.AgentObservation{}, err
	}
	raw, _ := json.Marshal(page.Records)
	var ids []struct{ ID string }
	_ = json.Unmarshal(raw, &ids)
	out := platform.AgentObservation{}
	for _, row := range ids {
		out.Sources = append(out.Sources, query.Object+"/"+row.ID)
	}
	out.Body, _ = json.Marshal(page)
	return out, nil
}
func (h hostView) AgentRead(reader platform.Member, name string, inputs ...json.RawMessage) (platform.AgentObservation, *kernel.Error) {
	reader.AIContext = true
	value, err := h.t.read(reader, name, inputs...)
	if err != nil {
		return platform.AgentObservation{}, err
	}
	raw, _ := json.Marshal(value)
	return platform.AgentObservation{Body: raw, Sources: h.t.refsIn(value)}, nil
}
func (h hostView) AgentSurface(reader platform.Member, app string) platform.AgentSurface {
	reader.AIContext = true
	var out platform.AgentSurface
	var call *platform.AgentAction
	owner, action, found := h.Action(build.SchemaFunctionCall)
	if found && slices.ContainsFunc(h.t.Catalog(reader), func(a platform.Action) bool { return a.Schema == action.Schema }) {
		call = &platform.AgentAction{Owner: owner, Action: action}
	}
	for _, definition := range h.t.Definitions(reader) {
		if app != "" && definition.Ref.App != app || definition.Ref.App == build.ID && definition.Source == "code" && definition.Function == nil {
			continue
		}
		switch {
		case definition.Entity != nil:
			out.Entities = append(out.Entities, *definition.Entity)
		case definition.Action != nil:
			action := platform.AgentAction{Owner: definition.Ref.App, Action: *definition.Action}
			if criteria, ok := h.t.modelCriteria(definition.Ref); ok {
				for _, condition := range criteria.Conditions {
					action.Conditions = append(action.Conditions, condition.Message)
				}
			}
			out.Actions = append(out.Actions, action)
		case definition.Function != nil:
			if call != nil {
				out.Functions = append(out.Functions, platform.AgentFunction{Owner: definition.Ref.App, Function: *definition.Function, Call: *call})
			}
		}
	}
	return out
}

const agentObjectRows = 50

func (h hostView) AgentObjects(reader platform.Member, op string, raw json.RawMessage, now time.Time) (platform.AgentObservation, *kernel.Error) {
	reader.AIContext = true
	invalid := func(message string) (platform.AgentObservation, *kernel.Error) {
		return platform.AgentObservation{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, message)
	}
	var req ObjectSetRequest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&req) != nil || decoder.Decode(new(any)) != io.EOF || req.Offset < 0 || req.Limit < 0 || req.Set.Check() != nil {
		return invalid("The object set request is not valid")
	}
	aggregated := len(req.Groups) > 0 || len(req.Measures) > 0 || req.Window != nil || req.Histogram != nil || req.MaxRows != 0 || req.Collection != nil
	out := platform.AgentObservation{}
	switch op {
	case "load":
		if aggregated {
			return invalid("A load carries no aggregate options")
		}
		if req.Limit == 0 || req.Limit > agentObjectRows {
			req.Limit = agentObjectRows
		}
		caller := platform.NewCaller(runtime{t: h.t}, reader, "", false, false)
		page, object, err := h.t.loadObjectSet(h.t.Projection, caller, req.Set, objectWindow{Sort: req.Sort, Offset: req.Offset, Limit: req.Limit, Bounded: true}, now)
		if err != nil {
			return out, err
		}
		body, _ := json.Marshal(page)
		var rows struct {
			Records []struct{ ID string } `json:"records"`
		}
		_ = json.Unmarshal(body, &rows)
		for _, row := range rows.Records {
			if object != "" && row.ID != "" {
				out.Sources = append(out.Sources, object+"/"+row.ID)
			}
		}
		out.Body = body
	case "aggregate":
		if len(req.Sort) > 0 || req.Offset != 0 || req.Limit != 0 || req.Format || req.Window != nil || req.Histogram != nil || req.Collection != nil {
			return invalid("An aggregate carries groups and measures only")
		}
		result, err := h.t.aggregateObjectSet(reader, req, now)
		if err != nil {
			return out, err
		}
		out.Body, _ = json.Marshal(result)
	default:
		return invalid("Unknown object set operation")
	}
	return out, nil
}

func (h hostView) AgentSources(value any) []string { return h.t.refsIn(value) }
func (h hostView) AgentKnowledge(reader *platform.Member, app, q string, limit int, now time.Time) json.RawMessage {
	found := h.t.Knowledge(aiContextReader(reader), app, q, limit, now)
	for i := range found {
		found[i].Text = agents.Clip(found[i].Text, 700)
	}
	raw, _ := json.Marshal(found)
	return raw
}
func (h hostView) AgentAppReader(app string) *platform.Member { return h.t.appReader(app) }
func (h hostView) AgentMeaning(app string) string             { return h.t.meaning(app) }
func (h hostView) AgentGlossary(app string) string            { return h.t.glossary(app) }
func (h hostView) AgentLanguage(language string) string       { return agents.LanguageName(language) }
func (h hostView) AgentSuspended(member string) bool          { return h.t.suspended(member) }
func (h hostView) ExternalAgentMembers() []platform.Member {
	owner, ok := h.t.app(controlowner.PlatformApp).(platform.IdentityDirectory)
	if !ok {
		return nil
	}
	return owner.AgentMembers()
}
func (h hostView) AgentDecisionMembers() []string {
	var out []string
	for _, entry := range h.t.Audit() {
		out = append(out, entry.Member)
	}
	return out
}
func readAgentHost[T any](h hostView, read func(*Tenant) (T, *kernel.Error)) (T, *kernel.Error) {
	if h.decision != nil {
		return read(h.t)
	}
	return recordRead(h.t, context.Background(), read)
}

func (h hostView) AgentUsage() []platform.AIUsage {
	values, _ := readAgentHost(h, func(t *Tenant) ([]platform.AIUsage, *kernel.Error) {
		if t.ai == nil {
			return nil, nil
		}
		return t.ai.Usage(), nil
	})
	return values
}

func (h hostView) WithAgentRead(read func(platform.AgentEvaluationRuntime) *kernel.Error) *kernel.Error {
	source := h.t
	if source.readOrigin != nil {
		source = source.readOrigin
	}
	_, refusal := recordRead(source, context.Background(), func(t *Tenant) (struct{}, *kernel.Error) {
		if t.agents == nil {
			return struct{}{}, unknown()
		}
		return struct{}{}, read(t.agents.AgentEvaluationRuntime())
	})
	return refusal
}

func (h hostView) ScopeAgentRun(run string) func() {
	previous := h.t.agentRun
	h.t.agentRun = run
	return func() { h.t.agentRun = previous }
}

func (h hostView) ResolveAgentModel(name string, options platform.ModelOptions, stage string) (platform.AIModelBinding, platform.AIProvider, *kernel.Error) {
	type selection struct {
		binding  platform.AIModelBinding
		provider platform.AIProvider
	}
	value, refusal := readAgentHost(h, func(t *Tenant) (selection, *kernel.Error) {
		binding, provider, refusal := t.resolveModel("agent", name, options)
		if refusal == nil {
			binding, refusal = t.modelStage(binding, provider, stage)
		}
		return selection{binding: binding, provider: provider}, refusal
	})
	return value.binding, value.provider, refusal
}
func (h hostView) AgentModel(name string) (platform.AIModel, platform.AIProvider, *kernel.Error) {
	type selection struct {
		model    platform.AIModel
		provider platform.AIProvider
	}
	value, refusal := readAgentHost(h, func(t *Tenant) (selection, *kernel.Error) {
		if t.ai == nil {
			return selection{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "AI capability unavailable")
		}
		model, provider, refusal := t.ai.Model(name)
		return selection{model: model, provider: provider}, refusal
	})
	return value.model, value.provider, refusal
}

func (h hostView) AgentProviderAvailable(provider string, now time.Time) bool {
	return h.t.breakers.Allow("ai:"+provider, now, false)
}
func (h hostView) CallAgentModel(provider platform.AIProvider, model platform.AIModel, member platform.Member, request platform.ChatRequest, now time.Time) (platform.ChatAnswer, *platform.AIError) {
	_, refusal := recordRead(h.t, context.Background(), func(t *Tenant) (struct{}, *kernel.Error) {
		c := platform.NewCaller(runtime{t: t}, member, platform.AgentApp, false, false)
		return struct{}{}, authorizationRefusal(t.authorizeNative(c, "model:"+request.Model, "action", []string{platform.AnyMember}, true))
	})
	if refusal != nil {
		return platform.ChatAnswer{}, &platform.AIError{Status: 403, Detail: refusal.Message}
	}
	answer, failure := h.t.call(provider, model, member, request, now)
	_, _ = recordRead(h.t, context.Background(), func(t *Tenant) (struct{}, *kernel.Error) {
		if value, ok := t.held(platform.RunType + "/" + request.Run); ok {
			if run, ok := value.(platform.AgentRunRecord); ok {
				answer.Usage = agentRunUsage(answer.Usage, run)
			}
		}
		return struct{}{}, nil
	})
	return answer, failure
}

func agentRunUsage(usage platform.AIUsage, run platform.AgentRunRecord) platform.AIUsage {
	usage.Applications = append([]platform.AssetBinding(nil), run.Applications...)
	usage.Run = run.ID
	if owner, name, valid := strings.Cut(run.Agent, "."); valid {
		ref := platform.AssetRef{App: owner, Kind: platform.AssetAgent, Name: name}
		usage.Resource = &ref
	}
	return usage
}
func (h hostView) MeterAgentUsage(member platform.Member, usage platform.AIUsage) *kernel.Error {
	return h.t.meter(member, usage)
}
func (h hostView) ReplayAgentUsage(usage platform.AIUsage) {
	if h.t.ai != nil && usage.Model != "" {
		h.t.ai.Meter(usage)
	}
}

var _ platform.AgentHost = hostView{}

func (h hostView) AgentTasks() platform.TaskClosure         { return h.Tasks() }
func (h hostView) AgentFlows() platform.AgentFlowCompletion { return h.Processes() }

func aiContextReader(reader *platform.Member) *platform.Member {
	if reader == nil {
		return nil
	}
	copy := *reader
	copy.AIContext = true
	return &copy
}

type agentEvaluationApp struct {
	platform.App
	agent string
}

func (a agentEvaluationApp) Fork(ledger *platform.Ledger) (platform.App, error) {
	fork, err := a.App.Fork(ledger)
	if err != nil {
		return nil, err
	}
	return agentEvaluationApp{App: fork, agent: a.agent}, nil
}

func (a agentEvaluationApp) Manifest() platform.Manifest {
	m := a.App.Manifest()
	m.Agents = slices.DeleteFunc(slices.Clone(m.Agents), func(d platform.Agent) bool { return d.Name != a.agent })
	return m
}

func (h hostView) AgentEvaluationApp(candidate, agent string) (platform.App, string, error) {
	owner, name, valid := strings.Cut(agent, ".")
	if !valid || owner != build.ID {
		return nil, "", fmt.Errorf("candidate evaluations require a controlled builder agent")
	}
	saved, err := h.t.releases.Candidate(candidate)
	if err != nil {
		return nil, "", err
	}
	ref := platform.AssetRef{App: owner, Kind: platform.AssetAgent, Name: name}
	i := slices.IndexFunc(saved.Assets, func(a platform.ReleaseAsset) bool { return a.Ref == ref })
	if i < 0 {
		return nil, "", fmt.Errorf("the saved candidate does not contain this agent")
	}
	compiler, ok := h.t.app(owner).(platform.ReleaseExecutionOwner)
	if !ok {
		return nil, "", fmt.Errorf("the agent's original execution owner is unavailable")
	}
	bound, err := compiler.ForkReleaseExecution(saved.Assets)
	if err != nil {
		return nil, "", err
	}
	digest, err := platform.CanonicalDigest(saved.Assets[i])
	return agentEvaluationApp{App: bound, agent: name}, digest, err
}

func (t *Tenant) candidateAgentModel(candidate, name string) (string, platform.ModelOptions, error) {
	app, _, err := (hostView{t: t}).AgentEvaluationApp(candidate, name)
	if err != nil {
		return "", platform.ModelOptions{}, err
	}
	defs := app.Manifest().Agents
	if len(defs) != 1 {
		return "", platform.ModelOptions{}, fmt.Errorf("candidate agent is unavailable")
	}
	model := defs[0].Model
	if model == "" {
		model = t.setting(t.automation(platform.AgentApp, false), platform.SettingAgentModel)
	}
	binding, _, problem := t.resolveModel("agent", model, defs[0].ModelOptions)
	if problem != nil {
		return "", platform.ModelOptions{}, fmt.Errorf("%s", problem.Message)
	}
	return binding.Model, defs[0].ModelOptions, nil
}

func (t *Tenant) evaluateReleaseAgentLocked(m platform.Member, request ReleaseEvaluationRequest, now time.Time) (string, error) {
	if t.agents == nil {
		return "", fmt.Errorf("the agent capability is unavailable")
	}
	identity, err := platform.CanonicalDigest([]string{t.ID, m.ID, request.CandidateID, request.Agent, request.Key})
	if err != nil {
		return "", err
	}
	id := "eval-" + identity[:32]
	if prior, ok := platform.Get[platform.Evaluation](t.automation(platform.AgentApp, false), id); ok {
		if prior.Candidate != request.CandidateID || prior.Agent != request.Agent || prior.Created.By != m.ID {
			return "", fmt.Errorf("the original evaluation request differs")
		}
		return id, nil
	}
	model, _, err := t.candidateAgentModel(request.CandidateID, request.Agent)
	if err != nil {
		return "", err
	}
	if model == "" {
		return "", fmt.Errorf("set the agent model before evaluating a candidate")
	}
	sub := &pb.Submission{TenantId: t.ID, PrincipalId: m.ID, Authority: platform.AgentApp,
		IdempotencyKey: "evaluation:" + identity, Target: &pb.EntityRef{Type: platform.EvaluationType, Id: id},
		Schema: &pb.SchemaRef{Name: platform.SchemaEvalStart, Version: 1}, Payload: platform.Raw(map[string]any{"agent": request.Agent, "model": model, "suite": true, "candidate": request.CandidateID})}
	if _, refusal := t.accept(t.agents, m, sub, now, acceptOptions{automation: true}); refusal != nil {
		return "", fmt.Errorf("agent evaluation was refused: %s", refusal.Message)
	}
	return id, nil
}

func (t *Tenant) suspended(member string) bool {
	v, ok := t.held(platform.SwitchType + "/" + member)
	return ok && v.(platform.Switch).Suspended
}

func (t *Tenant) Think(now time.Time) { integrations.OnLane(t.turns(now)) }

func (t *Tenant) turns(now time.Time) []func() {
	if t.agents == nil || t.businessStopped() {
		return nil
	}
	var out []func()
	turns, _ := recordRead(t, context.Background(), func(t *Tenant) ([]platform.AgentTurn, *kernel.Error) { return t.agents.DueAgentTurns(now), nil })
	for _, x := range turns {
		if !t.agents.ClaimAgentTurn(x.Run.ID) {
			continue
		}
		out = append(out, func() {
			defer t.agents.IdleAgentTurn(x.Run.ID)
			if !t.businessStopped() {
				t.take(x, now)
			}
		})
	}
	return out
}

func (t *Tenant) stillRunning(id string) bool {
	running, refusal := recordRead(t, context.Background(), func(t *Tenant) (bool, *kernel.Error) {
		run, known := platform.Get[platform.AgentRunRecord](t.automation(platform.AgentApp, false), id)
		return known && run.State == "running", nil
	})
	if !running || refusal != nil {
		return false
	}
	return true
}

func (t *Tenant) take(x platform.AgentTurn, now time.Time) {
	{
		if x.Stop != "" {
			t.agentStep(platform.AgentResultStep{Run: x.Run.ID, Stop: x.Stop}, now)
			return
		}
		if x.Run.OnBehalf != "" {
			allowed, refusal := recordRead(t, context.Background(), func(t *Tenant) (bool, *kernel.Error) { return t.agents.AllowsAgentRun(x.Run), nil })
			if refusal != nil || !allowed {
				t.agentStep(platform.AgentResultStep{Run: x.Run.ID, Stop: "the person no longer has access to this agent's app"}, now)
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
			return
		}
		if !t.stillRunning(x.Run.ID) {
			if answer.Usage.Model != "" {
				t.meter(t.agents.AgentPrincipal(x.Run.Agent), answer.Usage)
			}
			return
		}
		body := platform.AgentResultStep{Run: x.Run.ID, Content: answer.Content, Protocol: answer.Protocol, Usage: answer.Usage}
		if failure != nil {
			if failure.Quota {
				body.Stop = failure.Detail
			} else {
				body.Failure = failure.Detail
			}
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

func (t *Tenant) continueAgentCalls(run platform.AgentRunRecord, now time.Time) {
	defer wakeWork()
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
		if !t.applyAgentStep(platform.AgentResultStep{Run: run.ID, CallID: call.ID, Tool: call.Name, Arguments: call.Arguments,
			Observation: observations[i]}, now) {
			break
		}
	}
}

func (t *Tenant) agentStep(b platform.AgentResultStep, now time.Time) {
	t.applyAgentStep(b, now)
}

func (t *Tenant) applyAgentStep(b platform.AgentResultStep, now time.Time) bool {
	applied, refusal := decideOnImage(t, context.Background(), "agent:settle", func(t *Tenant) (bool, *kernel.Error) {
		return t.prepareAgentStep(b, now), nil
	}, func(t *Tenant, candidate preparedAccepted, applied bool, refusal *kernel.Error) (bool, *kernel.Error) {
		if len(candidate.entry.Body) != 0 {
			t.finishAgentStep(candidate)
		}
		return applied && !t.quarantined(), refusal
	})
	return applied && refusal == nil
}

func (t *Tenant) prepareAgentStep(b platform.AgentResultStep, now time.Time) bool {
	if t.quarantined() {
		return false
	}
	if t.recordsAcceptedResults() {
		now = now.Truncate(time.Microsecond)
	}
	run, known := platform.Get[platform.AgentRunRecord](t.automation(platform.AgentApp, false), b.Run)
	if b.Evaluation != nil {
		ev, waited := platform.Get[platform.Evaluation](t.automation(platform.AgentApp, false), b.Evaluation.ID)
		if !waited || ev.State != "queued" || ev.Revision != b.Evaluation.Revision {
			return false
		}
		run.Agent = b.Evaluation.Agent
	} else if !known || run.State != "running" {
		return false
	}
	if b.CallID != "" && (len(run.Pending) == 0 || run.Pending[0].ID != b.CallID) {
		return false
	}
	if b.Stop == "" && run.OnBehalf != "" {
		if !t.agents.AllowsAgentRun(run) {
			b.Stop = "the person no longer has access to this agent's app"
			b.Tool, b.Arguments, b.Observation, b.Calls, b.CallID = "", nil, nil, nil, ""
		}
	}
	err := t.acceptAgentStep(b, now)
	if err != nil && b.Stop == "" && !t.quarantined() {
		err = t.acceptAgentStep(platform.AgentResultStep{Run: b.Run, Evaluation: b.Evaluation, Usage: b.Usage, Stop: "The agent step could not be accepted: " + err.Error()}, now)
	}
	if err != nil {
		t.observeOwnerRun(now, "agent", b.Run, platform.AgentApp, "", "failed", "Agent step", err.Error())
	}
	return err == nil && !t.quarantined()
}

func (t *Tenant) Evaluate(now time.Time) {
	if t.agents == nil || t.ai == nil {
		return
	}
	for {
		if t.businessStopped() {
			return
		}
		evals, refusal := recordRead(t, context.Background(), func(t *Tenant) ([]platform.Evaluation, *kernel.Error) {
			evals, _, refusal := platform.Find[platform.Evaluation](t.automation(platform.AgentApp, false), platform.Query{Domain: json.RawMessage(`[["state","=","queued"]]`), Sort: []string{"id"}, Limit: 1})
			return evals, refusal
		})
		if refusal != nil || len(evals) == 0 {
			return
		}
		report := t.agentEvaluator.Evaluate(t.agents.AgentEvaluationRuntime(), evals[0], now, func(ev *platform.Evaluation) bool {
			revision := ev.Revision
			t.agentStep(platform.AgentResultStep{Run: ev.ID, Evaluation: ev}, now)
			current, refusal := recordRead(t, context.Background(), func(t *Tenant) (platform.Evaluation, *kernel.Error) {
				current, known := platform.Get[platform.Evaluation](t.automation(platform.AgentApp, false), ev.ID)
				if !known {
					return current, unknown()
				}
				return current, nil
			})
			known := refusal == nil
			if !known || current.Revision != revision+1 {
				return false
			}
			*ev = current
			return true
		})
		if report.State != "done" {
			return
		}
		t.agentStep(platform.AgentResultStep{Run: report.ID, Evaluation: &report}, now)
		current, refusal := recordRead(t, context.Background(), func(t *Tenant) (platform.Evaluation, *kernel.Error) {
			current, known := platform.Get[platform.Evaluation](t.automation(platform.AgentApp, false), report.ID)
			if !known {
				return current, unknown()
			}
			return current, nil
		})
		known := refusal == nil
		if !known || current.State == "queued" {
			return
		}
	}
}

func (h hostView) AgentModelConfigurationDigest(model platform.AIModel, provider platform.AIProvider, binding *platform.AIModelBinding) (string, error) {
	return aigateway.ConfigurationDigest(model, provider, binding)
}

func (h hostView) AgentAnswerText(outcome platform.Outcome) string { return answerOf(outcome) }
