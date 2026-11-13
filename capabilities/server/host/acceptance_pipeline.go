package host

import (
	"errors"
	"fmt"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	runtimekernel "platformserver/kernel"
	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
	"platformserver/services/actions"
	"platformserver/services/ai/gateway"
	agentowner "platformserver/services/ai/runtime"
	"platformserver/services/functions/runtime"
	controlowner "platformserver/services/security/control"
	"slices"
	"time"
)

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
			record, _ := platform.Get[platform.AgentRunRecord](t.automation(platform.AgentApp, false), run)
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

func (t *Tenant) applyAcceptedEffect(raw []byte) (acceptedformat.Effect, bool, error) {
	h := runtimekernel.EffectApply{Changes: t.changeApply(), Finish: t.operationClaims().PrepareFinish, Published: t.publishAcceptedChanges, Current: t.outbound.State,
		Settle: func(id string, after acceptedformat.EffectState, finished *pb.Work) {
			t.outbound.Settled(id, after)
			if finished != nil {
				runtimekernel.InstallWork(t.Works, finished)
			}
		}}
	if t.ai != nil {
		h.Usage, h.Meter = t.ai.Snapshot, t.ai.Meter
	}
	return t.ApplyEffectResult(raw, h)
}

func (t *Tenant) settleAccepted(id string, out platform.Outcome, usage *platform.AIUsage, at time.Time) {
	var draft *stagedDecision
	settlement := actions.EffectSettlement{Tenant: t.ID, App: controlowner.PlatformApp, Queued: t.outbound.Get, Usage: aigateway.EffectUsage,
		NoReviewer: func() bool {
			return t.tasks == nil || len((hostView{t: t}).Holding(controlowner.PlatformApp, controlowner.Admin)) == 0
		},
		Finish: t.operationClaims().PrepareFinish,
		Failed: func(before acceptedformat.EffectState, err error) {
			t.observeOwnerRun(at, "effect", id, before.App, before.Endpoint, "failed", "Effect answer", err.Error())
		},
		Stage: func(result *acceptedformat.Effect, before acceptedformat.EffectState, after platform.Effect, out platform.Outcome, usage *platform.AIUsage) (err error) {
			draft, err = t.stageEffectResult(result, before, after, out, usage, at)
			return err
		},
		Quarantined: t.quarantined,
		Reset:       func() { draft = t.newStagedDecision(); draft.At = at },
		Principal:   func() platform.Member { return t.automation(controlowner.PlatformApp, false).Member },
		Prepared: func(body, principal []byte, key, hash string) {
			t.stageCandidate(draft, controlowner.PlatformApp, body, principal, key, hash, at, func(candidate preparedAccepted) { t.finishEffect(id, candidate) })
		}}
	if t.ai != nil {
		settlement.Snapshot = t.ai.Snapshot
	}
	settlement.Settle(id, out, usage, at)
}

func (t *Tenant) stageEffectResult(result *acceptedformat.Effect, before acceptedformat.EffectState, after platform.Effect, out platform.Outcome, usage *platform.AIUsage, at time.Time) (*stagedDecision, error) {
	draft := t.newStagedDecision()
	draft.At = at
	if after.State == "failed" && t.App(controlowner.PlatformApp) != nil {
		admin := platform.NewCaller(draft, platform.Member{ID: "app:" + controlowner.PlatformApp, Tenant: t.ID}, controlowner.PlatformApp, false, true)
		admin.Notify(platform.Notification{Title: "Effect to " + after.Endpoint + " failed: " + after.Event,
			Body: after.Error + ". Retry it in Settings → Automation once the cause is fixed.",
			Key:  "failed:" + after.ID}, at, platform.Recipient{AppRole: controlowner.Admin})
	}
	if (platform.EffectSettled(after.State) || after.State == "unknown") && before.State != "discarded" {
		if err := t.stageEffectAnswer(draft, after, out, usage, at); err != nil {
			if t.quarantined() {
				return draft, err
			}
			result.CallbackError = err.Error()
			result.Authorizations = slices.Clone(draft.Authorizations)
			draft = t.newStagedDecision()
			draft.At = at
		}
	}
	if before.Endpoint == acceptedformat.ModelEndpoint && (after.State == "rejected" || after.State == "failed" || after.State == "unknown") {
		recipients := (hostView{t: t, app: t.App(before.App)}).Holding(controlowner.PlatformApp, controlowner.Admin)
		if len(recipients) > 0 && t.tasks != nil {
			caller := platform.NewCaller(draft, platform.Member{ID: "app:" + before.App, Tenant: t.ID}, before.App, false, true)
			if failure := aigateway.StopHandoff(caller, t.App(before.App).AcceptedLedger().RecordsFor(t.ID), before, out.Detail, recipients); failure != nil {
				draft.Failure = failure
			}
		}
	}
	err := draft.EffectResult(draft.sources(), result, before.App, at, func(plan acceptedformat.ChangePlan) error {
		_, err := t.prepareAcceptedChanges(plan)
		return err
	})
	return draft, err
}

func (t *Tenant) finishEffect(id string, candidate preparedAccepted) {
	runtimekernel.EffectFinish{Settlement: t.settlement(),
		Commit:   func() ([]byte, error) { return t.acceptResult(candidate.entry, candidate.key, candidate.hash) },
		Conflict: func(err error) bool { return errors.Is(err, errAcceptedConflict) },
		Release:  func() { t.outbound.Update(id, func(x *actions.Queued) { x.Sending = false }) },
		Apply:    t.applyAcceptedEffect}.Finish(id, candidate.key, candidate.hash)
}

func (t *Tenant) stageEffectAnswer(draft *stagedDecision, x platform.Effect, out platform.Outcome, usage *platform.AIUsage, at time.Time) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("unsupported effect callback: %v", p)
		}
	}()
	return actions.EffectAnswer{Tenant: t.ID, Draft: draft,
		Authorize: func(c platform.Caller, permission string) *kernel.Error {
			return draft.authorizeNative(c, permission, "action", nil, t.App(c.App) != nil)
		},
		Model: func() (platform.App, *pb.Submission) { return t.modelAnswerSubmission(x, out, usage) },
		Decide: func(c platform.Caller, a platform.App, s *pb.Submission, at time.Time) *kernel.Error {
			_, refusal := draft.Decide(c, a, s, at)
			return refusal
		},
		Operation: func(x platform.Effect) {
			draft.OperationAnswers = map[string]platform.OperationResult{x.ID: compute.ResultOf(x)}
		},
		Completion: func() platform.OperationCompletion {
			owner := t.procs
			if app, ok := t.procs.(platform.App); ok {
				owner = draft.stateApp(app).(platform.Processes)
			}
			completion, _ := owner.(platform.OperationCompletion)
			return completion
		},
		Answerer: func(app string) platform.Answerer {
			if t.answerers[app] == nil {
				return nil
			}
			return draft.stateApp(t.App(app)).(platform.Answerer)
		},
		Source: t.outbound.Get,
		Origin: draft.AnswerWithOrigin,
		Agents: func(c platform.Caller, x platform.Effect, out platform.Outcome, at time.Time) {
			if t.agents != nil {
				draft.stateApp(t.agents).(platform.AgentRuntime).AgentEffectEnded(t.automated(c, platform.AgentApp), nil, x, out, at)
			}
		},
		Finish: func(at time.Time) *kernel.Error {
			if draft.Failure != nil {
				return draft.Failure
			}
			if err := draft.answerRequests(at); err != nil {
				return err
			}
			return draft.stageObservers()
		}}.Stage(x, out, at)
}

func (d *stagedDecision) answerRequests(now time.Time) *kernel.Error {
	return actions.RequestAnswers{Tenant: d.tenant.ID,
		Pending: func() bool { return len(d.requests) > 0 },
		Pop: func() actions.PendingRequest {
			q := d.requests[0]
			d.requests = d.requests[1:]
			return q
		},
		Begin: func(name, app, target string) func(string) {
			return d.tenant.begin(name, trace.SpanContext{}, attribute.String("platform.app", app), attribute.String("platform.target", target))
		},
		Attempt:   d.Attempt,
		Router:    func() actions.Router { return d.routingTenant().router() },
		Automated: d.tenant.automated,
		Reply: func(c platform.Caller, reply *pb.Submission, at time.Time) *kernel.Error {
			_, err := platform.Decide(c, d.tenant.App(c.App), reply, at)
			return err
		},
		Failure:     func() *kernel.Error { return d.Failure },
		Unsupported: unsupportedStagedEffect}.Run(now)
}
