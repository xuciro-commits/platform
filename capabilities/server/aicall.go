package platformserver

import (
	"cmp"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"net/http"
	resultformat "platformserver/internal/acceptance/result"
	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/aigateway"
	"platformserver/aigateway/providers"
	"platformserver/internal/construction"
	"platformserver/platform"
)

// Model calls: the host checks the caller's access, calls
// the provider outside the tenant's lock, and journals the call's usage as an
// entry of kind usage. Replay applies usage and never calls a model; prompts
// and answers are the caller's and are not kept.

const aiTimeout = 120 * time.Second

// Message is one turn of a conversation, as the OpenAI wire has it: a
// system, user or assistant turn, an assistant's tool calls, or a tool's result.
type Message = platform.Message

// Tool is a function the model may call: a name, what it does, and
// its parameters as JSON Schema properties.
type Tool = platform.Tool

// ToolCall is a model's call of a tool, with its arguments as JSON.
type ToolCall = platform.ToolCall

// ChatRequest is a member's call: a model by name ("<provider>/<model>") and the conversation.
type ChatRequest = platform.ChatRequest

// ChatAnswer is what the model answered, with the call's usage.
type ChatAnswer = platform.ChatAnswer

// AIError is a call the provider did not answer or refused, or one a limit
// kept from the provider (Quota).
type AIError = platform.AIError

// unavailable is a failure that says the provider is down or overloaded, not
// that the request was wrong: no answer, a rate limit or a server error.
func unavailable(e *AIError) bool {
	return e.Status == 0 || e.Status == http.StatusTooManyRequests || e.Status >= 500
}

// models is the AI app as the host calls models for members and agents: the
// enabled models and their providers, and the usage of every call.
type models interface {
	platform.App
	Snapshot() (json.RawMessage, error)
	Callable(m platform.Member, name string) (platform.AIModel, platform.AIProvider, *kernel.Error)
	Model(name string) (platform.AIModel, platform.AIProvider, *kernel.Error)
	Provider(id string) (platform.AIProvider, bool)
	Meter(u platform.AIUsage)
	Spent(member string, now time.Time) int
	Reservation(string) (platform.AIReservation, bool)
	Usage() []platform.AIUsage
	Quota(platform.Member, platform.AIDefaults, time.Time) platform.AIQuota
}

// limits are the defaults a member without its own takes.
func (t *Tenant) limits() platform.AIDefaults {
	number := func(app, name string) int {
		n, _ := strconv.Atoi(t.setting(t.automation(app, false), name))
		return n
	}
	return platform.AIDefaults{People: number(platform.AIApp, platform.AISettingDailyTokens), Agents: number(AgentApp, SettingAgentDaily),
		PerMinute: number(platform.AIApp, platform.AISettingPerMinute)}
}

// reserveCall saves the original meter owner's liability before external I/O.
// Call identities belong to a run turn/effect attempt, never to HTTP payloads.
func (t *Tenant) reserveCall(m platform.Member, model platform.AIModel, req ChatRequest, input, output int, now time.Time) *AIError {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.businessStopped() || t.recordReadRefusal() != nil {
		return &AIError{Status: 503, Detail: "Model dispatch is stopped until the tenant recovers"}
	}
	if prior, ok := t.ai.Reservation(req.Call); ok {
		if prior.Member != m.ID || prior.Model != model.Name() || prior.Input != input || prior.Output != output || prior.RequestHash != req.RequestHash {
			return &AIError{Status: 409, Detail: "The model call identity already has different input"}
		}
		return &AIError{Status: 409, Detail: "The model call already has an unsettled or saved result; reconcile it before retrying"}
	}
	r := platform.AIReservation{RequestHash: req.RequestHash, Call: req.Call, At: now, Member: m.ID, Agent: m.Agent, Model: model.Name(), Run: req.Run, Input: input, Output: output, State: "pending"}
	body, _ := json.Marshal(platform.AIReserveInput{Reservation: r, Defaults: t.limits(), RunTokens: req.RunTokens})
	owner, ok := t.ai.(platform.AcceptedInputApp)
	if !ok {
		return &AIError{Status: 503, Detail: "The AI meter cannot accept a reservation"}
	}
	_, refusal := t.inputAccepted(owner, platform.Member{ID: "app:" + platform.AIApp, Tenant: t.ID}, platform.AIReserveCall, body, now)
	if refusal != nil {
		failure := &AIError{Status: 503, Detail: cmp.Or(refusal.Message, refusal.Error())}
		if refusal.Code == pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
			failure.Status, failure.Quota = 429, true
		}
		return failure
	}
	return nil
}

func (t *Tenant) releaseCall(call string, now time.Time) *kernel.Error {
	t.mu.Lock()
	defer t.mu.Unlock()
	r, ok := t.ai.Reservation(call)
	if !ok {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The model reservation is unavailable")
	}
	body, _ := json.Marshal(platform.AIReserveInput{Reservation: r, Cancel: true})
	_, refusal := t.inputAccepted(t.ai.(platform.AcceptedInputApp), platform.Member{ID: "app:" + platform.AIApp, Tenant: t.ID}, platform.AIReserveCall, body, now)
	return refusal
}

// mostTokens is the platform generation ceiling. Zero leaves the reviewed
// model limit in force; an unbounded call cannot acquire a reservation.
func (t *Tenant) mostTokens() int {
	n, _ := strconv.Atoi(t.setting(t.automation(platform.AIApp, false), platform.AISettingMaxTokens))
	return n
}

// Chat calls a model for m. A refusal of access is a kernel error; a provider
// failure is an AIError, and its usage is journaled as failed. delta, when
// given, receives the answer as it comes.
func (t *Tenant) Chat(m platform.Member, req ChatRequest, now time.Time, delta ...func(string)) (ChatAnswer, *kernel.Error, *AIError) {
	return t.chatForStage(m, req, now, platform.AIStageRuntime, delta...)
}

// DesignChat is an explicit Studio operation. The accepting owner selects the
// stage after checking the current builder role; request JSON cannot select it.
func (t *Tenant) DesignChat(m platform.Member, req ChatRequest, now time.Time, delta ...func(string)) (ChatAnswer, *kernel.Error, *AIError) {
	return t.chatForStage(m, req, now, platform.AIStageDesign, delta...)
}

func (t *Tenant) chatForStage(m platform.Member, req ChatRequest, now time.Time, stage string, delta ...func(string)) (ChatAnswer, *kernel.Error, *AIError) {
	if t.ai == nil {
		return ChatAnswer{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}, nil
	}
	if len(req.Messages) == 0 {
		return ChatAnswer{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}, nil
	}
	var model platform.AIModel
	var pv platform.AIProvider
	_, refusal := recordRead(t, context.Background(), func() (struct{}, *kernel.Error) {
		if stage == platform.AIStageDesign {
			c := platform.NewCaller(runtime{t}, m, construction.ID, false, false)
			if refusal := authorizationRefusal(t.authorizeNative(c, "POST /v1/build/ai/chat", "action", []string{construction.Builder}, true)); refusal != nil {
				return struct{}{}, refusal
			}
		}
		if m.Agent && t.suspended(m.ID) {
			return struct{}{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The agent {agent} is suspended", m.ID)
		}
		var err *kernel.Error
		model, pv, err = t.ai.Callable(m, req.Model)
		if err != nil && err.Code == pb.ErrorCode_ERROR_CODE_NOT_FOUND {
			return struct{}{}, err
		}
		c := platform.NewCaller(runtime{t}, m, platform.AIApp, false, false)
		decision := t.authorizeNative(c, "model:"+req.Model, "action", []string{platform.AnyMember}, err == nil)
		if refusal := authorizationRefusal(decision); refusal != nil {
			return struct{}{}, refusal
		}
		if err != nil {
			return struct{}{}, err
		}
		options := req.Options
		if req.MaxTokens > 0 {
			options.MaxTokens = req.MaxTokens
		}
		if ceiling := t.mostTokens(); ceiling > 0 && options.MaxTokens > ceiling && req.Options.MaxTokens == 0 {
			options.MaxTokens = ceiling
		}
		binding, problem := model.Resolve("generation", options, t.mostTokens())
		if problem != nil {
			return struct{}{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, problem.Error())
		}
		binding.Provider = &pv
		binding, err = t.modelStage(binding, pv, stage)
		if err != nil {
			return struct{}{}, err
		}
		req.Binding = &binding
		return struct{}{}, nil
	})
	if refusal != nil {
		return ChatAnswer{}, refusal, nil
	}
	if len(delta) > 0 {
		req.Delta = delta[0]
	}
	answer, failure := t.call(pv, model, m, req, now)
	if answer.Usage.Model != "" {
		if err := t.meter(m, answer.Usage); err != nil {
			return ChatAnswer{}, err, nil
		}
	}
	return answer, nil, failure
}

// call calls a model once, outside the tenant's lock; the caller meters it.
func (t *Tenant) call(pv platform.AIProvider, model platform.AIModel, m platform.Member, req ChatRequest, now time.Time) (ChatAnswer, *AIError) {
	var binding platform.AIModelBinding
	var configErr error
	purpose := "generation"
	if m.Agent {
		purpose = "agent"
	}
	if req.Binding != nil && req.Binding.Purpose != "" {
		purpose = req.Binding.Purpose
	}
	if req.Binding != nil {
		binding = *req.Binding
		frozen := platform.AIModel{Provider: model.Provider, Model: model.Model, Capabilities: binding.Capabilities, Defaults: binding.Options, Revision: binding.Revision}
		_, configErr = frozen.Resolve(purpose, platform.ModelOptions{}, t.mostTokens())
		if configErr == nil {
			model.Defaults = platform.ModelOptions{}
			_, configErr = model.Resolve(purpose, binding.Options, t.mostTokens())
		}
	} else {
		options := req.Options
		if req.MaxTokens > 0 {
			options.MaxTokens = req.MaxTokens
		}
		if ceiling := t.mostTokens(); ceiling > 0 && options.MaxTokens > ceiling && req.Options.MaxTokens == 0 {
			options.MaxTokens = ceiling
		}
		binding, configErr = model.Resolve(purpose, options, t.mostTokens())
	}
	if configErr == nil {
		policy := platform.AIInferencePolicy{}
		if t.services.AIInference != nil {
			policy = *t.services.AIInference
		}
		configErr = aigateway.Check(policy, pv, binding)
	}
	if binding.Provider != nil && (binding.Provider.Wire != pv.Wire || binding.Provider.Vendor != pv.Vendor || binding.Provider.BaseURL != pv.BaseURL) {
		configErr = fmt.Errorf("The provider protocol or address changed; start a new request")
	}
	req.MaxTokens, req.Options = binding.Options.MaxTokens, binding.Options
	req.Binding = &binding
	// The token counter is conservative and includes instructions, tools and
	// protected continuation. Never silently truncate a tool/result pair.
	input, _ := json.Marshal(struct {
		Messages []map[string]any
		Tools    []Tool
	}{providers.OpenAIMessages(req.Messages), req.Tools})
	estimated := estimateTokens(string(input))
	if configErr == nil && binding.Capabilities != nil && binding.Capabilities.InputTokens != nil && estimated > *binding.Capabilities.InputTokens {
		configErr = fmt.Errorf("Input exceeds the model input limit; shorten the prompt or history")
	}
	if configErr == nil && req.Options.InputTokens > 0 && estimated > req.Options.InputTokens {
		configErr = fmt.Errorf("Input exceeds the configured token budget; shorten the prompt or history")
	}
	if configErr == nil && binding.Capabilities != nil && binding.Capabilities.ContextTokens != nil && estimated+req.MaxTokens > *binding.Capabilities.ContextTokens {
		configErr = fmt.Errorf("Input and generation exceed the model context window; shorten the input or generation budget")
	}
	if configErr == nil && req.MaxTokens <= 0 {
		configErr = fmt.Errorf("Set a positive generation token limit before calling this model")
	}
	if configErr != nil {
		return ChatAnswer{}, &AIError{Status: 400, Detail: configErr.Error()}
	}
	if !t.breakers.allow("ai:"+pv.ID, now, false) {
		return ChatAnswer{}, &AIError{Status: 503, Detail: "the provider " + pv.ID + " failed repeatedly"}
	}
	if req.Call == "" {
		req.Call = "chat:" + rand.Text()
	}
	inputBound := len(input) + 32*(len(req.Messages)+len(req.Tools))
	protected := []json.RawMessage{}
	for _, message := range req.Messages {
		inputBound += len(message.Protocol)
		protected = append(protected, message.Protocol)
	}
	identity, _ := json.Marshal([]any{json.RawMessage(input), binding, protected})
	req.RequestHash = fmt.Sprintf("%x", sha256.Sum256(identity))
	if failure := t.reserveCall(m, model, req, inputBound, req.MaxTokens, now); failure != nil {
		return ChatAnswer{}, failure
	}
	// Readiness does not consume the half-open probe. Claim it only after
	// the reservation has been accepted; a competing probe releases this call.
	if !t.breakers.allow("ai:"+pv.ID, now, true) {
		if refusal := t.releaseCall(req.Call, now); refusal != nil {
			return ChatAnswer{}, &AIError{Status: 503, Detail: refusal.Error()}
		}
		return ChatAnswer{}, &AIError{Status: 503, Detail: "the provider " + pv.ID + " failed repeatedly"}
	}
	started := time.Now()
	dispatched := false
	provider := t.modelProvider(pv, func() { dispatched = true })
	complete := provider.Complete
	if pv.Wire == "anthropic" {
		complete = provider.CompleteAnthropic
	}
	span := outside(trace.SpanContext{}, "chat "+model.Model, attribute.String("gen_ai.operation.name", "chat"),
		attribute.String("gen_ai.provider.name", pv.ID), attribute.String("gen_ai.request.model", model.Model), attribute.String("platform.tenant", t.ID))
	answer, u, failure := complete(pv, model.Model, req)
	t.breakers.report("ai:"+pv.ID, failure == nil || !unavailable(failure), now)
	if !dispatched {
		end(span, "not dispatched")
		if refusal := t.releaseCall(req.Call, now); refusal != nil {
			return ChatAnswer{}, &AIError{Status: 503, Detail: refusal.Error()}
		}
		return answer, failure
	}
	if !u.TokensReported {
		// A provider that says nothing about usage must not be booked as
		// spending nothing: the platform books a conservative estimate of the
		// request and the answer, marked as one, so daily caps and run budgets
		// still bite.
		request, _ := json.Marshal(req)
		said := answer.Content
		for _, c := range answer.ToolCalls {
			said += string(c.Arguments)
		}
		u.Input, u.Output, u.TokensEstimated = estimateTokens(string(request)), estimateTokens(said), true
		knownCache := 0
		for _, count := range []*int{u.CacheReadInput, u.CacheWriteInput} {
			if count != nil {
				knownCache += *count
			}
		}
		u.Input = max(u.Input, knownCache)
	}
	if u.TokensReported && (u.Input > inputBound || u.Output > req.MaxTokens) {
		failure = &AIError{Status: http.StatusUnprocessableEntity, Detail: "The provider reported usage above the reserved token bound"}
		answer.ToolCalls, answer.Protocol = nil, nil
	}
	span.SetAttributes(attribute.Int("gen_ai.usage.input_tokens", u.Input), attribute.Int("gen_ai.usage.output_tokens", u.Output))
	if failure != nil {
		end(span, failure.Detail)
	} else {
		end(span, "ok")
	}
	u.Call, u.Run = req.Call, req.Run
	u.At, u.Member, u.Agent, u.Model, u.Millis, u.Outcome = now, m.ID, m.Agent, model.Name(), time.Since(started).Milliseconds(), "ok"
	if failure != nil {
		u.Outcome = failure.Detail
		if len(u.Outcome) > 300 {
			u.Outcome = u.Outcome[:300]
		}
	}
	answer.Usage = u
	request, _ := json.Marshal(req)
	reply, _ := json.Marshal(answer)
	t.transcribe(Transcript{At: now, Member: m.ID, Model: model.Name(), Run: req.Run, Request: request, Answer: reply, Outcome: u.Outcome})
	return answer, failure
}

// estimateTokens is a conservative estimate of the tokens in text: about a
// third of its bytes, and 0 only for text that is not there. It is used when,
// and only when, a provider reports no usage.
func estimateTokens(text string) int {
	if text == "" {
		return 0
	}
	return (len(text) + 2) / 3
}

// meter journals a call's usage, then applies it.
func (t *Tenant) meter(m platform.Member, u platform.AIUsage) *kernel.Error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.quarantined() {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "This tenant is isolated until its accepted results recover")
	}
	return t.acceptUsage(m, u)
}

// openAIMessages puts a conversation on the OpenAI wire: tool calls as
// functions with their arguments as a string, tool results with their call.
// Provider transport receives only the original controlled credential and
// outbound capabilities; adapters never receive a Tenant or business writer.
func (t *Tenant) modelProvider(pv platform.AIProvider, dispatch ...func()) providers.Client {
	send := t.AIClient
	if send == nil {
		dialer := guardedDialer(pv.Kind == "local")
		client := &http.Client{Transport: &http.Transport{DialContext: dialer.DialContext, Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		send = client.Do
	}
	return providers.Client{HTTP: doer(func(request *http.Request) (*http.Response, error) {
		if len(dispatch) > 0 {
			dispatch[0]()
		}
		response, err := send(request)
		return redactCredentialFailure(response, request), err
	}), Credential: func(provider platform.AIProvider) ([]byte, error) {
		key, _, err := t.resolveCredential(provider.Secret, "ai/"+provider.ID, provider.BaseURL)
		return key, err
	}}
}

type doer func(*http.Request) (*http.Response, error)

func (d doer) Do(r *http.Request) (*http.Response, error) { return d(r) }

// Apps' model requests. An accepted decision's Request of a
// model becomes owned work of the built-in model destination: kept in the
// outbound queue with the effects, sent on the I/O lane through the door every
// call passes (as the app, within its limits), retried when the provider is
// unavailable, and answered to the app's Reply action as a journaled decision.
// A replay rebuilds the request from the decision and takes the answer from
// the journal: it asks no model.

// modelAsk is a model request as the outbound queue keeps it.
type modelAsk struct {
	Model              string                   `json:"model"`
	Binding            *platform.AIModelBinding `json:"binding,omitempty"`
	ConfigurationError string                   `json:"configurationError,omitempty"`
	Prompt             platform.Prompt          `json:"prompt"`
	Reply              string                   `json:"reply"`
	Record             string                   `json:"record"` // the decision's target, "<type>/<id>", which the reply is on
	Call               string                   `json:"call"`
	Function           *functionBinding         `json:"function,omitempty"`
	Evaluation         bool                     `json:"evaluation,omitempty"`
	EvaluationConfig   string                   `json:"evaluationConfig,omitempty"`
	EvaluationFunction *platform.AIFunction     `json:"evaluationFunction,omitempty"`
}

func (t *Tenant) askModel(c platform.Caller, rec *pb.ChangeRecord, q platform.Request) {
	planned := t.planModelRequest(c, rec, q, nil)
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	t.outbound = append(t.outbound, &effect{span: t.current(), Effect: planned})
	t.trimEffects()
}

// The live and staged paths share the exact model intent planner. Planning
// allocates no work and never calls a provider.
func (t *Tenant) planModelRequest(c platform.Caller, rec *pb.ChangeRecord, q platform.Request, pending []platform.Effect) platform.Effect {
	prompt, _ := q.Payload.(platform.Prompt)
	options := platform.ModelOptions{MaxTokens: prompt.MaxTokens}
	if q.EvaluationFunction != nil {
		options = q.EvaluationFunction.ModelOptions.Override(options)
	}
	s := rec.GetSubmission()
	// Resolve the administrator's default in the accepted decision. A queued
	// request must not switch models when settings change before dispatch or
	// retry. An empty binding stays empty and is refused without provider I/O.
	model := cmp.Or(q.Model, t.setting(t.automation(platform.AIApp, false), platform.AISettingAppModel))
	var binding *platform.AIModelBinding
	failure := ""
	if c.Staging() && !c.Replaying {
		if resolved, _, err := t.resolveModel("generation", model, options); err != nil {
			failure = err.Message
			if failure == "" {
				failure = "The selected model is disabled or missing"
			}
		} else {
			stage := platform.AIStageRuntime
			if q.Evaluation || t.simulation {
				stage = platform.AIStageDesign
			}
			_, provider, _ := t.ai.Model(resolved.Model)
			if pinned, refusal := t.modelStage(resolved, provider, stage); refusal != nil {
				failure = refusal.Message
			} else {
				binding = &pinned
			}
		}
	}
	body, _ := json.Marshal(modelAsk{Model: model, Binding: binding, ConfigurationError: failure, Prompt: prompt, Reply: q.Reply, Record: s.GetTarget().GetType() + "/" + s.GetTarget().GetId(), Call: q.Target, Evaluation: q.Evaluation, EvaluationConfig: q.EvaluationConfig, EvaluationFunction: q.EvaluationFunction})
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	n := 0
	for _, x := range t.outbound {
		if x.Endpoint == resultformat.ModelEndpoint && x.App == c.App && strings.HasPrefix(x.Key, rec.GetChangeId()+"#") {
			n++
		}
	}
	for _, x := range pending {
		if x.Endpoint == resultformat.ModelEndpoint && x.App == c.App && strings.HasPrefix(x.Key, rec.GetChangeId()+"#") {
			n++
		}
	}
	key := fmt.Sprintf("%s#%d", rec.GetChangeId(), n)
	return platform.Effect{ID: fmt.Sprintf("%s:%s:model:%s", t.ID, c.App, key), Endpoint: resultformat.ModelEndpoint,
		Event: c.App + "/model", App: c.App, Key: key, Target: q.Target, At: rec.GetRecordedTime().AsTime(), State: "pending", Due: rec.GetRecordedTime().AsTime(), Body: string(body)}
}

// sendModel makes one attempt of a model request: delivered with the answer,
// retried while the provider or a limit keeps it, rejected when it cannot be asked.
func (t *Tenant) sendModel(x platform.Effect, now time.Time) (platform.Outcome, *platform.AIUsage) {
	out := platform.Outcome{Effect: x.ID}
	var ask modelAsk
	if json.Unmarshal([]byte(x.Body), &ask) != nil {
		out.Result, out.Detail = "rejected", "The saved model request is invalid"
		return out, nil
	}
	if ask.Function != nil {
		t.mu.Lock()
		allowed := t.functionAllowed(x.App, ask, now)
		t.mu.Unlock()
		if !allowed {
			out.Result, out.Detail = "rejected", "The AI function input is no longer authorised"
			return out, nil
		}
	}
	if ask.ConfigurationError != "" {
		out.Result, out.Detail = "rejected", ask.ConfigurationError
		return out, nil
	}
	if t.ai == nil {
		out.Result, out.Detail = "rejected", "The model capability is unavailable"
		return out, nil
	}
	model := ask.Model
	if model == "" {
		out.Result, out.Detail = "rejected", "no model is set for apps"
		return out, nil
	}
	messages := []Message{{Role: "user", Content: ask.Prompt.User}}
	if ask.Prompt.System != "" {
		messages = append([]Message{{Role: "system", Content: ask.Prompt.System}}, messages...)
	}
	// The administrators chose the model for apps, as they choose the agents':
	// no member's access applies, the limits do.
	m := platform.Member{ID: "app:" + x.App, Tenant: t.ID}
	t.mu.Lock()
	enabled, pv, err := t.ai.Model(model)
	t.mu.Unlock()
	if err != nil {
		out.Result, out.Detail = "rejected", "the model "+model+" is not enabled"
		return out, nil
	}
	c := platform.NewCaller(runtime{t}, m, x.App, false, true)
	if refusal := authorizationRefusal(t.authorizeNative(c, "model:"+model, "action", nil, true)); refusal != nil {
		out.Result, out.Detail = "rejected", refusal.Message
		return out, nil
	}
	if ask.Evaluation {
		if ask.EvaluationFunction == nil || ask.EvaluationFunction.Check() != nil || ask.Prompt.System != ask.EvaluationFunction.SystemPrompt() ||
			ask.Prompt.MaxTokens != ask.EvaluationFunction.MaxTokens || len(ask.Prompt.User) > ask.EvaluationFunction.MaxInputBytes {
			out.Result, out.Detail = "rejected", "the evaluation function binding changed"
			return out, nil
		}
		var evaluatedBinding *platform.AIModelBinding
		if ask.Binding != nil {
			current, _, refusal := t.resolveModel("generation", model, ask.EvaluationFunction.ModelOptions.Override(platform.ModelOptions{MaxTokens: ask.EvaluationFunction.MaxTokens}))
			if refusal != nil {
				out.Result, out.Detail = "rejected", refusal.Message
				return out, nil
			}
			current, refusal = t.modelStage(current, pv, platform.AIStageDesign)
			if refusal != nil {
				out.Result, out.Detail = "rejected", refusal.Message
				return out, nil
			}
			evaluatedBinding = &current
		}
		config, digestErr := modelConfigurationDigest(enabled, pv, evaluatedBinding)
		if digestErr != nil || ask.EvaluationConfig == "" || ask.EvaluationConfig != config {
			out.Result, out.Detail = "rejected", "the evaluation model configuration changed"
			return out, nil
		}
	}
	answer, failure := t.call(pv, enabled, m, ChatRequest{Model: model, Messages: messages, MaxTokens: ask.Prompt.MaxTokens, Binding: ask.Binding, Run: x.ID, Call: fmt.Sprintf("effect:%s:%d", x.ID, x.Attempts)}, now)
	switch {
	case failure != nil && unavailable(failure):
		out.Result, out.Detail = "retry", failure.Detail
	case failure != nil:
		out.Result, out.Detail = "rejected", failure.Detail
	default:
		out.Result, out.Answer = "delivered", json.RawMessage(strconv.Quote(answer.Content))
		definition := ask.EvaluationFunction
		if ask.Function != nil {
			definition = &ask.Function.Definition
		}
		if definition != nil && (len(answer.ToolCalls) != 0 || answer.Usage.Output > definition.MaxTokens ||
			definition.ValidateOutput([]byte(answer.Content)) != nil) {
			out.Result, out.Detail, out.Answer = "rejected", "The AI function answer failed its type or budget checks", nil
		}
	}
	if answer.Usage.Model == "" {
		return out, nil
	}
	return out, &answer.Usage
}

func (t *Tenant) modelAnswerSubmission(x platform.Effect, o platform.Outcome, usage *platform.AIUsage) (platform.App, *pb.Submission) {
	var ask modelAsk
	json.Unmarshal([]byte(x.Body), &ask)
	app := t.app(x.App)
	if app == nil || ask.Reply == "" {
		return nil, nil
	}
	answer := platform.Answer{Call: ask.Call, Action: "ask", Outcome: "accepted"}
	if (ask.Function != nil || ask.Evaluation) && usage != nil {
		answer.Metered, answer.TokensReported = true, usage.TokensReported
		answer.InputTokens, answer.OutputTokens = usage.Input, usage.Output
		answer.CostReported, answer.CostUSD = usage.CostReported, usage.Cost
		answer.LatencyMillis, answer.ServedModel = usage.Millis, usage.Served
	}
	if x.State == "delivered" {
		json.Unmarshal(o.Answer, &answer.Text)
	} else {
		answer.Outcome, answer.Code = "refused", o.Detail
	}
	typ, id, _ := strings.Cut(ask.Record, "/")
	payload, _ := json.Marshal(answer)
	reply := &pb.Submission{TenantId: t.ID, PrincipalId: "app:" + x.App, Authority: t.authorityOf(typ), Target: &pb.EntityRef{Type: typ, Id: id},
		Schema: &pb.SchemaRef{Name: ask.Reply, Version: 1}, IdempotencyKey: "answer:" + x.ID, Payload: payload}
	return app, reply
}
