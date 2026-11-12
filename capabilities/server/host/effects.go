package host

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"net/http"
	"os"
	"path/filepath"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	acceptedformat "platformserver/kernel/acceptedformat"
	"platformserver/platform"
	"platformserver/services/actions"
	"platformserver/services/data/transform"
	"platformserver/services/resources/authoring"
	controlowner "platformserver/services/security/control"
	"slices"
	"strings"
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
	a := t.App(c.App)
	if a == nil || key == "" {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	i := slices.IndexFunc(a.Manifest().Emits, func(e platform.EffectKind) bool { return e.Name == kind })
	if i < 0 {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	typ, _, _ := strings.Cut(entity, "/")
	resource := platform.AssetRef{App: c.App, Kind: platform.AssetObject, Name: typ}
	if typ == "" {
		resource = platform.AssetRef{App: c.App, Kind: platform.AssetApp, Name: c.App}
	}
	applications, err := t.bindApplications(resource, t.releases.Active(), "")
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The effect application cannot be bound")
	}
	agent, run := "", ""
	if c.Agent {
		agent, run = c.ID, t.agentRun
	}
	body, err := json.Marshal(map[string]any{"type": c.App + "/" + kind, "timestamp": now, "data": data})
	if err != nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	exists := func(id string) bool {
		return t.outbound.Any(func(x actions.Queued) bool { return x.ID == id }) || slices.ContainsFunc(staged, func(x platform.Effect) bool { return x.ID == id })
	}
	planned := actions.AppEffects(t.endpoints.All(), platform.Effect{Applications: applications, App: c.App, Key: key, Target: entity, At: now, Agent: agent, Run: run, Due: now, Body: string(body)},
		t.ID, kind, c.Agent && a.Manifest().Emits[i].Irreversible, c.Agent, exists)
	return planned, nil
}

func (t *Tenant) askHeldEffects(c platform.Caller, kind, entity string, planned []platform.Effect, now time.Time) {
	a := t.App(c.App)
	i := slices.IndexFunc(a.Manifest().Emits, func(e platform.EffectKind) bool { return e.Name == kind })
	console := t.automated(c, controlowner.PlatformApp)
	for _, x := range planned {
		if x.State != "held" {
			continue
		}
		console.Notify(platform.Notification{Title: fmt.Sprintf("Approve %s for %s", a.Manifest().Emits[i].Title, entity),
			Body: fmt.Sprintf("The AI agent %s caused it, and it cannot be recalled once sent. Approve or discard it in Settings → Integrations.", c.ID),
			Ref:  controlowner.EffectType + "/" + x.ID, Key: "approve:" + x.ID}, now, platform.Recipient{AppRole: controlowner.Admin})
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
	if t.Secrets != nil {
		return t.Secrets(name)
	}
	if dir := os.Getenv("PLATFORM_SECRETS_DIR"); dir != "" {
		if raw, err := os.ReadFile(filepath.Join(dir, filepath.Base(name))); err == nil {
			return bytes.TrimSpace(raw), true
		}
	}
	v, ok := os.LookupEnv("PLATFORM_SECRET_" + strings.ToUpper(strings.NewReplacer("-", "_", ".", "_").Replace(name)))
	return []byte(v), ok && v != ""
}

func (t *Tenant) Dispatch(now time.Time) { integrations.OnLane(t.dispatches(now)) }

func (t *Tenant) dispatches(now time.Time) []func() {
	if t.businessStopped() {
		return nil
	}
	type job struct {
		effect   platform.Effect
		endpoint actions.Endpoint
		span     trace.SpanContext
	}
	var jobs []job
	endpoints := append(t.endpoints.All(), t.connectionEndpoints()...)
	for _, ep := range append(endpoints, actions.Endpoint{ID: acceptedformat.ModelEndpoint, Kind: acceptedformat.ModelEndpoint}) {
		if ep.Kind != acceptedformat.ModelEndpoint && !t.breakers.Allow("endpoint:"+ep.ID, now, true) {
			continue
		}
		if x, ok := t.outbound.Claim(ep.ID, now); ok {
			jobs = append(jobs, job{x.Effect, ep, x.Span})
		}
	}
	var sends []func()
	for _, j := range jobs {
		sends = append(sends, func() {
			if t.businessStopped() {
				return
			}
			if !t.beginDispatchAttempt(j.effect, now) {
				return
			}
			span := outside(j.span, "send "+j.effect.Event+" to "+j.endpoint.ID, attribute.String("platform.tenant", t.ID),
				attribute.String("platform.endpoint", j.endpoint.ID), attribute.String("platform.effect", j.effect.ID))
			outcome, usage := t.sendWithUsage(j.endpoint, j.effect, now)
			end(span, map[bool]string{true: "ok", false: outcome.Result + ": " + outcome.Detail}[outcome.Result == "delivered"])
			if t.quarantined() {
				return
			}
			counted(t, "effect", j.effect.App, outcome.Result)
			if j.endpoint.Kind != acceptedformat.ModelEndpoint {
				t.breakers.Report("endpoint:"+j.endpoint.ID, outcome.Result != "retry", now)
			}
			t.settleWithUsage(j.effect.ID, outcome, usage, now)
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
