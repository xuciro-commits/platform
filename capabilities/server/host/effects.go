package host

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"net/http"
	"net/url"
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
	s := e.Record.GetSubmission()
	planned, refusal := t.writebackEffects(e, preparing)
	if refusal != nil {
		return nil, refusal
	}
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	for _, ep := range t.endpoints {
		i := slices.IndexFunc(names, func(n string) bool { return slices.Contains(ep.Events, n) })
		if i < 0 {
			continue
		}
		var payload any = string(s.GetPayload())
		if json.Valid(s.GetPayload()) {
			payload = json.RawMessage(s.GetPayload())
		}
		at := e.Record.GetRecordedTime().AsTime()
		body, _ := json.Marshal(map[string]any{"type": names[i], "timestamp": at, "data": map[string]any{
			"app": e.App, "action": s.GetSchema().GetName(), "schemaVersion": s.GetSchema().GetVersion(), "entity": target(s), "changeId": e.Record.GetChangeId(),
			"principal": s.GetPrincipalId(), "revision": e.Record.GetRevision(), "payload": payload}})
		planned = append(planned, platform.Effect{ID: fmt.Sprintf("%s:%s:%s:%s", t.ID, e.App, e.Record.GetChangeId(), ep.ID),
			Endpoint: ep.ID, Event: names[i], Target: target(s), At: at, State: "pending", Due: at, Body: string(body)})
	}
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
	name := c.App + "/" + kind
	body, err := json.Marshal(map[string]any{"type": name, "timestamp": now, "data": data})
	if err != nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	var planned []platform.Effect
	for _, ep := range t.endpoints {
		id := fmt.Sprintf("%s:%s:%s:%s:%s", t.ID, c.App, kind, key, ep.ID)
		if !slices.Contains(ep.Effects, name) || t.outbound.Any(func(x actions.Queued) bool { return x.ID == id }) ||
			slices.ContainsFunc(staged, func(x platform.Effect) bool { return x.ID == id }) {
			continue
		}
		state := "pending"
		if c.Agent && (a.Manifest().Emits[i].Irreversible || ep.Irreversible) {
			state = "held"
		}
		planned = append(planned, platform.Effect{Applications: applications, ID: id, Endpoint: ep.ID, Event: name, App: c.App, Key: key, Target: entity, At: now,
			State: state, Agent: agent, Run: run, Due: now, Body: string(body)})
	}
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

type EndpointView struct {
	actions.Endpoint
	Pending  int    `json:"pending"`
	Failing  int    `json:"failing"`
	Health   string `json:"health"`
	Delivers int    `json:"delivered"`
}

func (t *Tenant) Endpoints() []EndpointView {
	all := t.outbound.Select(nil)
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	out := []EndpointView{}
	for _, ep := range t.endpoints {
		v := EndpointView{Endpoint: *ep, Health: "ok"}
		for _, x := range all {
			switch {
			case x.Endpoint != ep.ID:
			case x.State == "delivered":
				v.Delivers++
			case !platform.EffectSettled(x.State):
				v.Pending++
				if x.State == "retrying" && v.Failing == 0 {
					v.Failing, v.Health = x.Attempts, "failing"
				}
			}
		}
		if _, ok := t.CredentialValue(ep.Secret, "endpoint/"+ep.ID, ep.URL); !ok && ep.Secret != "" {
			v.Health = "secret missing"
		}
		out = append(out, v)
	}
	return out
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
	t.opsMu.Lock()
	for _, ep := range append(append(slices.Clone(t.endpoints), t.connectionEndpoints()...), &actions.Endpoint{ID: acceptedformat.ModelEndpoint, Kind: acceptedformat.ModelEndpoint}) {
		if ep.Kind != acceptedformat.ModelEndpoint && !t.breakers.Allow("endpoint:"+ep.ID, now, true) {
			continue
		}
		if x, ok := t.outbound.Claim(ep.ID, now); ok {
			jobs = append(jobs, job{x.Effect, *ep, x.Span})
		}
	}
	t.opsMu.Unlock()
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

func (t *Tenant) decideEndpoint(c platform.Caller, s *pb.Submission, now time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	id := s.GetTarget().GetId()
	invalid := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	t.opsMu.Lock()
	known := slices.IndexFunc(t.endpoints, func(e *actions.Endpoint) bool { return e.ID == id }) >= 0
	t.opsMu.Unlock()
	if s.GetSchema().GetName() == controlowner.SchemaEndpointRemove {
		if !known {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
		unknown := t.outbound.Any(func(x actions.Queued) bool { return x.Endpoint == id && x.State == "unknown" })
		if unknown {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Resolve unknown deliveries before removing their endpoint")
		}
		return func(*pb.ChangeRecord) {
			t.opsMu.Lock()
			t.endpoints = slices.DeleteFunc(t.endpoints, func(e *actions.Endpoint) bool { return e.ID == id })
			t.opsMu.Unlock()
			var dropped []platform.Effect
			t.outbound.UpdateAll(func(x *actions.Queued) {
				if x.Endpoint == id && !platform.EffectSettled(x.State) {
					x.State = "discarded"
					dropped = append(dropped, x.Effect)
				}
			})
			for _, x := range dropped {
				t.told(x, platform.Outcome{Effect: x.ID, Result: "discarded", Detail: "its endpoint was removed by " + c.ID}, now, c.Replaying)
			}
		}, nil
	}
	var ep actions.Endpoint
	if json.Unmarshal(s.GetPayload(), &ep) != nil || id == "" {
		return nil, invalid
	}
	u, err := url.Parse(ep.URL)
	if err != nil || u.Host == "" {
		return nil, invalid
	}
	switch ep.Kind {
	case "", "webhook":
		if ep.Secret == "" || len(ep.Events)+len(ep.Effects) == 0 || len(ep.Notifications) > 0 || u.Scheme != "https" && !(u.Scheme == "http" && ep.AllowPrivate) {
			return nil, invalid
		}
		ep.Kind = "webhook"
	case "a2a":
		if len(ep.Effects) == 0 || len(ep.Events)+len(ep.Notifications) > 0 || u.Scheme != "https" && !(u.Scheme == "http" && ep.AllowPrivate) {
			return nil, invalid
		}
	case "email":
		if u.Scheme != "smtp" || !strings.Contains(ep.From, "@") || len(ep.Notifications) == 0 || len(ep.Events)+len(ep.Effects) > 0 || u.User != nil && ep.Secret == "" {
			return nil, invalid
		}
		for _, app := range ep.Notifications {
			if t.App(app) == nil {
				return nil, invalid
			}
		}
	default:
		return nil, invalid
	}
	for _, event := range ep.Events {
		if _, declared := t.protocolEvent(event); t.owner["action:"+event] == nil && !declared {
			return nil, invalid
		}
	}
	for _, kind := range ep.Effects {
		app, name, _ := strings.Cut(kind, "/")
		if a := t.App(app); a == nil || !slices.ContainsFunc(a.Manifest().Emits, func(e platform.EffectKind) bool { return e.Name == name }) {
			return nil, invalid
		}
	}
	if known {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	ep.ID = id
	return func(*pb.ChangeRecord) { t.opsMu.Lock(); t.endpoints = append(t.endpoints, &ep); t.opsMu.Unlock() }, nil
}

func (t *Tenant) decideEffect(c platform.Caller, s *pb.Submission, now time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	id := s.GetTarget().GetId()
	notFound := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	x, found := t.outbound.Get(id)
	if found && x.App == build.ID && x.Event == build.ID+"/dispatch" {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Manage this command through its dispatch review and declared reviewer role")
	}
	if s.GetSchema().GetName() == controlowner.SchemaEffectApprove {
		if !found || x.State != "held" {
			return nil, notFound
		}
		if c.Agent {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
		}
		return func(r *pb.ChangeRecord) {
			t.outbound.Update(id, func(q *actions.Queued) { q.State, q.Due = "pending", now })
			if x.Run != "" && t.agents != nil {
				t.agents.RecordAgentSignal(c, r, x.Run, platform.Signal{At: now, Kind: "approved", By: c.ID, Detail: x.Event}, now)
			}
		}, nil
	}
	if s.GetSchema().GetName() == controlowner.SchemaEffectDiscard {
		if found && x.State == "unknown" {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "An unknown delivery needs reconciliation or a recorded human decision")
		}
		if !found || platform.EffectSettled(x.State) {
			return nil, notFound
		}
		return func(r *pb.ChangeRecord) {
			held := false
			t.outbound.Update(id, func(q *actions.Queued) {
				held = q.State == "held"
				q.State = "discarded"
				x = *q
			})
			if held && x.Run != "" && t.agents != nil {
				t.agents.RecordAgentSignal(c, r, x.Run, platform.Signal{At: now, Kind: "discarded", By: c.ID, Detail: x.Event}, now)
			}
			if t.agents != nil {
				t.agents.AgentEffectEnded(c, r, x.Effect, platform.Outcome{Result: "discarded", Detail: "discarded by " + c.ID}, now)
			}
			t.told(x.Effect, platform.Outcome{Effect: x.ID, Result: "discarded", Detail: "discarded by " + c.ID}, now, c.Replaying)
		}, nil
	}
	if s.GetSchema().GetName() == controlowner.SchemaEffectResolve {
		var input struct {
			Outcome, Reason string
			Answer          json.RawMessage
		}
		if !found || x.State != "unknown" || x.Sending || c.Agent || json.Unmarshal(s.GetPayload(), &input) != nil || !slices.Contains([]string{"delivered", "rejected"}, input.Outcome) || strings.TrimSpace(input.Reason) == "" || len(input.Reason) > 4096 || len(input.Answer) > 64<<10 {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "An unknown delivery needs a human result and bounded evidence")
		}
		return func(*pb.ChangeRecord) {
			t.outbound.Update(id, func(q *actions.Queued) {
				q.State, q.Error, q.Last = input.Outcome, "Resolved by "+c.ID+": "+input.Reason, now
				x = *q
			})
			t.told(x.Effect, platform.Outcome{Effect: x.ID, Result: input.Outcome, Detail: x.Error, Answer: input.Answer}, now, c.Replaying)
		}, nil
	}
	if !found || (x.State != "failed" && x.State != "rejected") {
		return nil, notFound
	}
	return func(*pb.ChangeRecord) {
		t.outbound.Update(id, func(q *actions.Queued) {
			q.State, q.Due, q.Since = "pending", now, q.Attempts
			q.CallbackError = ""
		})
	}, nil
}

func guarded(req *http.Request, allowPrivate bool) (*http.Response, error) {
	dialer := actions.GuardedDialer(allowPrivate)
	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{DialContext: dialer.DialContext, Proxy: nil},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(req)
	return controlowner.RedactCredentialFailure(response, req), err
}
