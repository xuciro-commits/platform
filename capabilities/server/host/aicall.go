package host

import (
	"context"
	"platformserver/services/resources/monitoring"
	controlowner "platformserver/services/security/control"

	"encoding/json"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	acceptedformat "platformserver/kernel/acceptedformat"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/services/actions"
	"platformserver/services/ai/gateway"
	"platformserver/services/ai/gateway/providers"
	"platformserver/services/resources/authoring"
)

func (t *Tenant) limits() platform.AIDefaults {
	return aigateway.Limits(func(app, name string) string { return t.setting(t.automation(app, false), name) })
}

func (t *Tenant) reserveCall(m platform.Member, model platform.AIModel, req platform.ChatRequest, input, output int, now time.Time) *platform.AIError {
	failure, refusal := decideOnImage(t, context.Background(), "model:reserve", func(t *Tenant) (*platform.AIError, *kernel.Error) {
		return t.prepareReserveCall(m, model, req, input, output, now), nil
	}, func(t *Tenant, candidate preparedAccepted, failure *platform.AIError, refusal *kernel.Error) (*platform.AIError, *kernel.Error) {
		_, refusal = t.finishPreparedInput(platform.Member{ID: "app:" + platform.AIApp, Tenant: t.ID}, platform.AIReserveCall, candidate, nil, refusal)
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

func (t *Tenant) prepareReserveCall(m platform.Member, model platform.AIModel, req platform.ChatRequest, input, output int, now time.Time) *platform.AIError {
	return aigateway.Reservations{
		Stopped: func() bool { return t.businessStopped() || t.recordReadRefusal() != nil },
		Prior:   func(call string) (platform.AIReservation, bool) { return t.ai.Reservation(call) },
		Limits:  t.limits,
		Accept: func(body []byte) *kernel.Error {
			_, refusal := t.inputAccepted(t.ai, platform.Member{ID: "app:" + platform.AIApp, Tenant: t.ID}, platform.AIReserveCall, body, now)
			return refusal
		},
	}.Prepare(m, model, req, input, output, now)
}

func (t *Tenant) releaseCall(call string, now time.Time) *kernel.Error {
	member := platform.Member{ID: "app:" + platform.AIApp, Tenant: t.ID}
	_, refusal := decideOnImage(t, context.Background(), "model:release", func(t *Tenant) (any, *kernel.Error) {
		r, ok := t.ai.Reservation(call)
		if !ok {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The model reservation is unavailable")
		}
		body, _ := json.Marshal(platform.AIReserveInput{Reservation: r, Cancel: true})
		return t.inputAccepted(t.ai, member, platform.AIReserveCall, body, now)
	}, func(t *Tenant, candidate preparedAccepted, answer any, refusal *kernel.Error) (any, *kernel.Error) {
		return t.finishPreparedInput(member, platform.AIReserveCall, candidate, answer, refusal)
	})
	return refusal
}

func (t *Tenant) Chat(m platform.Member, req platform.ChatRequest, now time.Time, delta ...func(string)) (platform.ChatAnswer, *kernel.Error, *platform.AIError) {
	return t.chatForStage(m, req, now, platform.AIStageRuntime, delta...)
}

func (t *Tenant) DesignChat(m platform.Member, req platform.ChatRequest, now time.Time, delta ...func(string)) (platform.ChatAnswer, *kernel.Error, *platform.AIError) {
	return t.chatForStage(m, req, now, platform.AIStageDesign, delta...)
}

func (t *Tenant) chatForStage(m platform.Member, req platform.ChatRequest, now time.Time, stage string, delta ...func(string)) (platform.ChatAnswer, *kernel.Error, *platform.AIError) {
	return aigateway.ChatRun{Available: t.ai != nil, Call: t.call, Meter: t.meter,
		Setup: func(req platform.ChatRequest) (model platform.AIModel, pv platform.AIProvider, prepared platform.ChatRequest, refusal *kernel.Error) {
			_, refusal = recordRead(t, context.Background(), func(t *Tenant) (struct{}, *kernel.Error) {
				var err *kernel.Error
				model, pv, prepared, err = aigateway.ChatSetup{
					Design: func() *kernel.Error {
						c := platform.NewCaller(runtime{t: t}, m, build.ID, false, false)
						return authorizationRefusal(t.authorizeNative(c, "POST /v1/build/ai/chat", "action", []string{build.Builder}, true))
					},
					Suspended: t.suspended,
					Callable: func(m platform.Member, name string) (platform.AIModel, platform.AIProvider, *kernel.Error) {
						return t.ai.Callable(m, name)
					},
					Authorize: func(name string) *kernel.Error {
						c := platform.NewCaller(runtime{t: t}, m, platform.AIApp, false, false)
						return authorizationRefusal(t.authorizeNative(c, "model:"+name, "action", []string{platform.AnyMember}, true))
					},
					Resolver: t.modelResolver(),
				}.Prepare(m, req, stage)
				return struct{}{}, err
			})
			return
		}}.Run(m, req, now, delta...)
}

func (t *Tenant) gateway(mostTokens func() int) aigateway.Calls {
	policy := platform.AIInferencePolicy{}
	if t.services.AIInference != nil {
		policy = *t.services.AIInference
	}
	return aigateway.Calls{Inference: policy, MostTokens: mostTokens, Reserve: t.reserveCall, Release: t.releaseCall, Meter: t.meter,
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
	}
}

func (t *Tenant) call(pv platform.AIProvider, model platform.AIModel, m platform.Member, req platform.ChatRequest, now time.Time) (platform.ChatAnswer, *platform.AIError) {
	ceiling, _ := recordRead(t, context.Background(), func(t *Tenant) (int, *kernel.Error) { return t.modelResolver().MostTokens(), nil })
	gateway := t.gateway(func() int { return ceiling })
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

func (t *Tenant) meter(m platform.Member, u platform.AIUsage) *kernel.Error {
	_, refusal := decideOnImage(t, context.Background(), "model:usage", func(t *Tenant) (struct{}, *kernel.Error) {
		return struct{}{}, t.acceptUsage(m, u)
	}, func(t *Tenant, candidate preparedAccepted, value struct{}, refusal *kernel.Error) (struct{}, *kernel.Error) {
		if len(candidate.entry.Body) == 0 {
			return value, refusal
		}
		return value, t.finishUsage(candidate)
	})
	return refusal
}

func (t *Tenant) modelProvider(pv platform.AIProvider, dispatch ...func()) providers.Client {
	return aigateway.ProviderTransport{Send: t.AIClient, Redact: controlowner.RedactCredentialFailure,
		Credential: func(provider platform.AIProvider) ([]byte, error) {
			key, _, err := t.resolveCredential(provider.Secret, "ai/"+provider.ID, provider.BaseURL)
			return key, err
		}}.Client(pv, dispatch...)
}

func (t *Tenant) askModel(c platform.Caller, rec *pb.ChangeRecord, q platform.Request) {
	t.outbound.Add(t.current(), t.planModelRequest(c, rec, q, nil))
}

func (t *Tenant) planModelRequest(c platform.Caller, rec *pb.ChangeRecord, q platform.Request, pending []platform.Effect) platform.Effect {
	return aigateway.ModelPlanning{
		Resolver: t.modelResolver(),
		Provider: func(model string) platform.AIProvider {
			_, provider, _ := t.ai.Model(model)
			return provider
		},
		Default:    func() string { return t.setting(t.automation(platform.AIApp, false), platform.AISettingAppModel) },
		Simulation: t.simulation,
		Queued: func(ours func(platform.Effect) bool) int {
			return len(t.outbound.Select(func(x actions.Queued) bool { return ours(x.Effect) }))
		},
	}.Effect(t.ID, c, rec, q, pending, acceptedformat.ModelEndpoint)
}

func (t *Tenant) sendModel(x platform.Effect, now time.Time) (platform.Outcome, *platform.AIUsage) {
	return aigateway.ModelDispatch{Call: t.call,
		Gate: func(ask aigateway.ModelAsk, m platform.Member, out platform.Outcome) (gated platform.Outcome, enabled platform.AIModel, pv platform.AIProvider, ready bool, refusal *kernel.Error) {
			_, refusal = recordRead(t, context.Background(), func(t *Tenant) (struct{}, *kernel.Error) {
				gated, enabled, pv, ready = ask.Ready(out, aigateway.ModelSend{
					Allowed:   func() bool { return t.functionAllowed(x.App, ask, now) },
					Available: t.ai != nil,
					Model:     func(name string) (platform.AIModel, platform.AIProvider, *kernel.Error) { return t.ai.Model(name) },
					Authorize: func(name string) *kernel.Error {
						c := platform.NewCaller(runtime{t: t}, m, x.App, false, true)
						return authorizationRefusal(t.authorizeNative(c, "model:"+name, "action", nil, true))
					},
					Resolver: t.modelResolver(),
				})
				return struct{}{}, nil
			})
			return
		}}.Send(t.ID, x, now)
}

func (t *Tenant) modelAnswerSubmission(x platform.Effect, o platform.Outcome, usage *platform.AIUsage) (platform.App, *pb.Submission) {
	return aigateway.AnswerSubmission(t.ID, x, o, usage, t.App, t.AuthorityOf)
}
