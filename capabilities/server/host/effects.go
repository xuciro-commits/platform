package host

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	runtimekernel "platformserver/kernel"
	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
	"platformserver/services/actions"
	"platformserver/services/ai/gateway"
	dataowner "platformserver/services/data"
	"platformserver/services/data/transform"
	"platformserver/services/functions/runtime"
	"platformserver/services/resources/authoring"
	"platformserver/services/resources/monitoring"
	controlowner "platformserver/services/security/control"
)

func (t *Tenant) eventEffects(e platform.Event, names []string, preparing bool) ([]platform.Effect, *kernel.Error) {
	planned, refusal := t.writebackEffects(e, preparing)
	if refusal != nil {
		return nil, refusal
	}
	planned = append(planned, actions.EventEffects(t.ID, t.endpoints.All(), e, names)...)
	return planned, nil
}

func (t *Tenant) emit(e platform.Event, names []string) {
	planned, _ := t.eventEffects(e, names, false)
	t.outbound.Add(t.current(), planned...)
}

func (t *Tenant) emitFor(c platform.Caller, kind, key, entity string, data any, now time.Time) (int, *kernel.Error) {
	planned, err := t.planAppEffects(c, kind, key, entity, data, now, nil)
	if err != nil {
		return 0, err
	}
	t.installAcceptedIntents(planned)
	t.askHeldEffects(c, kind, entity, planned, now)
	return len(planned), nil
}

func (t *Tenant) planAppEffects(c platform.Caller, kind, key, entity string, data any, now time.Time, staged []platform.Effect) ([]platform.Effect, *kernel.Error) {
	return actions.AppEffectPlan{Tenant: t.ID, App: t.App(c.App), Endpoints: t.endpoints.All(), Run: c.AgentRun(),
		Bind: func(resource platform.AssetRef) ([]platform.AssetBinding, error) {
			return t.bindApplications(resource, t.releases.Active(), "")
		},
		Exists: t.outbound.Has}.Plan(c, kind, key, entity, data, now, staged)
}

func (t *Tenant) askHeldEffects(c platform.Caller, kind, entity string, planned []platform.Effect, now time.Time) {
	console := t.automated(c, controlowner.PlatformApp)
	for _, n := range actions.HeldNotices(t.App(c.App), kind, entity, c.ID, planned, controlowner.EffectType) {
		console.Notify(n, now, platform.Recipient{AppRole: controlowner.Admin})
	}
}

func (t *Tenant) Effects(now time.Time) []platform.Effect {
	return actions.RedactedEffects(t.outbound.Select(nil), now)
}

func (t *Tenant) Endpoints() []actions.EndpointView {
	return actions.EndpointHealth(t.endpoints.All(), t.outbound.Select(nil), func(ep actions.Endpoint) bool {
		_, ok := t.CredentialValue(ep.Secret, "endpoint/"+ep.ID, ep.URL)
		return !ok && ep.Secret != ""
	})
}

func (t *Tenant) secret(name string) ([]byte, bool) {
	return controlowner.LookupSecret(t.Secrets, name)
}

func (t *Tenant) Dispatch(now time.Time) { integrations.OnLane(t.dispatches(now)) }

func (t *Tenant) dispatches(now time.Time) []func() {
	if t.businessStopped() {
		return nil
	}
	sends := actions.DispatchRound{Model: acceptedformat.ModelEndpoint, Stopped: t.businessStopped, Quarantined: t.quarantined,
		Allow: func(id string) bool { return t.breakers.Allow("endpoint:"+id, now, true) },
		Begin: func(x platform.Effect) bool { return t.beginDispatchAttempt(x, now) },
		Span: func(j actions.DispatchJob) func(string) {
			span := monitoring.Outside(j.Span, "send "+j.Effect.Event+" to "+j.Endpoint.ID, attribute.String("platform.tenant", t.ID),
				attribute.String("platform.endpoint", j.Endpoint.ID), attribute.String("platform.effect", j.Effect.ID))
			return func(outcome string) { monitoring.End(span, outcome) }
		},
		Count: func(app, result string) { monitoring.Counted(t.ID, "effect", app, result) },
		Send: func(ep actions.Endpoint, x platform.Effect) (platform.Outcome, *platform.AIUsage) {
			return t.sendWithUsage(ep, x, now)
		},
		Report: func(id string, ok bool) { t.breakers.Report("endpoint:"+id, ok, now) },
		Settle: func(id string, o platform.Outcome, usage *platform.AIUsage) { t.settleWithUsage(id, o, usage, now) },
	}.Sends(&t.outbound, append(t.endpoints.All(), actions.ConnectionEndpoints(t.outbound.Select(nil), dataowner.WritebackEndpoint)...), now)
	for _, id := range actions.RunnableIDs(t.outbound.Select(func(x actions.Queued) bool { return x.Endpoint == acceptedformat.OperationEndpoint }), now, 4) {
		if x, ok := t.claimOperation(id, now); ok {
			sends = append(sends, func() { out := t.executeOperation(x, now); t.settleWithUsage(x.ID, out, nil, time.Now().UTC()) })
		}
	}
	return sends
}

func (t *Tenant) sendWithUsage(ep actions.Endpoint, x platform.Effect, now time.Time) (platform.Outcome, *platform.AIUsage) {
	switch ep.Kind {
	case acceptedformat.ModelEndpoint:
		return t.gatewayRuntime().Send(x, now)
	case "email":
		return t.transport().Mail(ep, x, now), nil
	case "a2a":
		return t.transport().A2A(ep, x, false), nil
	case "connection":
		return t.dataClient().Writeback(x), nil
	}
	return t.transport().Webhook(ep, x, now), nil
}

func (t *Tenant) settle(effect string, o platform.Outcome, now time.Time) {
	t.settleWithUsage(effect, o, nil, now)
}

func (t *Tenant) settleWithUsage(effect string, o platform.Outcome, usage *platform.AIUsage, now time.Time) {
	_, _ = decideOnImage(t, context.Background(), "effect:settle", func(t *Tenant) (struct{}, *kernel.Error) {
		if t.quarantined() || o.Generation != 0 && !t.operationClaims().Current(effect, o.Generation) {
			return struct{}{}, nil
		}
		o = t.operationClaims().Revalidated(effect, o, func(x platform.Effect) bool { return t.operationAllowed(x, now) })
		t.settleAccepted(effect, o, usage, now)
		return struct{}{}, nil
	}, func(t *Tenant, candidate preparedAccepted, value struct{}, refusal *kernel.Error) (struct{}, *kernel.Error) {
		if len(candidate.entry.Body) != 0 {
			t.finishEffect(effect, candidate)
		}
		return value, refusal
	})
}

func (t *Tenant) told(x platform.Effect, o platform.Outcome, now time.Time, replaying bool) {
	if t.controlAnswer != nil {
		t.controlAnswer(x, o, now)
		return
	}
	if x.Endpoint == acceptedformat.ModelEndpoint {
		return
	}
	if a := t.answerers[x.App]; a != nil && x.App != "" {
		a.Answer(t.automation(x.App, replaying), x, o, now)
	}
}

func (t *Tenant) effectControl() actions.EffectControl {
	return actions.EffectControl{Outbox: &t.outbound, Endpoints: &t.endpoints, Agents: t.agents, Told: t.told,
		Valid: func(ep actions.Endpoint) bool {
			return ep.Valid(t.App, func(event string) bool {
				_, declared := t.router().Event(event)
				return t.App(t.owner["action:"+event]) != nil || declared
			})
		},
		Reviewed: func(x actions.Queued) bool { return x.App == build.ID && x.Event == build.ID+"/dispatch" }}
}

func guarded(req *http.Request, allowPrivate bool) (*http.Response, error) {
	dialer := actions.GuardedDialer(allowPrivate)
	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{DialContext: dialer.DialContext, Proxy: nil},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(req)
	return controlowner.RedactCredentialFailure(response, req), err
}

func (t *Tenant) applyAcceptedEffect(raw []byte) (acceptedformat.Effect, bool, error) {
	h := runtimekernel.EffectApply{Changes: t.changeApply(), Finish: t.operationClaims().PrepareFinish, Publish: t.publishAccepted, Current: t.outbound.State,
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
	reset := func() { draft = t.newStagedDecision(); draft.At = at }
	settlement := actions.EffectSettlement{Tenant: t.ID, App: controlowner.PlatformApp, Queued: t.outbound.Get, Usage: aigateway.EffectUsage,
		NoReviewer: func() bool {
			return t.tasks == nil || len((hostView{t: t}).Holding(controlowner.PlatformApp, controlowner.Admin)) == 0
		},
		Finish: t.operationClaims().PrepareFinish,
		Failed: func(before acceptedformat.EffectState, err error) {
			t.observeOwnerRun(at, "effect", id, before.App, before.Endpoint, "failed", "Effect answer", err.Error())
		},
		Stage: func(result *acceptedformat.Effect, before acceptedformat.EffectState, after platform.Effect, out platform.Outcome, usage *platform.AIUsage) error {
			reset()
			return actions.EffectStaging{App: controlowner.PlatformApp, Admin: controlowner.Admin, Console: t.App(controlowner.PlatformApp) != nil, At: at,
				Caller:         func(app string) platform.Caller { return draft.appCaller(app) },
				Answer:         func() error { return t.stageEffectAnswer(draft, after, out, usage, at) },
				Quarantined:    t.quarantined,
				Authorizations: func() []acceptedformat.AuthorizationEvidence { return slices.Clone(draft.Authorizations) },
				Reset:          reset,
				Handoff: func() {
					recipients := (hostView{t: t, app: t.App(before.App)}).Holding(controlowner.PlatformApp, controlowner.Admin)
					if len(recipients) == 0 || t.tasks == nil {
						return
					}
					if failure := aigateway.StopHandoff(draft.appCaller(before.App), t.App(before.App).AcceptedLedger().RecordsFor(t.ID), before, out.Detail, recipients); failure != nil {
						draft.Failure = failure
					}
				},
				Result: func() error {
					return draft.EffectResult(draft.sources(), result, before.App, at, func(plan acceptedformat.ChangePlan) error {
						_, err := t.PrepareChanges(plan, t.changeApply())
						return err
					})
				}}.Stage(result, before, after)
		},
		Quarantined: t.quarantined,
		Reset:       reset,
		Principal:   func() platform.Member { return t.automation(controlowner.PlatformApp, false).Member },
		Prepared: func(body, principal []byte, key, hash string) {
			t.stageCandidate(draft, controlowner.PlatformApp, body, principal, key, hash, at, func(candidate preparedAccepted) { t.finishEffect(id, candidate) })
		}}
	if t.ai != nil {
		settlement.Snapshot = t.ai.Snapshot
	}
	settlement.Settle(id, out, usage, at)
}

func (d *stagedDecision) appCaller(app string) platform.Caller {
	return platform.NewCaller(d, platform.Member{ID: "app:" + app, Tenant: d.tenant.ID}, app, false, true)
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
		Model: func() (platform.App, *pb.Submission) { return t.gatewayRuntime().Answer(x, out, usage) },
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
		Finish: draft.finishStaged}.Stage(x, out, at)
}

func (d *stagedDecision) finishStaged(at time.Time) *kernel.Error {
	if d.Failure != nil {
		return d.Failure
	}
	if err := d.answerRequests(at); err != nil {
		return err
	}
	return d.stageObservers()
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
