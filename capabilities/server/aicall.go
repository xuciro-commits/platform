package platformserver

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
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
	run         string    // the agent run or evaluation the call is for, on its transcript
}

// ChatAnswer is what the model answered, with the call's usage.
type ChatAnswer struct {
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"toolCalls,omitempty"`
	Usage     ai.Usage      `json:"usage"`
}

// AIError is a call the provider did not answer or refused.
type AIError struct {
	Status int    `json:"status,omitempty"` // the provider's HTTP status
	Detail string `json:"detail"`
}

func (e *AIError) Error() string { return e.Detail }

// models is the AI app as the host calls models for members and agents: the
// enabled models and their providers, and the usage of every call.
type models interface {
	platform.App
	Callable(m platform.Member, name string) (ai.Model, ai.Provider, *kernel.Error)
	Model(name string) (ai.Model, ai.Provider, *kernel.Error)
	Provider(id string) (ai.Provider, bool)
	Meter(u ai.Usage)
	Spent(member string, now time.Time) int
}

// Chat calls a model for m. A refusal of access is a kernel error; a provider
// failure is an AIError, and its usage is journaled as failed.
func (t *Tenant) Chat(m platform.Member, req ChatRequest, now time.Time) (ChatAnswer, *kernel.Error, *AIError) {
	if t.ai == nil {
		return ChatAnswer{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}, nil
	}
	if len(req.Messages) == 0 {
		return ChatAnswer{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}, nil
	}
	model, pv, err := t.ai.Callable(m, req.Model)
	if err != nil {
		return ChatAnswer{}, err, nil
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
	answer, u, failure := complete(pv, model.Model, req)
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
	raw, _ := json.Marshal(body)
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
		Usage struct {
			Prompt     int     `json:"prompt_tokens"`
			Completion int     `json:"completion_tokens"`
			Cost       float64 `json:"cost"` // OpenRouter reports it
		} `json:"usage"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	status, answer, failure := t.aiRequest(pv, http.MethodPost, "/chat/completions", raw, aiTimeout)
	if failure != nil {
		return ChatAnswer{}, ai.Usage{}, failure
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
	u := ai.Usage{Input: out.Usage.Prompt, Output: out.Usage.Completion, Cost: out.Usage.Cost}
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

// aiRequest sends one request to a provider with its key, through the dialer
// that refuses private addresses unless the provider is local.
func (t *Tenant) aiRequest(pv ai.Provider, method, path string, body []byte, timeout time.Duration) (int, []byte, *AIError) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, method, pv.BaseURL+path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if pv.Secret != "" {
		key, ok := t.secret(pv.Secret)
		if !ok {
			return 0, nil, &AIError{Detail: "secret " + pv.Secret + " missing"}
		}
		req.Header.Set("Authorization", "Bearer "+string(key))
	}
	if pv.Vendor == "openrouter" {
		req.Header.Set("X-Title", "Platform") // OpenRouter's app attribution
	}
	resp, err := t.aiHTTP(pv).Do(req)
	if err != nil {
		return 0, nil, &AIError{Detail: "no answer: " + err.Error()}
	}
	defer resp.Body.Close()
	answer, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	return resp.StatusCode, answer, nil
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
