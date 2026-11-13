package host

import (
	"context"
	"go.opentelemetry.io/otel/attribute"
	"net/http"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	acceptedformat "platformserver/kernel/acceptedformat"
	"platformserver/platform"
	"platformserver/services/actions"
	"platformserver/services/data/transform"
	"platformserver/services/resources/authoring"
	controlowner "platformserver/services/security/control"
	"time"
)

const bodyKept = 30 * 24 * time.Hour

func (t *Tenant) emit(e platform.Event, names []string) {
	planned, _ := t.eventEffects(e, names, false)
	t.outbound.Add(t.current(), planned...)
}

func (t *Tenant) eventEffects(e platform.Event, names []string, preparing bool) ([]platform.Effect, *kernel.Error) {
	planned, refusal := t.writebackEffects(e, preparing)
	if refusal != nil {
		return nil, refusal
	}
	planned = append(planned, actions.EventEffects(t.ID, t.endpoints.All(), e, names)...)
	return planned, nil
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
	return actions.AppEffectPlan{Tenant: t.ID, App: t.App(c.App), Endpoints: t.endpoints.All(), Run: t.agentRun,
		Bind: func(resource platform.AssetRef) ([]platform.AssetBinding, error) {
			return t.bindApplications(resource, t.releases.Active(), "")
		},
		Exists: func(id string) bool { return t.outbound.Any(func(x actions.Queued) bool { return x.ID == id }) }}.Plan(c, kind, key, entity, data, now, staged)
}

func (t *Tenant) askHeldEffects(c platform.Caller, kind, entity string, planned []platform.Effect, now time.Time) {
	console := t.automated(c, controlowner.PlatformApp)
	for _, n := range actions.HeldNotices(t.App(c.App), kind, entity, c.ID, planned, controlowner.EffectType) {
		console.Notify(n, now, platform.Recipient{AppRole: controlowner.Admin})
	}
}

func (t *Tenant) Effects(now time.Time) []platform.Effect {
	all := t.outbound.Select(nil)
	out := []platform.Effect{}
	for i := len(all) - 1; i >= 0; i-- {
		x := all[i].Effect
		if now.Sub(x.At) > bodyKept && x.State != "unknown" {
			x.Body = ""
		}
		out = append(out, x)
	}
	return out
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
	var sends []func()
	for _, j := range actions.ClaimJobs(&t.outbound, append(t.endpoints.All(), t.connectionEndpoints()...), acceptedformat.ModelEndpoint,
		func(id string) bool { return t.breakers.Allow("endpoint:"+id, now, true) }, now) {
		sends = append(sends, func() {
			if t.businessStopped() {
				return
			}
			if !t.beginDispatchAttempt(j.Effect, now) {
				return
			}
			span := outside(j.Span, "send "+j.Effect.Event+" to "+j.Endpoint.ID, attribute.String("platform.tenant", t.ID),
				attribute.String("platform.endpoint", j.Endpoint.ID), attribute.String("platform.effect", j.Effect.ID))
			outcome, usage := t.sendWithUsage(j.Endpoint, j.Effect, now)
			end(span, map[bool]string{true: "ok", false: outcome.Result + ": " + outcome.Detail}[outcome.Result == "delivered"])
			if t.quarantined() {
				return
			}
			counted(t, "effect", j.Effect.App, outcome.Result)
			if j.Endpoint.Kind != acceptedformat.ModelEndpoint {
				t.breakers.Report("endpoint:"+j.Endpoint.ID, outcome.Result != "retry", now)
			}
			t.settleWithUsage(j.Effect.ID, outcome, usage, now)
		})
	}
	return append(sends, t.operationDispatches(now)...)
}

func (t *Tenant) sendWithUsage(ep actions.Endpoint, x platform.Effect, now time.Time) (platform.Outcome, *platform.AIUsage) {
	if ep.Kind == acceptedformat.ModelEndpoint {
		return t.sendModel(x, now)
	}
	return t.send(ep, x, now), nil
}

func (t *Tenant) send(ep actions.Endpoint, x platform.Effect, now time.Time) platform.Outcome {
	if ep.Kind == acceptedformat.ModelEndpoint {
		out, _ := t.sendModel(x, now)
		return out
	}
	if ep.Kind == "email" {
		return t.transport().Mail(ep, x, now)
	}
	if ep.Kind == "a2a" {
		return t.transport().A2A(ep, x, false)
	}
	if ep.Kind == "connection" {
		return t.dataClient().Writeback(x)
	}
	return t.transport().Webhook(ep, x, now)
}

func (t *Tenant) settle(effect string, o platform.Outcome, now time.Time) {
	t.settleWithUsage(effect, o, nil, now)
}

func (t *Tenant) settleWithUsage(effect string, o platform.Outcome, usage *platform.AIUsage, now time.Time) {
	_, _ = decideOnImage(t, context.Background(), "effect:settle", func(t *Tenant) (struct{}, *kernel.Error) {
		t.prepareEffectSettlement(effect, o, usage, now)
		return struct{}{}, nil
	}, func(t *Tenant, candidate preparedAccepted, value struct{}, refusal *kernel.Error) (struct{}, *kernel.Error) {
		if len(candidate.entry.Body) != 0 {
			t.finishEffect(effect, candidate)
		}
		return value, refusal
	})
}

func (t *Tenant) prepareEffectSettlement(effect string, o platform.Outcome, usage *platform.AIUsage, now time.Time) {
	if t.quarantined() {
		return
	}
	if o.Generation != 0 && !t.operationCurrent(effect, o.Generation) {
		return
	}
	if o.Generation != 0 && o.Result == "delivered" {
		current, _ := t.outbound.Get(effect)
		if !t.operationAllowed(current.Effect, now) {
			o.Result, o.Detail, o.Answer, o.Digest = "rejected", "Operation permission or source access was revoked", nil, ""
		}
	}
	t.settleAccepted(effect, o, usage, now)
}

func (t *Tenant) told(x platform.Effect, o platform.Outcome, now time.Time, replaying bool) {
	if t.controlAnswer != nil {
		t.controlAnswer(x, o, now)
		return
	}
	if x.Endpoint == acceptedformat.ModelEndpoint {
		return
	}
	if a, ok := t.App(x.App).(platform.Answerer); ok && x.App != "" {
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

func (t *Tenant) decideEndpoint(c platform.Caller, s *pb.Submission, now time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	if s.GetSchema().GetName() == controlowner.SchemaEndpointRemove {
		return t.effectControl().RemoveEndpoint(c, s.GetTarget().GetId(), now)
	}
	return t.effectControl().AddEndpoint(s.GetTarget().GetId(), s.GetPayload())
}

func (t *Tenant) decideEffect(c platform.Caller, s *pb.Submission, now time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	control, id := t.effectControl(), s.GetTarget().GetId()
	switch s.GetSchema().GetName() {
	case controlowner.SchemaEffectApprove:
		return control.Approve(c, id, now)
	case controlowner.SchemaEffectDiscard:
		return control.Discard(c, id, now)
	case controlowner.SchemaEffectResolve:
		return control.Resolve(c, id, s.GetPayload(), now)
	}
	return control.Retry(id, now)
}

func guarded(req *http.Request, allowPrivate bool) (*http.Response, error) {
	dialer := actions.GuardedDialer(allowPrivate)
	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{DialContext: dialer.DialContext, Proxy: nil},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(req)
	return controlowner.RedactCredentialFailure(response, req), err
}
