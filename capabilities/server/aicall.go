package platformserver

import (
	"bytes"
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
	"platformserver/platform"
)

// Model calls (ADR-0015 point 6): the host checks the caller's access, calls
// the provider outside the tenant's lock, and journals the call's usage as an
// entry of kind usage. Replay applies usage and never calls a model; prompts
// and answers are the caller's and are not kept.

const aiTimeout = 120 * time.Second

// Message is one turn of a conversation, as the OpenAI wire has it.
type Message struct {
	Role    string `json:"role"` // system, user, assistant
	Content string `json:"content"`
}

// ChatRequest is a member's call: a model by name ("<provider>/<model>") and the conversation.
type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	MaxTokens   int       `json:"maxTokens,omitempty"`
	Temperature *float64  `json:"temperature,omitempty"`
}

// ChatAnswer is what the model answered, with the call's usage.
type ChatAnswer struct {
	Content string `json:"content"`
	Usage   Usage  `json:"usage"`
}

// AIError is a call the provider did not answer or refused.
type AIError struct {
	Status int    `json:"status,omitempty"` // the provider's HTTP status
	Detail string `json:"detail"`
}

func (e *AIError) Error() string { return e.Detail }

// Chat calls a model for m. A refusal of access is a kernel error; a provider
// failure is an AIError, and its usage is journaled as failed.
func (t *Tenant) Chat(m platform.Member, req ChatRequest, now time.Time) (ChatAnswer, *kernel.Error, *AIError) {
	if t.ai == nil {
		return ChatAnswer{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}, nil
	}
	if len(req.Messages) == 0 {
		return ChatAnswer{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}, nil
	}
	model, pv, err := t.ai.callable(m, req.Model)
	if err != nil {
		return ChatAnswer{}, err, nil
	}
	started := time.Now()
	content, u, failure := t.complete(pv, model.Model, req)
	u.At, u.Member, u.Agent, u.Model, u.Millis, u.Outcome = now, m.ID, m.Agent, model.Name(), time.Since(started).Milliseconds(), "ok"
	if failure != nil {
		u.Outcome = failure.Detail
		if len(u.Outcome) > 300 {
			u.Outcome = u.Outcome[:300]
		}
	}
	t.meter(m, u)
	return ChatAnswer{Content: content, Usage: u}, nil, failure
}

// meter journals a call's usage, then applies it.
func (t *Tenant) meter(m platform.Member, u Usage) {
	t.mu.Lock()
	defer t.mu.Unlock()
	body, _ := json.Marshal(u)
	t.record(t.ai, "usage", m, body, u.At)
	t.ai.meter(u)
}

// complete makes one call on the OpenAI Chat Completions wire.
func (t *Tenant) complete(pv Provider, model string, req ChatRequest) (string, Usage, *AIError) {
	body := map[string]any{"model": model, "messages": req.Messages}
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
			Message Message `json:"message"`
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
		return "", Usage{}, failure
	}
	if json.Unmarshal(answer, &out) != nil {
		return "", Usage{}, &AIError{Status: status, Detail: "unreadable answer"}
	}
	if out.Error != nil || status >= 300 || len(out.Choices) == 0 {
		detail := http.StatusText(status)
		if out.Error != nil {
			detail = out.Error.Message
		}
		return "", Usage{}, &AIError{Status: status, Detail: detail}
	}
	u := Usage{Input: out.Usage.Prompt, Output: out.Usage.Completion, Cost: out.Usage.Cost}
	if out.Model != "" && out.Model != model {
		u.Served = out.Model
	}
	return out.Choices[0].Message.Content, u, nil
}

// aiRequest sends one request to a provider with its key, through the dialer
// that refuses private addresses unless the provider is local.
func (t *Tenant) aiRequest(pv Provider, method, path string, body []byte, timeout time.Duration) (int, []byte, *AIError) {
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
	send := t.AIClient
	if send == nil {
		dialer := guardedDialer(pv.Kind == "local")
		client := &http.Client{Transport: &http.Transport{DialContext: dialer.DialContext, Proxy: nil},
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		send = client.Do
	}
	resp, err := send(req)
	if err != nil {
		return 0, nil, &AIError{Detail: "no answer: " + err.Error()}
	}
	defer resp.Body.Close()
	answer, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	return resp.StatusCode, answer, nil
}

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
	if m.Roles[AIApp] != AIAdmin {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}, nil
	}
	t.ai.mu.Lock()
	pv, ok := t.ai.provider(provider)
	t.ai.mu.Unlock()
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
