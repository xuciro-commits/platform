package platformserver

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"io"
	"log"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/ai"
	"platformserver/platform"
)

// Model calls (ADR-0015 point 6): the host checks the caller's access, calls
// the provider outside the tenant's lock, and journals the call's usage as an
// entry of kind usage. Replay applies usage and never calls a model; prompts
// and answers are the caller's and are not kept.

const aiTimeout = 120 * time.Second

// Message is one turn of a conversation, as the OpenAI wire has it: a
// system, user or assistant turn, an assistant's tool calls, or a tool's result.
type Message struct {
	Role       string     `json:"role"` // system, user, assistant, tool
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"toolCalls,omitempty"`
	ToolCallID string     `json:"toolCallId,omitempty"` // a tool turn: the call it answers
}

// Tool is a function the model may call (ADR-0021): a name, what it does, and
// its parameters as JSON Schema properties.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Properties  map[string]any `json:"properties"`
	Required    []string       `json:"required,omitempty"`
}

// ToolCall is a model's call of a tool, with its arguments as JSON.
type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ChatRequest is a member's call: a model by name ("<provider>/<model>") and the conversation.
type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Tools       []Tool    `json:"tools,omitempty"`
	MaxTokens   int       `json:"maxTokens,omitempty"`
	Temperature *float64  `json:"temperature,omitempty"`
	// Stream answers token by token as server-sent events (ADR-0029 D2);
	// usage is journaled once, when the call ends.
	Stream bool         `json:"stream,omitempty"`
	run    string       // the agent run or evaluation the call is for, on its transcript
	delta  func(string) // where a streamed call's tokens go
}

// ChatAnswer is what the model answered, with the call's usage.
type ChatAnswer struct {
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"toolCalls,omitempty"`
	Usage     ai.Usage   `json:"usage"`
}

// AIError is a call the provider did not answer or refused, or one a limit
// kept from the provider (Quota, ADR-0029 D1).
type AIError struct {
	Status int    `json:"status,omitempty"` // the provider's HTTP status
	Detail string `json:"detail"`
	Quota  bool   `json:"quota,omitempty"`
}

func (e *AIError) Error() string { return e.Detail }

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
	Callable(m platform.Member, name string) (ai.Model, ai.Provider, *kernel.Error)
	Model(name string) (ai.Model, ai.Provider, *kernel.Error)
	Provider(id string) (ai.Provider, bool)
	Meter(u ai.Usage)
	Spent(member string, now time.Time) int
	Allow(m platform.Member, model ai.Model, d ai.Defaults, now time.Time) string
	Usage() []ai.Usage
}

// allowed says why m may not call model now (ADR-0029 D1), or "": the door
// every model call passes — members', agents', evaluations' and embeddings'.
func (t *Tenant) allowed(m platform.Member, model ai.Model, now time.Time) string {
	number := func(app, name string) int {
		n, _ := strconv.Atoi(t.setting(t.automation(app, false), name))
		return n
	}
	return t.ai.Allow(m, model, ai.Defaults{People: number(ai.ID, ai.SettingDailyTokens), Agents: number(AgentApp, SettingAgentDaily),
		PerMinute: number(ai.ID, ai.SettingPerMinute)}, now)
}

// Chat calls a model for m. A refusal of access is a kernel error; a provider
// failure is an AIError, and its usage is journaled as failed. delta, when
// given, receives the answer as it comes (ADR-0029 D2).
func (t *Tenant) Chat(m platform.Member, req ChatRequest, now time.Time, delta ...func(string)) (ChatAnswer, *kernel.Error, *AIError) {
	if t.ai == nil {
		return ChatAnswer{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}, nil
	}
	if len(req.Messages) == 0 {
		return ChatAnswer{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}, nil
	}
	if m.Agent && t.suspended(m.ID) {
		return ChatAnswer{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The agent {agent} is suspended", m.ID), nil
	}
	model, pv, err := t.ai.Callable(m, req.Model)
	if err != nil {
		return ChatAnswer{}, err, nil
	}
	if !t.breakers.allow("ai:"+pv.ID, now) {
		return ChatAnswer{}, nil, &AIError{Status: http.StatusServiceUnavailable, Detail: "the provider " + pv.ID + " failed repeatedly; its calls wait until it answers again"}
	}
	if why := t.allowed(m, model, now); why != "" {
		return ChatAnswer{}, nil, &AIError{Status: http.StatusTooManyRequests, Detail: why, Quota: true}
	}
	if len(delta) > 0 && len(req.Tools) == 0 {
		req.delta = delta[0]
	}
	answer, failure := t.call(pv, model, m, req, now)
	t.meter(m, answer.Usage)
	return answer, nil, failure
}

// call calls a model once, outside the tenant's lock; the caller meters it.
func (t *Tenant) call(pv ai.Provider, model ai.Model, m platform.Member, req ChatRequest, now time.Time) (ChatAnswer, *AIError) {
	started := time.Now()
	complete := t.complete
	if pv.Wire == "anthropic" {
		complete = t.completeAnthropic
	}
	span := outside(trace.SpanContext{}, "chat "+model.Model, attribute.String("gen_ai.operation.name", "chat"),
		attribute.String("gen_ai.provider.name", pv.ID), attribute.String("gen_ai.request.model", model.Model), attribute.String("platform.tenant", t.ID))
	answer, u, failure := complete(pv, model.Model, req)
	if req.delta != nil && failure == nil && pv.Wire == "anthropic" { // no stream on this wire yet: the answer at once
		req.delta(answer.Content)
	}
	span.SetAttributes(attribute.Int("gen_ai.usage.input_tokens", u.Input), attribute.Int("gen_ai.usage.output_tokens", u.Output))
	if failure != nil {
		end(span, failure.Detail)
	} else {
		end(span, "ok")
	}
	t.breakers.report("ai:"+pv.ID, failure == nil || !unavailable(failure), now)
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
	t.transcribe(Transcript{At: now, Member: m.ID, Model: model.Name(), Run: req.run, Request: request, Answer: reply, Outcome: u.Outcome})
	return answer, failure
}

// meter journals a call's usage, then applies it.
func (t *Tenant) meter(m platform.Member, u ai.Usage) {
	t.mu.Lock()
	defer t.mu.Unlock()
	body, _ := json.Marshal(u)
	t.record(t.ai, "usage", m, body, u.At)
	t.ai.Meter(u)
}

// openAIMessages puts a conversation on the OpenAI wire: tool calls as
// functions with their arguments as a string, tool results with their call.
func openAIMessages(ms []Message) []map[string]any {
	out := make([]map[string]any, 0, len(ms))
	for _, m := range ms {
		msg := map[string]any{"role": m.Role, "content": m.Content}
		if len(m.ToolCalls) > 0 {
			calls := []map[string]any{}
			for _, c := range m.ToolCalls {
				calls = append(calls, map[string]any{"id": c.ID, "type": "function", "function": map[string]any{"name": c.Name, "arguments": string(c.Arguments)}})
			}
			msg["tool_calls"] = calls
		}
		if m.Role == "tool" {
			msg["tool_call_id"] = m.ToolCallID
		}
		out = append(out, msg)
	}
	return out
}

// complete makes one call on the OpenAI Chat Completions wire.
func (t *Tenant) complete(pv ai.Provider, model string, req ChatRequest) (ChatAnswer, ai.Usage, *AIError) {
	body := map[string]any{"model": model, "messages": openAIMessages(req.Messages)}
	if len(req.Tools) > 0 {
		tools := []map[string]any{}
		for _, x := range req.Tools {
			tools = append(tools, map[string]any{"type": "function", "function": map[string]any{"name": x.Name, "description": x.Description,
				"parameters": map[string]any{"type": "object", "properties": x.Properties, "required": x.Required}}})
		}
		body["tools"] = tools
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	if req.delta != nil && pv.Wire != "anthropic" {
		return t.completeStream(pv, model, body, req.delta)
	}
	raw, _ := json.Marshal(body)
	status, answer, failure := t.aiRequest(pv, http.MethodPost, "/chat/completions", raw, aiTimeout)
	if failure != nil {
		return ChatAnswer{}, ai.Usage{}, failure
	}
	return completion(status, answer, model)
}

// completion reads a chat completion's answer on the OpenAI wire.
func completion(status int, answer []byte, model string) (ChatAnswer, ai.Usage, *AIError) {
	var out struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
		Usage *struct {
			Prompt     *int     `json:"prompt_tokens"`
			Completion *int     `json:"completion_tokens"`
			Cost       *float64 `json:"cost"` // OpenRouter reports it
		} `json:"usage"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(answer, &out) != nil {
		return ChatAnswer{}, ai.Usage{}, &AIError{Status: status, Detail: "unreadable answer"}
	}
	if out.Error != nil || status >= 300 || len(out.Choices) == 0 {
		detail := http.StatusText(status)
		if out.Error != nil {
			detail = out.Error.Message
		}
		return ChatAnswer{}, ai.Usage{}, &AIError{Status: status, Detail: detail}
	}
	if out.Usage != nil && (out.Usage.Prompt != nil && *out.Usage.Prompt < 0 ||
		out.Usage.Completion != nil && *out.Usage.Completion < 0 ||
		out.Usage.Cost != nil && *out.Usage.Cost < 0) {
		return ChatAnswer{}, ai.Usage{}, &AIError{Status: http.StatusBadGateway, Detail: "the provider reported invalid model usage"}
	}
	u := ai.Usage{}
	if out.Usage != nil {
		if out.Usage.Prompt != nil && out.Usage.Completion != nil {
			u.Input, u.Output, u.TokensReported = *out.Usage.Prompt, *out.Usage.Completion, true
		}
		if out.Usage.Cost != nil {
			u.Cost, u.CostReported = *out.Usage.Cost, true
		}
	}
	if out.Model != "" && out.Model != model {
		u.Served = out.Model
	}
	msg := out.Choices[0].Message
	reply := ChatAnswer{Content: msg.Content}
	for _, c := range msg.ToolCalls {
		reply.ToolCalls = append(reply.ToolCalls, ToolCall{ID: c.ID, Name: c.Function.Name, Arguments: json.RawMessage(cmp.Or(c.Function.Arguments, "{}"))})
	}
	return reply, u, nil
}

// completeStream asks for the answer as server-sent events on the OpenAI
// wire, hands each piece of content to delta, and reads the usage the last
// event carries (stream_options.include_usage).
func (t *Tenant) completeStream(pv ai.Provider, model string, body map[string]any, delta func(string)) (ChatAnswer, ai.Usage, *AIError) {
	body["stream"], body["stream_options"] = true, map[string]any{"include_usage": true}
	raw, _ := json.Marshal(body)
	ctx, cancel := context.WithTimeout(context.Background(), aiTimeout)
	defer cancel()
	resp, failure := t.aiSend(ctx, pv, http.MethodPost, "/chat/completions", raw)
	if failure != nil {
		return ChatAnswer{}, ai.Usage{}, failure
	}
	defer resp.Body.Close()
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") { // a server that does not stream: the answer at once
		answer, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		reply, u, failure := completion(resp.StatusCode, answer, model)
		if failure == nil {
			delta(reply.Content)
		}
		return reply, u, failure
	}
	var content strings.Builder
	var u ai.Usage
	lines := bufio.NewScanner(resp.Body)
	lines.Buffer(make([]byte, 64<<10), 1<<20)
	for lines.Scan() {
		data, ok := strings.CutPrefix(lines.Text(), "data:")
		if !ok || strings.TrimSpace(data) == "[DONE]" {
			continue
		}
		var chunk struct {
			Model   string `json:"model"`
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
			Usage *struct {
				Prompt     *int     `json:"prompt_tokens"`
				Completion *int     `json:"completion_tokens"`
				Cost       *float64 `json:"cost"`
			} `json:"usage"`
		}
		if json.Unmarshal([]byte(data), &chunk) != nil {
			continue
		}
		if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
			content.WriteString(chunk.Choices[0].Delta.Content)
			delta(chunk.Choices[0].Delta.Content)
		}
		if chunk.Usage != nil {
			if chunk.Usage.Prompt != nil && *chunk.Usage.Prompt < 0 ||
				chunk.Usage.Completion != nil && *chunk.Usage.Completion < 0 ||
				chunk.Usage.Cost != nil && *chunk.Usage.Cost < 0 {
				return ChatAnswer{Content: content.String()}, ai.Usage{}, &AIError{Status: http.StatusBadGateway, Detail: "the provider reported invalid model usage"}
			}
			if chunk.Usage.Prompt != nil && chunk.Usage.Completion != nil {
				u.Input, u.Output, u.TokensReported = *chunk.Usage.Prompt, *chunk.Usage.Completion, true
			}
			if chunk.Usage.Cost != nil {
				u.Cost, u.CostReported = *chunk.Usage.Cost, true
			}
		}
		if chunk.Model != "" && chunk.Model != model {
			u.Served = chunk.Model
		}
	}
	if err := lines.Err(); err != nil {
		return ChatAnswer{Content: content.String()}, u, &AIError{Detail: "the stream broke: " + err.Error()}
	}
	return ChatAnswer{Content: content.String()}, u, nil
}

// aiRequest sends one request to a provider with its key, through the dialer
// that refuses private addresses unless the provider is local.
func (t *Tenant) aiRequest(pv ai.Provider, method, path string, body []byte, timeout time.Duration) (int, []byte, *AIError) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	resp, failure := t.aiSend(ctx, pv, method, path, body)
	if failure != nil {
		return 0, nil, failure
	}
	defer resp.Body.Close()
	answer, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	return resp.StatusCode, answer, nil
}

// aiSend sends a request to a provider with its key; the caller reads and closes the answer.
func (t *Tenant) aiSend(ctx context.Context, pv ai.Provider, method, path string, body []byte) (*http.Response, *AIError) {
	req, _ := http.NewRequestWithContext(ctx, method, pv.BaseURL+path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if pv.Secret != "" {
		key, ok := t.secret(pv.Secret)
		if !ok {
			return nil, &AIError{Detail: "secret " + pv.Secret + " missing"}
		}
		req.Header.Set("Authorization", "Bearer "+string(key))
	}
	if pv.Vendor == "openrouter" {
		req.Header.Set("X-Title", "Platform") // OpenRouter's app attribution
	}
	resp, err := t.aiHTTP(pv).Do(req)
	if err != nil {
		return nil, &AIError{Detail: "no answer: " + err.Error()}
	}
	return resp, nil
}

// aiHTTP is the client model calls go through: the test's, or one whose dialer
// refuses private addresses unless the provider is local.
func (t *Tenant) aiHTTP(pv ai.Provider) interface {
	Do(*http.Request) (*http.Response, error)
} {
	if t.AIClient != nil {
		return doer(t.AIClient)
	}
	dialer := guardedDialer(pv.Kind == "local")
	return &http.Client{Transport: &http.Transport{DialContext: dialer.DialContext, Proxy: nil},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

type doer func(*http.Request) (*http.Response, error)

func (d doer) Do(r *http.Request) (*http.Response, error) { return d(r) }

// CatalogModel is a model a provider offers.
type CatalogModel struct {
	ID      string `json:"id"`
	Name    string `json:"name,omitempty"`
	Context int    `json:"context,omitempty"`
	Free    bool   `json:"free,omitempty"` // priced at zero, as OpenRouter reports
}

var catalogs = struct {
	sync.Mutex
	byKey map[string]cachedCatalog
}{byKey: map[string]cachedCatalog{}}

type cachedCatalog struct {
	at     time.Time
	models []CatalogModel
}

// ProviderModels reads a provider's catalog live (GET /models), cached ten
// minutes; it is volatile, never journaled (ADR-0015 point 5). Administrators only.
func (t *Tenant) ProviderModels(m platform.Member, provider string, refresh bool) ([]CatalogModel, *kernel.Error, *AIError) {
	if t.ai == nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}, nil
	}
	if m.Roles[ai.ID] != ai.Admin {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}, nil
	}
	pv, ok := t.ai.Provider(provider)
	if !ok {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}, nil
	}
	key := t.ID + "/" + pv.ID + "/" + pv.BaseURL
	catalogs.Lock()
	c, cached := catalogs.byKey[key]
	catalogs.Unlock()
	if cached && !refresh && time.Since(c.at) < 10*time.Minute {
		return c.models, nil, nil
	}
	if pv.Wire == "anthropic" {
		models, failure := t.anthropicModels(pv)
		if failure != nil {
			return nil, nil, failure
		}
		catalogs.Lock()
		catalogs.byKey[key] = cachedCatalog{at: time.Now(), models: models}
		catalogs.Unlock()
		return models, nil, nil
	}
	status, answer, failure := t.aiRequest(pv, http.MethodGet, "/models", nil, 30*time.Second)
	if failure != nil {
		return nil, nil, failure
	}
	var out struct {
		Data []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Context int    `json:"context_length"`
			Pricing *struct {
				Prompt     string `json:"prompt"`
				Completion string `json:"completion"`
			} `json:"pricing"`
		} `json:"data"`
	}
	if status >= 300 || json.Unmarshal(answer, &out) != nil {
		return nil, nil, &AIError{Status: status, Detail: fmt.Sprintf("catalog: %s", strings.TrimSpace(string(answer[:min(len(answer), 200)])))}
	}
	models := []CatalogModel{}
	for _, d := range out.Data {
		x := CatalogModel{ID: d.ID, Name: d.Name, Context: d.Context}
		x.Free = d.Pricing != nil && d.Pricing.Prompt == "0" && d.Pricing.Completion == "0"
		models = append(models, x)
	}
	slices.SortFunc(models, func(a, b CatalogModel) int { return strings.Compare(a.ID, b.ID) })
	catalogs.Lock()
	catalogs.byKey[key] = cachedCatalog{at: time.Now(), models: models}
	catalogs.Unlock()
	return models, nil, nil
}

// Apps' model requests (ADR-0029 D3). An accepted decision's Request of a
// model becomes owned work of the built-in model destination: kept in the
// outbound queue with the effects, sent on the I/O lane through the door every
// call passes (as the app, within its limits), retried when the provider is
// unavailable, and answered to the app's Reply action as a journaled decision.
// A replay rebuilds the request from the decision and takes the answer from
// the journal: it asks no model.

const modelEndpoint = "model"

// modelAsk is a model request as the outbound queue keeps it.
type modelAsk struct {
	Model              string               `json:"model"`
	Prompt             platform.Prompt      `json:"prompt"`
	Reply              string               `json:"reply"`
	Record             string               `json:"record"` // the decision's target, "<type>/<id>", which the reply is on
	Call               string               `json:"call"`
	Function           *functionBinding     `json:"function,omitempty"`
	Evaluation         bool                 `json:"evaluation,omitempty"`
	EvaluationConfig   string               `json:"evaluationConfig,omitempty"`
	EvaluationFunction *platform.AIFunction `json:"evaluationFunction,omitempty"`
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
	s := rec.GetSubmission()
	// Resolve the administrator's default in the accepted decision. A queued
	// request must not switch models when settings change before dispatch or
	// retry. An empty binding stays empty and is refused without provider I/O.
	model := cmp.Or(q.Model, t.setting(t.automation(ai.ID, false), ai.SettingAppModel))
	body, _ := json.Marshal(modelAsk{Model: model, Prompt: prompt, Reply: q.Reply, Record: s.GetTarget().GetType() + "/" + s.GetTarget().GetId(), Call: q.Target, Evaluation: q.Evaluation, EvaluationConfig: q.EvaluationConfig, EvaluationFunction: q.EvaluationFunction})
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	n := 0
	for _, x := range t.outbound {
		if x.Endpoint == modelEndpoint && x.App == c.App && strings.HasPrefix(x.Key, rec.GetChangeId()+"#") {
			n++
		}
	}
	for _, x := range pending {
		if x.Endpoint == modelEndpoint && x.App == c.App && strings.HasPrefix(x.Key, rec.GetChangeId()+"#") {
			n++
		}
	}
	key := fmt.Sprintf("%s#%d", rec.GetChangeId(), n)
	return platform.Effect{ID: fmt.Sprintf("%s:%s:model:%s", t.ID, c.App, key), Endpoint: modelEndpoint,
		Event: c.App + "/model", App: c.App, Key: key, Target: q.Target, At: rec.GetRecordedTime().AsTime(), State: "pending", Due: rec.GetRecordedTime().AsTime(), Body: string(body)}
}

// sendModel makes one attempt of a model request: delivered with the answer,
// retried while the provider or a limit keeps it, rejected when it cannot be asked.
func (t *Tenant) sendModel(x platform.Effect, now time.Time) (platform.Outcome, *ai.Usage) {
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
	if ask.Evaluation {
		if ask.EvaluationFunction == nil || ask.EvaluationFunction.Check() != nil || ask.Prompt.System != ask.EvaluationFunction.SystemPrompt() ||
			ask.Prompt.MaxTokens != ask.EvaluationFunction.MaxTokens || len(ask.Prompt.User) > ask.EvaluationFunction.MaxInputBytes {
			out.Result, out.Detail = "rejected", "the evaluation function binding changed"
			return out, nil
		}
		config, digestErr := canonicalDigest([]any{enabled, pv})
		if digestErr != nil || ask.EvaluationConfig == "" || ask.EvaluationConfig != config {
			out.Result, out.Detail = "rejected", "the evaluation model configuration changed"
			return out, nil
		}
	}
	if !t.breakers.allow("ai:"+pv.ID, now) {
		out.Result, out.Detail = "retry", "the provider "+pv.ID+" failed repeatedly"
		return out, nil
	}
	if why := t.allowed(m, enabled, now); why != "" {
		out.Result, out.Detail = "retry", why
		return out, nil
	}
	answer, failure := t.call(pv, enabled, m, ChatRequest{Model: model, Messages: messages, MaxTokens: ask.Prompt.MaxTokens, run: x.ID}, now)
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
	return out, &answer.Usage
}

// answerModel submits a settled model request's answer to the app's Reply
// action, as the app, and journals it; a replay has it in the journal.
func (t *Tenant) answerModel(x platform.Effect, o platform.Outcome, usage *ai.Usage, now time.Time) {
	app, reply := t.modelAnswerSubmission(x, o, usage)
	if app == nil || reply == nil {
		return
	}
	c := t.automation(x.App, false)
	if _, err := app.Submit(c, reply, now); err != nil {
		log.Printf("tenant %s: %s refused the answer to its model request %s: %v", t.ID, x.App, x.ID, err) // a defect of the app
		return
	}
	t.journal(app, c.Member, reply, now)
}

func (t *Tenant) modelAnswerSubmission(x platform.Effect, o platform.Outcome, usage *ai.Usage) (platform.App, *pb.Submission) {
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
