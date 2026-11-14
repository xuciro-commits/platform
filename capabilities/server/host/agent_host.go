package host

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	runtimekernel "platformserver/kernel"
	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
	"platformserver/services/actions"
	"platformserver/services/ai/evaluation"
	"platformserver/services/ai/gateway"
	agentowner "platformserver/services/ai/runtime"
	releaseowner "platformserver/services/devops/releases"
	"platformserver/services/objects"
	"platformserver/services/resources/authoring"
)

func (h hostView) ProtocolActions(protocol string) []platform.Action {
	return actions.ProtocolActions(h.t.LiveApps(), protocol)
}
func (h hostView) ProtocolProvider(schema string) (platform.App, string, bool) {
	return h.t.router().Provider(schema)
}
func (h hostView) InvokeDecision(c platform.Caller, protocol, action, id string, raw []byte, key, correlation string, now time.Time) (*pb.EntityRef, *pb.ChangeRecord, *kernel.Error) {
	return h.t.router().Invoke(c, protocol, action, id, raw, key, correlation, now)
}
func (h hostView) AgentContext(reader *platform.Member, typ, id string, now time.Time) (platform.AgentObservation, *kernel.Error) {
	view, err := h.t.contextRead(aiContextReader(reader), typ, id, now)
	if err != nil {
		return platform.AgentObservation{}, err
	}
	return agentowner.Observe(view, h.t.refsIn(view)), nil
}
func (h hostView) AgentSearch(reader *platform.Member, q string, now time.Time) platform.AgentObservation {
	return agentowner.SearchObservation(h.t.searchRead(aiContextReader(reader), q, now), func(hit build.Hit) string { return hit.Type + "/" + hit.ID })
}
func (h hostView) AgentQuery(reader platform.Member, query platform.NamedQuery, of string, now time.Time) (platform.AgentObservation, *kernel.Error) {
	reader.AIContext = true
	page, err := h.t.runDeclaredQueryFrom(h.t.Projection, reader, query, of, now)
	if err != nil {
		return platform.AgentObservation{}, err
	}
	return agentowner.QueryObservation(query.Object, page.Records, page), nil
}
func (h hostView) AgentRead(reader platform.Member, name string, inputs ...json.RawMessage) (platform.AgentObservation, *kernel.Error) {
	reader.AIContext = true
	value, err := h.t.read(reader, name, inputs...)
	if err != nil {
		return platform.AgentObservation{}, err
	}
	return agentowner.Observe(value, h.t.refsIn(value)), nil
}
func (h hostView) AgentSurface(reader platform.Member, app string) platform.AgentSurface {
	reader.AIContext = true
	var call *platform.AgentAction
	owner, action, found := h.Action(build.SchemaFunctionCall)
	if found && slices.ContainsFunc(h.t.Catalog(reader), func(a platform.Action) bool { return a.Schema == action.Schema }) {
		call = &platform.AgentAction{Owner: owner, Action: action}
	}
	return agentowner.SurfaceSource{Builder: build.ID, Call: call, Criteria: h.t.modelCriteria}.Surface(app, h.t.Definitions(reader))
}

func (h hostView) AgentObjects(reader platform.Member, op string, raw json.RawMessage, now time.Time) (platform.AgentObservation, *kernel.Error) {
	reader.AIContext = true
	return agentowner.Objects{
		Load: func(q agentowner.ObjectQuery, now time.Time) (any, string, *kernel.Error) {
			caller := platform.NewCaller(runtime{t: h.t}, reader, "", false, false)
			page, object, err := h.t.loadObjectSet(h.t.Projection, caller, q.Set, objects.Window{Sort: q.Sort, Offset: q.Offset, Limit: q.Limit, Bounded: true}, now)
			return page, object, err
		},
		Aggregate: func(q agentowner.ObjectQuery, now time.Time) (any, *kernel.Error) {
			return h.t.aggregateObjectSet(reader, objects.ObjectSetRequest{Set: q.Set, Sort: q.Sort, Offset: q.Offset, Limit: q.Limit,
				Groups: q.Groups, Measures: q.Measures, Window: q.Window, Histogram: q.Histogram, MaxRows: q.MaxRows, Collection: q.Collection, Format: q.Format}, now)
		},
	}.ObserveRaw(op, raw, now)
}

func (h hostView) AgentSources(value any) []string { return h.t.refsIn(value) }
func (h hostView) AgentKnowledge(reader *platform.Member, app, q string, limit int, now time.Time) json.RawMessage {
	return agentowner.KnowledgeObservation(h.t.Knowledge(aiContextReader(reader), app, q, limit, now))
}
func (h hostView) AgentAppReader(app string) *platform.Member {
	return &platform.Member{ID: "app:" + app, Tenant: h.t.ID, Roles: map[string]string{app: platform.AnyMember}}
}
func (h hostView) AgentMeaning(app string) string       { return h.t.meaning(app) }
func (h hostView) AgentGlossary(app string) string      { return h.t.glossary(app) }
func (h hostView) AgentLanguage(language string) string { return agents.LanguageName(language) }
func (h hostView) AgentSuspended(member string) bool    { return h.t.suspended(member) }
func (h hostView) ExternalAgentMembers() []platform.Member {
	if h.t.identityOwner == nil {
		return nil
	}
	return h.t.identityOwner.AgentMembers()
}
func (h hostView) AgentDecisionMembers() []string {
	return agentowner.DecisionMembers(h.t.Audit())
}
func (h hostView) readAgent(read func(*Tenant) *kernel.Error) *kernel.Error {
	if h.decision != nil {
		return read(h.t)
	}
	_, refusal := recordRead(h.t, context.Background(), func(t *Tenant) (struct{}, *kernel.Error) { return struct{}{}, read(t) })
	return refusal
}

func (h hostView) AgentUsage() (values []platform.AIUsage) {
	h.readAgent(func(t *Tenant) *kernel.Error {
		if t.ai != nil {
			values = t.ai.Usage()
		}
		return nil
	})
	return values
}

func (h hostView) WithAgentRead(read func(platform.AgentEvaluationRuntime) *kernel.Error) *kernel.Error {
	_, refusal := recordRead(h.t.readOriginOrSelf(), context.Background(), func(t *Tenant) (struct{}, *kernel.Error) {
		if t.agents == nil {
			return struct{}{}, unknown()
		}
		return struct{}{}, read(t.agents.AgentEvaluationRuntime())
	})
	return refusal
}

func (h hostView) ResolveAgentModel(name string, options platform.ModelOptions, stage string) (binding platform.AIModelBinding, provider platform.AIProvider, refusal *kernel.Error) {
	refusal = h.readAgent(func(t *Tenant) (failure *kernel.Error) {
		binding, provider, failure = t.modelResolver().ResolveStaged("agent", name, options, stage)
		return failure
	})
	return binding, provider, refusal
}
func (h hostView) AgentModel(name string) (model platform.AIModel, provider platform.AIProvider, refusal *kernel.Error) {
	refusal = h.readAgent(func(t *Tenant) (failure *kernel.Error) {
		if t.ai == nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "AI capability unavailable")
		}
		model, provider, failure = t.ai.Model(name)
		return failure
	})
	return model, provider, refusal
}

func (h hostView) AgentProviderAvailable(provider string, now time.Time) bool {
	return h.t.breakers.Allow("ai:"+provider, now, false)
}
func (h hostView) CallAgentModel(provider platform.AIProvider, model platform.AIModel, member platform.Member, request platform.ChatRequest, now time.Time) (platform.ChatAnswer, *platform.AIError) {
	return aigateway.AgentCall{
		Authorize: func(member platform.Member, request platform.ChatRequest) *platform.AIError {
			_, refusal := recordRead(h.t, context.Background(), func(t *Tenant) (struct{}, *kernel.Error) {
				return struct{}{}, t.modelAllowed(platform.AgentApp, member, request.Model)
			})
			if refusal != nil {
				return &platform.AIError{Status: 403, Detail: refusal.Message}
			}
			return nil
		},
		Call: h.t.call,
		Answer: func(request platform.ChatRequest, answer platform.ChatAnswer) platform.ChatAnswer {
			_, _ = recordRead(h.t, context.Background(), func(t *Tenant) (struct{}, *kernel.Error) {
				if value, ok := t.held(platform.RunType + "/" + request.Run); ok {
					if run, ok := value.(platform.AgentRunRecord); ok {
						answer.Usage = agentowner.RunUsage(answer.Usage, run)
					}
				}
				return struct{}{}, nil
			})
			return answer
		},
	}.Run(provider, model, member, request, now)
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

func (h hostView) AgentEvaluationApp(candidate, agent string) (platform.App, string, error) {
	return h.t.candidateEvaluation().App(candidate, agent)
}

func (t *Tenant) candidateEvaluation() agentowner.CandidateEvaluation {
	return agentowner.CandidateEvaluation{Builder: build.ID, Candidate: t.releases.Candidate,
		Owner: func(app string) (platform.ReleaseExecutionOwner, bool) {
			owner, ok := t.App(app).(platform.ReleaseExecutionOwner)
			return owner, ok
		}}
}

func (t *Tenant) evaluateReleaseAgentLocked(m platform.Member, request releaseowner.ReleaseEvaluationRequest, now time.Time) (string, error) {
	return agentowner.ReleaseRunner{Tenant: t.ID, Candidate: t.candidateEvaluation(), Agent: t.agents != nil,
		Existing: func(id string) (platform.Evaluation, bool) {
			return t.evaluation(id)
		},
		DefaultModel: func() string { return t.setting(t.automation(platform.AgentApp, false), platform.SettingAgentModel) },
		Resolve: func(model string, options platform.ModelOptions) (string, *kernel.Error) {
			binding, _, problem := t.modelResolver().Resolve("agent", model, options)
			return binding.Model, problem
		},
		Accept: func(sub *pb.Submission) *kernel.Error {
			_, refusal := t.accept(t.agents, m, sub, now, acceptOptions{automation: true})
			return refusal
		},
	}.Start(m, request.CandidateID, request.Agent, request.Key)
}

func (t *Tenant) suspended(member string) bool {
	v, ok := t.held(platform.SwitchType + "/" + member)
	return ok && v.(platform.Switch).Suspended
}

func (t *Tenant) agentRun(id string) (platform.AgentRunRecord, bool) {
	return platform.Get[platform.AgentRunRecord](t.automation(platform.AgentApp, false), id)
}

func (t *Tenant) agentSchedule() agentowner.Schedule {
	return agentowner.Schedule{Agents: t.agents, Stopped: t.businessStopped, Quarantined: t.quarantined, Recording: t.Recording,
		Read: func(read func(agentowner.ScheduleView) *kernel.Error) *kernel.Error {
			_, refusal := recordRead(t, context.Background(), func(t *Tenant) (struct{}, *kernel.Error) {
				return struct{}{}, read(agentowner.ScheduleView{Agents: t.agents, Run: t.agentRun, Evaluation: t.evaluation,
					Queued: func() ([]platform.Evaluation, *kernel.Error) {
						evals, _, refusal := platform.Find[platform.Evaluation](t.automation(platform.AgentApp, false), platform.Query{Domain: json.RawMessage(`[["state","=","queued"]]`), Sort: []string{"id"}, Limit: 1})
						return evals, refusal
					}})
			})
			return refusal
		},
		Call: t.call, Meter: t.meter, Step: t.applyAgentStep, Wake: wakeWork, Run: t.agentRun, Evaluation: t.evaluation, Accept: t.acceptAgentStep,
		Failed: func(at time.Time, run, detail string) {
			t.observeOwnerRun(at, "agent", run, platform.AgentApp, "", "failed", "Agent step", detail)
		},
		Evaluator: t.agentEvaluator,
	}
}

func (t *Tenant) applyAgentStep(b platform.AgentResultStep, now time.Time) bool {
	applied, refusal := decideOnImage(t, context.Background(), "agent:settle", func(t *Tenant) (bool, *kernel.Error) {
		return t.agentSchedule().Prepare(b, now), nil
	}, func(t *Tenant, candidate preparedAccepted, applied bool, refusal *kernel.Error) (bool, *kernel.Error) {
		if len(candidate.entry.Body) != 0 {
			t.finishAgentStep(candidate)
		}
		return applied && !t.quarantined(), refusal
	})
	return applied && refusal == nil
}

func (t *Tenant) evaluation(id string) (platform.Evaluation, bool) {
	return platform.Get[platform.Evaluation](t.automation(platform.AgentApp, false), id)
}

func (t *Tenant) Evaluate(now time.Time) {
	if t.agents == nil || t.ai == nil {
		return
	}
	t.agentSchedule().Drain(now)
}

func (h hostView) AgentModelConfigurationDigest(model platform.AIModel, provider platform.AIProvider, binding *platform.AIModelBinding) (string, error) {
	return aigateway.ConfigurationDigest(model, provider, binding)
}

func (h hostView) AgentAnswerText(outcome platform.Outcome) string { return actions.AnswerOf(outcome) }

func (t *Tenant) acceptAgentStep(b platform.AgentResultStep, at time.Time) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("agent answer cannot stage: %v", p)
		}
	}()
	draft := t.newStagedDecision()
	draft.At = at
	return agentowner.AgentAcceptance{Tenant: t.ID, Agent: draft.stateApp(t.agents).(platform.AgentRuntime), Draft: draft,
		RunAgent: func(run string) string {
			record, _ := t.agentRun(run)
			return record.Agent
		},
		UsageBefore: t.usagePredecessor, Answer: draft.answerRequests, Observers: draft.stageObservers,
		Failure: func() *kernel.Error { return draft.Failure },
		Events:  func() []platform.Event { return draft.Events },
		Changes: func(last platform.Event, at time.Time) ([]byte, error) {
			return draft.batchResult(last.App, last.Record.GetSubmission(), last.Record, at)
		},
		Prepared: func(body, principal []byte, key, hash string) {
			t.stageCandidate(draft, platform.AgentApp, body, principal, key, hash, at, t.finishAgentStep)
		}}.Accept(b, at)
}

func (t *Tenant) finishAgentStep(candidate preparedAccepted) {
	runtimekernel.AgentFinish{Settlement: t.settlement(),
		Commit: func() ([]byte, error) { return t.acceptResult(candidate.entry, candidate.key, candidate.hash) },
		Apply: func(committed []byte) error {
			_, err := t.applyAcceptedAgent(committed)
			return err
		}}.Finish(candidate.key, candidate.hash)
}

func (t *Tenant) applyAcceptedAgent(raw []byte) (acceptedformat.RuntimeAgent, error) {
	h := runtimekernel.AgentApply{Usage: t.usage(), Batch: t.batchApply(), Published: t.publishAcceptedBatch}
	if t.agents != nil {
		h.Ledger = t.agents.AcceptedLedger()
	}
	return t.ApplyAgentResult(raw, h)
}
