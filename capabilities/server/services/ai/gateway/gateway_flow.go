package aigateway

import (
	"cmp"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/services/actions"
	"platformserver/services/ai/gateway/providers"
)

func ReservationFailure(refusal *kernel.Error) *platform.AIError {
	failure := &platform.AIError{Status: 503, Detail: cmp.Or(refusal.Message, refusal.Error())}
	if refusal.Code == pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
		failure.Status, failure.Quota = 429, true
	}
	return failure
}

type Reservations struct {
	Stopped func() bool
	Prior   func(call string) (platform.AIReservation, bool)
	Limits  func() platform.AIDefaults
	Accept  func(body []byte) *kernel.Error
}

func (r Reservations) Prepare(m platform.Member, model platform.AIModel, req platform.ChatRequest, input, output int, now time.Time) *platform.AIError {
	if r.Stopped() {
		return &platform.AIError{Status: 503, Detail: "Model dispatch is stopped until the tenant recovers"}
	}
	if prior, ok := r.Prior(req.Call); ok {
		if prior.Member != m.ID || prior.Model != model.Name() || prior.Input != input || prior.Output != output || prior.RequestHash != req.RequestHash {
			return &platform.AIError{Status: 409, Detail: "The model call identity already has different input"}
		}
		return &platform.AIError{Status: 409, Detail: "The model call already has an unsettled or saved result; reconcile it before retrying"}
	}
	reservation := platform.AIReservation{RequestHash: req.RequestHash, Call: req.Call, At: now, Member: m.ID, Agent: m.Agent, Model: model.Name(), Run: req.Run, Input: input, Output: output, State: "pending"}
	body, _ := json.Marshal(platform.AIReserveInput{Reservation: reservation, Defaults: r.Limits(), RunTokens: req.RunTokens})
	if refusal := r.Accept(body); refusal != nil {
		return ReservationFailure(refusal)
	}
	return nil
}

type ChatSetup struct {
	Design    func() *kernel.Error
	Suspended func(member string) bool
	Callable  func(m platform.Member, name string) (platform.AIModel, platform.AIProvider, *kernel.Error)
	Authorize func(model string) *kernel.Error
	Resolver  Resolver
}

func (s ChatSetup) Prepare(m platform.Member, req platform.ChatRequest, stage string) (platform.AIModel, platform.AIProvider, platform.ChatRequest, *kernel.Error) {
	var model platform.AIModel
	var pv platform.AIProvider
	if stage == platform.AIStageDesign {
		if refusal := s.Design(); refusal != nil {
			return model, pv, req, refusal
		}
	}
	if m.Agent && s.Suspended(m.ID) {
		return model, pv, req, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The agent {agent} is suspended", m.ID)
	}
	model, pv, err := s.Callable(m, req.Model)
	if err != nil {
		return model, pv, req, err
	}
	if refusal := s.Authorize(req.Model); refusal != nil {
		return model, pv, req, refusal
	}
	options := req.Options
	if req.MaxTokens > 0 {
		options.MaxTokens = req.MaxTokens
	}
	if ceiling := s.Resolver.MostTokens(); ceiling > 0 && options.MaxTokens > ceiling && req.Options.MaxTokens == 0 {
		options.MaxTokens = ceiling
	}
	binding, problem := model.Resolve("generation", options, s.Resolver.MostTokens())
	if problem != nil {
		return model, pv, req, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, problem.Error())
	}
	binding.Provider = &pv
	binding, err = s.Resolver.Stage(binding, pv, stage)
	if err != nil {
		return model, pv, req, err
	}
	req.Binding = &binding
	return model, pv, req, nil
}

type ModelPlanning struct {
	Resolver   Resolver
	Provider   func(model string) platform.AIProvider
	Default    func() string
	Simulation bool
	Queued     func(ours func(platform.Effect) bool) int
}

func (p ModelPlanning) Effect(tenant string, c platform.Caller, rec *pb.ChangeRecord, q platform.Request, pending []platform.Effect, endpoint string) platform.Effect {
	prompt, _ := q.Payload.(platform.Prompt)
	options := platform.ModelOptions{MaxTokens: prompt.MaxTokens}
	if q.EvaluationFunction != nil {
		options = q.EvaluationFunction.ModelOptions.Override(options)
	}
	s := rec.GetSubmission()
	model := cmp.Or(q.Model, p.Default())
	var binding *platform.AIModelBinding
	failure := ""
	if c.Staging() && !c.Replaying {
		if resolved, _, err := p.Resolver.Resolve("generation", model, options); err != nil {
			failure = err.Message
			if failure == "" {
				failure = "The selected model is disabled or missing"
			}
		} else {
			stage := platform.AIStageRuntime
			if q.Evaluation || p.Simulation {
				stage = platform.AIStageDesign
			}
			if pinned, refusal := p.Resolver.Stage(resolved, p.Provider(resolved.Model), stage); refusal != nil {
				failure = refusal.Message
			} else {
				binding = &pinned
			}
		}
	}
	ask := ModelAsk{Model: model, Binding: binding, ConfigurationError: failure, Prompt: prompt, Reply: q.Reply, Record: s.GetTarget().GetType() + "/" + s.GetTarget().GetId(), Call: q.Target, Evaluation: q.Evaluation, EvaluationConfig: q.EvaluationConfig, EvaluationFunction: q.EvaluationFunction}
	ours := func(x platform.Effect) bool {
		return x.Endpoint == endpoint && x.App == c.App && strings.HasPrefix(x.Key, rec.GetChangeId()+"#")
	}
	n := p.Queued(ours)
	for _, x := range pending {
		if ours(x) {
			n++
		}
	}
	return ask.Effect(tenant, c.App, rec, q.Target, n, endpoint)
}

type ModelSend struct {
	Allowed   func() bool
	Available bool
	Model     func(name string) (platform.AIModel, platform.AIProvider, *kernel.Error)
	Authorize func(model string) *kernel.Error
	Resolver  Resolver
}

func (ask ModelAsk) Ready(out platform.Outcome, s ModelSend) (platform.Outcome, platform.AIModel, platform.AIProvider, bool) {
	var enabled platform.AIModel
	var pv platform.AIProvider
	reject := func(detail string) (platform.Outcome, platform.AIModel, platform.AIProvider, bool) {
		out.Result, out.Detail = "rejected", detail
		return out, enabled, pv, false
	}
	if ask.Function != nil && !s.Allowed() {
		return reject("The AI function input is no longer authorised")
	}
	if ask.ConfigurationError != "" {
		return reject(ask.ConfigurationError)
	}
	if !s.Available {
		return reject("The model capability is unavailable")
	}
	model := ask.Model
	if model == "" {
		return reject("no model is set for apps")
	}
	var err *kernel.Error
	enabled, pv, err = s.Model(model)
	if err != nil {
		return reject("the model " + model + " is not enabled")
	}
	if refusal := s.Authorize(model); refusal != nil {
		return reject(refusal.Message)
	}
	if ask.Evaluation {
		if detail := ask.EvaluationDrift(enabled, pv, func(options platform.ModelOptions) (platform.AIModelBinding, *kernel.Error) {
			current, _, refusal := s.Resolver.Resolve("generation", model, options)
			if refusal != nil {
				return current, refusal
			}
			return s.Resolver.Stage(current, pv, platform.AIStageDesign)
		}); detail != "" {
			return reject(detail)
		}
	}
	return out, enabled, pv, true
}

type doer func(*http.Request) (*http.Response, error)

func (d doer) Do(r *http.Request) (*http.Response, error) { return d(r) }

type ProviderTransport struct {
	Send       func(*http.Request) (*http.Response, error)
	Redact     func(*http.Response, *http.Request) *http.Response
	Credential func(platform.AIProvider) ([]byte, error)
}

func (p ProviderTransport) Client(pv platform.AIProvider, dispatch ...func()) providers.Client {
	send := p.Send
	if send == nil {
		dialer := actions.GuardedDialer(pv.Kind == "local")
		client := &http.Client{Transport: &http.Transport{DialContext: dialer.DialContext, Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		send = client.Do
	}
	return providers.Client{HTTP: doer(func(request *http.Request) (*http.Response, error) {
		if len(dispatch) > 0 {
			dispatch[0]()
		}
		response, err := send(request)
		return p.Redact(response, request), err
	}), Credential: p.Credential}
}
