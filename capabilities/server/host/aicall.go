package host

import (
	"context"
	"encoding/json"
	"platformserver/kernel/protocol"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	runtimekernel "platformserver/kernel"
	"platformserver/platform"
	"platformserver/services/actions"
	aigateway "platformserver/services/ai/gateway"
	"platformserver/services/ai/gateway/providers"
	build "platformserver/services/resources/authoring"
	"platformserver/services/resources/monitoring"
	controlowner "platformserver/services/security/control"
)

func (t *Tenant) limits() protocol.AIDefaults { return t.gatewayRuntime().Limits() }

func (t *Tenant) gatewayRuntime() aigateway.Runtime {
	policy := protocol.AIInferencePolicy{}
	if t.services.AIInference != nil {
		policy = *t.services.AIInference
	}
	return aigateway.Runtime{
		Tenant: t.ID, Models: t.Models, Inference: policy,
		Settings: func(app, name string) string { return t.setting(t.automation(app, false), name) },
		Resolver: t.modelResolver(), Suspended: t.suspended,
		DesignAllowed: func(m platform.Member) *platform.Error {
			c := platform.NewCaller(runtime{t: t}, m, build.ID, false, false)
			return authorizationRefusal(t.authorizeNative(c, "POST /v1/build/ai/chat", "action", []string{build.Builder}, true))
		},
		ModelAllowed:    func(m platform.Member, name string) *platform.Error { return t.modelAllowed(protocol.AIApp, m, name) },
		FunctionAllowed: t.functionAllowed,
		Available: func(provider string, at time.Time, claim bool) bool {
			return t.breakers.Allow("ai:"+provider, at, claim)
		},
		Report: func(provider string, ok bool, at time.Time) { t.breakers.Report("ai:"+provider, ok, at) },
		Provider: func(provider platform.AIProvider, dispatch func()) providers.Client {
			return t.modelProvider(provider, dispatch)
		},
		Transcribe: func(at time.Time, member, model, run string, request, answer []byte, outcome string) {
			t.transcribe(Transcript{At: at, Member: member, Model: model, Run: run, Request: request, Answer: answer, Outcome: outcome})
		},
		Reserve: t.reserveCall, Release: t.releaseCall, Meter: t.meter, Call: t.call,
		Queued: func(ours func(platform.Effect) bool) int {
			return len(t.outbound.Select(func(x actions.Queued) bool { return ours(x.Effect) }))
		},
		ProviderOf: func(model string) platform.AIProvider {
			_, provider, _ := t.Models.Model(model)
			return provider
		},
		Simulation: t.simulation, App: t.App, Authority: t.AuthorityOf,
		Read: func(read func(aigateway.Runtime) *platform.Error) *platform.Error {
			_, refusal := recordRead(t, context.Background(), func(t *Tenant) (struct{}, *platform.Error) {
				return struct{}{}, read(t.gatewayRuntime())
			})
			return refusal
		},
	}
}

func (t *Tenant) modelAllowed(app string, m platform.Member, name string) *platform.Error {
	c := platform.NewCaller(runtime{t: t}, m, app, false, false)
	return authorizationRefusal(t.authorizeNative(c, "model:"+name, "action", []string{platform.AnyMember}, true))
}

func (t *Tenant) reserveCall(m platform.Member, model platform.AIModel, req platform.ChatRequest, input, output int, now time.Time) *platform.AIError {
	failure, refusal := decideOnImage(t, context.Background(), "model:reserve", func(t *Tenant) (*platform.AIError, *platform.Error) {
		return aigateway.Reservations{
			Stopped: func() bool { return t.businessStopped() || t.recordReadRefusal() != nil },
			Prior:   func(call string) (protocol.AIReservation, bool) { return t.Models.Reservation(call) },
			Limits:  t.limits,
			Accept: func(body []byte) *platform.Error {
				_, refusal := t.inputAccepted(t.Models, platform.Member{ID: "app:" + protocol.AIApp, Tenant: t.ID}, protocol.AIReserveCall, body, now)
				return refusal
			},
		}.Prepare(m, model, req, input, output, now), nil
	}, func(t *Tenant, decision preparedAccepted, failure *platform.AIError, refusal *platform.Error) (*platform.AIError, *platform.Error) {
		_, refusal = t.finishPreparedInput(platform.Member{ID: "app:" + protocol.AIApp, Tenant: t.ID}, protocol.AIReserveCall, decision, nil, refusal)
		if refusal != nil {
			return aigateway.ReservationFailure(refusal), nil
		}
		return failure, nil
	})
	if refusal != nil {
		return aigateway.ReservationFailure(refusal)
	}
	return failure
}

func (t *Tenant) releaseCall(call string, now time.Time) *platform.Error {
	member := platform.Member{ID: "app:" + protocol.AIApp, Tenant: t.ID}
	_, refusal := decideOnImage(t, context.Background(), "model:release", func(t *Tenant) (any, *platform.Error) {
		r, ok := t.Models.Reservation(call)
		if !ok {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The model reservation is unavailable")
		}
		body, _ := json.Marshal(protocol.AIReserveInput{Reservation: r, Cancel: true})
		return t.inputAccepted(t.Models, member, protocol.AIReserveCall, body, now)
	}, func(t *Tenant, decision preparedAccepted, answer any, refusal *platform.Error) (any, *platform.Error) {
		return t.finishPreparedInput(member, protocol.AIReserveCall, decision, answer, refusal)
	})
	return refusal
}

func (t *Tenant) Chat(m platform.Member, req platform.ChatRequest, now time.Time, delta ...func(string)) (platform.ChatAnswer, *platform.Error, *platform.AIError) {
	return t.gatewayRuntime().Chat(m, req, protocol.AIStageRuntime, now, delta...)
}

func (t *Tenant) DesignChat(m platform.Member, req platform.ChatRequest, now time.Time, delta ...func(string)) (platform.ChatAnswer, *platform.Error, *platform.AIError) {
	return t.gatewayRuntime().Chat(m, req, protocol.AIStageDesign, now, delta...)
}

func (t *Tenant) call(pv platform.AIProvider, model platform.AIModel, m platform.Member, req platform.ChatRequest, now time.Time) (platform.ChatAnswer, *platform.AIError) {
	ceiling, _ := recordRead(t, context.Background(), func(t *Tenant) (int, *platform.Error) { return t.modelResolver().MostTokens(), nil })
	gateway := t.gatewayRuntime().Calls(func() int { return ceiling })
	span := monitoring.Outside(trace.SpanContext{}, "chat "+model.Model, attribute.String("gen_ai.operation.name", "chat"), attribute.String("gen_ai.provider.name", pv.ID), attribute.String("gen_ai.request.model", model.Model), attribute.String("platform.tenant", t.ID))
	answer, failure := gateway.Call(pv, model, m, req, now)
	span.SetAttributes(attribute.Int("gen_ai.usage.input_tokens", answer.Usage.Input), attribute.Int("gen_ai.usage.output_tokens", answer.Usage.Output))
	if failure != nil {
		monitoring.End(span, failure.Detail)
	} else {
		monitoring.End(span, "ok")
	}
	return answer, failure
}

func (t *Tenant) meter(m platform.Member, u platform.AIUsage) *platform.Error {
	_, refusal := decideOnImage(t, context.Background(), "model:usage", func(t *Tenant) (struct{}, *platform.Error) {
		return struct{}{}, t.acceptUsage(m, u)
	}, func(t *Tenant, decision preparedAccepted, value struct{}, refusal *platform.Error) (struct{}, *platform.Error) {
		if len(decision.entry.Body) == 0 {
			return value, refusal
		}
		return value, t.finishUsage(decision)
	})
	if refusal == nil {
		t.notifySoftQuota(u.At)
	}
	return refusal
}

func (t *Tenant) modelProvider(pv platform.AIProvider, dispatch ...func()) providers.Client {
	return aigateway.ProviderTransport{Send: t.AIClient, Redact: controlowner.RedactCredentialFailure,
		Credential: func(provider platform.AIProvider) ([]byte, error) {
			key, _, err := t.resolveCredential(provider.Secret, "ai/"+provider.ID, provider.BaseURL)
			return key, err
		}}.Client(pv, dispatch...)
}

func (t *Tenant) usage() runtimekernel.UsageApply {
	if t.Models == nil {
		return runtimekernel.UsageApply{}
	}
	return runtimekernel.UsageApply{Usage: t.Models.Snapshot, Meter: t.Models.Meter}
}

func (t *Tenant) usagePredecessor() (string, error) { return t.usage().Predecessor() }

func (t *Tenant) usageAcceptance() aigateway.UsageAcceptance {
	return aigateway.UsageAcceptance{Tenant: t.ID,
		Prior: func() []platform.AIUsage { return t.Models.Usage() },
		Caller: func(m platform.Member) platform.Caller {
			return platform.NewCaller(runtime{t: t, scoped: true}, m, protocol.AIApp, false, true)
		},
		Authorize: func(c platform.Caller, valid bool) (platform.AuthzDecision, *platform.Error) {
			decision := t.authorizeNative(c, "usage:observe", "derive", nil, valid)
			return decision, authorizationRefusal(decision)
		},
		Predecessor: t.usagePredecessor,
		Apply:       func(raw []byte) error { _, err := t.ApplyUsageResult(raw, t.usage()); return err },
		Quarantine:  t.quarantine,
		Applied:     func() { t.acceptedResultApplied(); t.operationsChanged(protocol.AIApp) }}
}

func (t *Tenant) acceptUsage(m platform.Member, u platform.AIUsage) *platform.Error {
	r, raw, refusal := t.usageAcceptance().Seal(m, u)
	if refusal != nil || raw == nil {
		return refusal
	}
	decision := preparedAccepted{entry: runtimekernel.AcceptedEntry(protocol.AIApp, m, raw, r.At, nil), key: r.Key, hash: r.RequestHash}
	if t.stage(decision) {
		return nil
	}
	refusal = t.finishUsage(decision)
	if refusal == nil {
		t.notifySoftQuota(u.At)
	}
	return refusal
}

func (t *Tenant) finishUsage(decision preparedAccepted) *platform.Error {
	committed, err := t.acceptResult(decision.entry, decision.key, decision.hash)
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The model usage was not accepted")
	}
	refusal := t.usageAcceptance().Finish(committed, decision.key, decision.hash)
	if refusal == nil {
		t.invalidateReadImage()
	}
	return refusal
}
