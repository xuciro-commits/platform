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
	"maps"
	"net/http"
	resultformat "platformserver/internal/acceptance/result"
	"slices"
	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
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
	Reserve(m platform.Member, model platform.AIModel, d platform.AIDefaults, now time.Time) string
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

// reserve takes the door every model call passes — members', agents',
// evaluations' and embeddings': it refuses with why, or lets the call start
// and counts it in flight until its usage is metered, both under one lock.
// Calls that start together would each pass the same limit without it.
func (t *Tenant) reserve(m platform.Member, model platform.AIModel, now time.Time) string {
	if t.ai == nil {
		return ""
	}
	return t.ai.Reserve(m, model, t.limits(), now)
}

// mostTokens is the most tokens one answer may ask for (platform setting); 0:
// the provider's own default. An answer a caller asks to be longer is capped.
func (t *Tenant) mostTokens() int {
	n, _ := strconv.Atoi(t.setting(t.automation(platform.AIApp, false), platform.AISettingMaxTokens))
	return n
}

// Chat calls a model for m. A refusal of access is a kernel error; a provider
// failure is an AIError, and its usage is journaled as failed. delta, when
// given, receives the answer as it comes.
func (t *Tenant) Chat(m platform.Member, req ChatRequest, now time.Time, delta ...func(string)) (ChatAnswer, *kernel.Error, *AIError) {
	if t.ai == nil {
		return ChatAnswer{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}, nil
	}
	if len(req.Messages) == 0 {
		return ChatAnswer{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}, nil
	}
	var model platform.AIModel
	var pv platform.AIProvider
	var preparationFailure *AIError
	_, refusal := recordRead(t, context.Background(), func() (struct{}, *kernel.Error) {
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
		if !t.breakers.allow("ai:"+pv.ID, now) {
			preparationFailure = &AIError{Status: http.StatusServiceUnavailable, Detail: "the provider " + pv.ID + " failed repeatedly; its calls wait until it answers again"}
			return struct{}{}, nil
		}
		if why := t.reserve(m, model, now); why != "" {
			preparationFailure = &AIError{Status: http.StatusTooManyRequests, Detail: why, Quota: true}
		}
		return struct{}{}, nil
	})
	if refusal != nil {
		return ChatAnswer{}, refusal, nil
	}
	if preparationFailure != nil {
		return ChatAnswer{}, nil, preparationFailure
	}
	if len(delta) > 0 {
		req.Delta = delta[0]
	}
	answer, failure := t.call(pv, model, m, req, now)
	if err := t.meter(m, answer.Usage); err != nil {
		return ChatAnswer{}, err, nil
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
	}{openAIMessages(req.Messages), req.Tools})
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
	started := time.Now()
	complete := t.complete
	if pv.Wire == "anthropic" {
		complete = t.completeAnthropic
	}
	span := outside(trace.SpanContext{}, "chat "+model.Model, attribute.String("gen_ai.operation.name", "chat"),
		attribute.String("gen_ai.provider.name", pv.ID), attribute.String("gen_ai.request.model", model.Model), attribute.String("platform.tenant", t.ID))
	var answer ChatAnswer
	var u platform.AIUsage
	var failure *AIError
	if configErr != nil {
		failure = &AIError{Status: 400, Detail: configErr.Error()}
	} else {
		answer, u, failure = complete(pv, model.Model, req)
	}
	if !u.TokensReported && configErr == nil {
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
func openAIMessages(ms []Message) []map[string]any {
	out := make([]map[string]any, 0, len(ms))
	for _, m := range ms {
		msg := map[string]any{"role": m.Role, "content": m.Content}
		if len(m.Protocol) > 0 {
			var reasoning string
			if json.Unmarshal(m.Protocol, &reasoning) == nil {
				msg["reasoning_content"] = reasoning
			}
		}
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
func (t *Tenant) complete(pv platform.AIProvider, model string, req ChatRequest) (ChatAnswer, platform.AIUsage, *AIError) {
	body := map[string]any{"model": model, "messages": openAIMessages(req.Messages)}
	if len(req.Tools) > 0 {
		tools := []map[string]any{}
		for _, x := range req.Tools {
			tools = append(tools, map[string]any{"type": "function", "function": map[string]any{"name": x.Name, "description": x.Description,
				"parameters": map[string]any{"type": "object", "properties": x.Properties, "required": x.Required, "additionalProperties": false}}})
		}
		body["tools"] = tools
	}
	if req.MaxTokens > 0 {
		if pv.Vendor == "openai" {
			body["max_completion_tokens"] = req.MaxTokens
		} else {
			body["max_tokens"] = req.MaxTokens
		}
	}
	if req.Options.Thinking != "" {
		body["thinking"] = map[string]any{"type": req.Options.Thinking}
	}
	if req.Options.Effort != "" {
		body["reasoning_effort"] = req.Options.Effort
	}
	if (pv.Vendor == "deepseek" || req.Binding != nil && req.Binding.Capabilities != nil && req.Binding.Capabilities.Adapter == "deepseek") && req.Options.Thinking != "disabled" && req.Temperature != nil {
		return ChatAnswer{}, platform.AIUsage{}, &AIError{Status: 400, Detail: "Temperature cannot be combined with DeepSeek thinking"}
	}
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	if req.Stream || req.Delta != nil || req.OnStream != nil {
		return t.completeStream(pv, model, body, req)
	}
	raw, _ := json.Marshal(body)
	status, answer, failure := t.aiRequest(pv, http.MethodPost, "/chat/completions", raw, aiTimeout)
	if failure != nil {
		return ChatAnswer{}, platform.AIUsage{}, failure
	}
	return completion(status, answer, model)
}

type completionUsage struct {
	Prompt     *int     `json:"prompt_tokens"`
	Completion *int     `json:"completion_tokens"`
	Cost       *float64 `json:"cost"` // compatible providers may report it
	Details    *struct {
		Cached *int `json:"cached_tokens"`
		Write  *int `json:"cache_write_tokens"`
	} `json:"prompt_tokens_details"`
}

func reportedCompletionUsage(usage *completionUsage) (platform.AIUsage, *AIError) {
	u := platform.AIUsage{}
	if usage == nil {
		return u, nil
	}
	for _, n := range []*int{usage.Prompt, usage.Completion} {
		if n != nil && *n < 0 {
			return u, &AIError{Status: http.StatusBadGateway, Detail: "the provider reported invalid model usage"}
		}
	}
	if usage.Prompt != nil && usage.Completion != nil {
		u.Input, u.Output, u.TokensReported = *usage.Prompt, *usage.Completion, true
	}
	if usage.Cost != nil {
		if *usage.Cost < 0 {
			return u, &AIError{Status: http.StatusBadGateway, Detail: "the provider reported invalid model usage"}
		}
		u.Cost, u.CostReported = *usage.Cost, true
	}
	if usage.Details != nil {
		u.CacheReadInput, u.CacheWriteInput = usage.Details.Cached, usage.Details.Write
		remaining := u.Input
		for _, count := range []*int{u.CacheReadInput, u.CacheWriteInput} {
			if count != nil {
				if *count < 0 || u.TokensReported && *count > remaining {
					return platform.AIUsage{}, &AIError{Status: http.StatusBadGateway, Detail: "the provider reported invalid model usage"}
				}
				remaining -= *count
			}
		}
	}
	return u, nil
}

func streamText(req ChatRequest, text string) {
	if text == "" {
		return
	}
	if req.Delta != nil {
		req.Delta(text)
	}
	if req.OnStream != nil {
		req.OnStream(platform.ChatStreamEvent{Content: text})
	}
}

func streamTool(req ChatRequest, call platform.ToolCallDelta) {
	if req.OnStream != nil {
		req.OnStream(platform.ChatStreamEvent{ToolCall: &call})
	}
}

// completion reads a chat completion's answer on the OpenAI wire.
func completion(status int, answer []byte, model string) (ChatAnswer, platform.AIUsage, *AIError) {
	var out struct {
		Model   string `json:"model"`
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content   string `json:"content"`
				Reasoning string `json:"reasoning_content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
		Usage *completionUsage `json:"usage"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(answer, &out) != nil {
		return ChatAnswer{}, platform.AIUsage{}, &AIError{Status: status, Detail: "unreadable answer"}
	}
	if out.Error != nil || status >= 300 || len(out.Choices) == 0 {
		detail := http.StatusText(status)
		if out.Error != nil {
			detail = out.Error.Message
		}
		return ChatAnswer{}, platform.AIUsage{}, &AIError{Status: status, Detail: detail}
	}
	u, failure := reportedCompletionUsage(out.Usage)
	if failure != nil {
		return ChatAnswer{}, u, failure
	}
	if out.Model != "" && out.Model != model {
		u.Served = out.Model
	}
	msg := out.Choices[0].Message
	reply := ChatAnswer{Content: msg.Content}
	if msg.Reasoning != "" {
		reply.Protocol, _ = json.Marshal(msg.Reasoning)
	}
	if len(msg.ToolCalls) > 0 && out.Choices[0].FinishReason == "length" {
		return reply, u, &AIError{Status: http.StatusBadGateway, Detail: "the model's tool calls were cut off"}
	}
	for _, c := range msg.ToolCalls {
		args := json.RawMessage(cmp.Or(c.Function.Arguments, "{}"))
		if c.ID == "" || c.Function.Name == "" || !json.Valid(args) {
			return ChatAnswer{Content: msg.Content}, u, &AIError{Status: http.StatusBadGateway, Detail: "the provider returned an incomplete tool call"}
		}
		reply.ToolCalls = append(reply.ToolCalls, ToolCall{ID: c.ID, Name: c.Function.Name, Arguments: args})
	}
	return reply, u, nil
}

// completeStream asks for the answer as server-sent events on the OpenAI
// wire, hands each piece of content to delta, and reads the usage the last
// event carries (stream_options.include_usage).
func (t *Tenant) completeStream(pv platform.AIProvider, model string, body map[string]any, req ChatRequest) (ChatAnswer, platform.AIUsage, *AIError) {
	body["stream"], body["stream_options"] = true, map[string]any{"include_usage": true}
	raw, _ := json.Marshal(body)
	ctx, cancel := context.WithTimeout(context.Background(), aiTimeout)
	defer cancel()
	resp, failure := t.aiSend(ctx, pv, http.MethodPost, "/chat/completions", raw)
	if failure != nil {
		return ChatAnswer{}, platform.AIUsage{}, failure
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") { // a server that does not stream: the answer at once
		answer, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		reply, u, failure := completion(resp.StatusCode, answer, model)
		if failure == nil {
			streamText(req, reply.Content)
			for i, call := range reply.ToolCalls {
				streamTool(req, platform.ToolCallDelta{Index: i, ID: call.ID, Name: call.Name, Arguments: string(call.Arguments)})
			}
		}
		return reply, u, failure
	}
	var content strings.Builder
	var reasoning strings.Builder
	var u platform.AIUsage
	calls := map[int]ToolCall{}
	complete := false
	cutOff := false
	lines := bufio.NewScanner(resp.Body)
	lines.Buffer(make([]byte, 64<<10), 1<<20)
	for lines.Scan() {
		data, ok := strings.CutPrefix(lines.Text(), "data:")
		if !ok {
			continue
		}
		if strings.TrimSpace(data) == "[DONE]" {
			complete = true
			break
		}
		var chunk struct {
			Model   string `json:"model"`
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					Reasoning string `json:"reasoning_content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
			Usage *completionUsage `json:"usage"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal([]byte(data), &chunk) != nil {
			return ChatAnswer{Content: content.String()}, u, &AIError{Status: http.StatusBadGateway, Detail: "unreadable stream event"}
		}
		if chunk.Error != nil {
			return ChatAnswer{Content: content.String()}, u, &AIError{Status: http.StatusBadGateway, Detail: chunk.Error.Message}
		}
		if chunk.Usage != nil {
			served := u.Served
			u, failure = reportedCompletionUsage(chunk.Usage)
			u.Served = served
			if failure != nil {
				return ChatAnswer{Content: content.String()}, u, failure
			}
		}
		if chunk.Model != "" && chunk.Model != model {
			u.Served = chunk.Model
		}
		if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
			content.WriteString(chunk.Choices[0].Delta.Content)
			streamText(req, chunk.Choices[0].Delta.Content)
		}
		if len(chunk.Choices) > 0 {
			reasoning.WriteString(chunk.Choices[0].Delta.Reasoning)
			choice := chunk.Choices[0]
			for _, call := range choice.Delta.ToolCalls {
				if call.Index < 0 {
					return ChatAnswer{Content: content.String()}, u, &AIError{Status: http.StatusBadGateway, Detail: "invalid tool call index"}
				}
				c := calls[call.Index]
				c.ID += call.ID
				c.Name += call.Function.Name
				c.Arguments = append(c.Arguments, call.Function.Arguments...)
				calls[call.Index] = c
				streamTool(req, platform.ToolCallDelta{Index: call.Index, ID: call.ID, Name: call.Function.Name, Arguments: call.Function.Arguments})
			}
			if choice.FinishReason != "" {
				complete = true
			}
			if len(calls) > 0 && choice.FinishReason == "length" {
				// The usage event can follow the finish reason. Read it before
				// rejecting the incomplete calls so the incurred usage is kept.
				cutOff = true
			}
		}
	}
	if err := lines.Err(); err != nil {
		return ChatAnswer{Content: content.String()}, u, &AIError{Detail: "the stream broke: " + err.Error()}
	}
	if !complete {
		return ChatAnswer{Content: content.String()}, u, &AIError{Status: http.StatusBadGateway, Detail: "the stream ended before the answer was complete"}
	}
	if cutOff {
		return ChatAnswer{Content: content.String()}, u, &AIError{Status: http.StatusBadGateway, Detail: "the model's tool calls were cut off"}
	}
	answer := ChatAnswer{Content: content.String()}
	if reasoning.Len() > 0 {
		answer.Protocol, _ = json.Marshal(reasoning.String())
	}
	for _, index := range slices.Sorted(maps.Keys(calls)) {
		if index != len(answer.ToolCalls) {
			return ChatAnswer{Content: content.String()}, u, &AIError{Status: http.StatusBadGateway, Detail: "invalid tool call index"}
		}
		call := calls[index]
		if len(call.Arguments) == 0 {
			call.Arguments = json.RawMessage("{}")
		}
		if call.ID == "" || call.Name == "" || !json.Valid(call.Arguments) {
			return ChatAnswer{Content: content.String()}, u, &AIError{Status: http.StatusBadGateway, Detail: "the provider returned an incomplete tool call"}
		}
		answer.ToolCalls = append(answer.ToolCalls, call)
	}
	return answer, u, nil
}

// aiRequest sends one request to a provider with its key, through the dialer
// that refuses private addresses unless the provider is local.
func (t *Tenant) aiRequest(pv platform.AIProvider, method, path string, body []byte, timeout time.Duration) (int, []byte, *AIError) {
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
func (t *Tenant) aiSend(ctx context.Context, pv platform.AIProvider, method, path string, body []byte) (*http.Response, *AIError) {
	req, _ := http.NewRequestWithContext(ctx, method, pv.BaseURL+path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if pv.Secret != "" {
		key, _, err := t.resolveCredential(pv.Secret, "ai/"+pv.ID, pv.BaseURL)
		if err != nil {
			return nil, &AIError{Detail: err.Error()}
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
func (t *Tenant) aiHTTP(pv platform.AIProvider) interface {
	Do(*http.Request) (*http.Response, error)
} {
	send := t.AIClient
	if send == nil {
		dialer := guardedDialer(pv.Kind == "local")
		client := &http.Client{Transport: &http.Transport{DialContext: dialer.DialContext, Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		send = client.Do
	}
	return doer(func(request *http.Request) (*http.Response, error) {
		response, err := send(request)
		return redactCredentialFailure(response, request), err
	})
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
			binding = &resolved
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
			evaluatedBinding = &current
		}
		config, digestErr := modelConfigurationDigest(enabled, pv, evaluatedBinding)
		if digestErr != nil || ask.EvaluationConfig == "" || ask.EvaluationConfig != config {
			out.Result, out.Detail = "rejected", "the evaluation model configuration changed"
			return out, nil
		}
	}
	if !t.breakers.allow("ai:"+pv.ID, now) {
		out.Result, out.Detail = "retry", "the provider "+pv.ID+" failed repeatedly"
		return out, nil
	}
	if why := t.reserve(m, enabled, now); why != "" {
		out.Result, out.Detail = "retry", why
		return out, nil
	}
	answer, failure := t.call(pv, enabled, m, ChatRequest{Model: model, Messages: messages, MaxTokens: ask.Prompt.MaxTokens, Binding: ask.Binding, Run: x.ID}, now)
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
