package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"platformserver/platform"
	"time"
)

type Client struct {
	HTTP interface {
		Do(*http.Request) (*http.Response, error)
	}
	Credential func(platform.AIProvider) ([]byte, error)
}

// aiRequest sends one request to a provider with its key, through the dialer
// that refuses private addresses unless the provider is local.
func (c Client) Request(pv platform.AIProvider, method, path string, body []byte, timeout time.Duration) (int, []byte, *platform.AIError) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	resp, failure := c.Send(ctx, pv, method, path, body)
	if failure != nil {
		return 0, nil, failure
	}
	defer resp.Body.Close()
	answer, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	return resp.StatusCode, answer, nil
}

// aiSend sends a request to a provider with its key; the caller reads and closes the answer.
func (c Client) Send(ctx context.Context, pv platform.AIProvider, method, path string, body []byte) (*http.Response, *platform.AIError) {
	req, _ := http.NewRequestWithContext(ctx, method, pv.BaseURL+path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if pv.Secret != "" {
		key, err := c.Credential(pv)
		if err != nil {
			return nil, &platform.AIError{Detail: err.Error()}
		}
		req.Header.Set("Authorization", "Bearer "+string(key))
	}
	if pv.Vendor == "openrouter" {
		req.Header.Set("X-Title", "Platform") // OpenRouter's app attribution
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, &platform.AIError{Detail: "no answer: " + err.Error()}
	}
	return resp, nil
}

// Models returns provider wire entries; reviewed capability adoption belongs to
// the original configuration owner, not the transport adapter.
func (c Client) Models(pv platform.AIProvider) ([]json.RawMessage, *platform.AIError) {
	if pv.Wire == "anthropic" {
		return c.anthropicModels(pv)
	}
	entries := []json.RawMessage{}
	path, seen := "/models", map[string]bool{}
	for {
		status, raw, failure := c.Request(pv, http.MethodGet, path, nil, 30*time.Second)
		if failure != nil {
			return nil, failure
		}
		var page struct {
			Data    []json.RawMessage `json:"data"`
			HasMore bool              `json:"has_more"`
			LastID  string            `json:"last_id"`
		}
		if status >= 300 || json.Unmarshal(raw, &page) != nil {
			return nil, &platform.AIError{Status: status, Detail: fmt.Sprintf("catalog: %s", raw[:min(len(raw), 200)])}
		}
		entries = append(entries, page.Data...)
		if !page.HasMore {
			return entries, nil
		}
		if page.LastID == "" || seen[page.LastID] {
			return nil, &platform.AIError{Status: 502, Detail: "The provider returned an invalid catalog cursor"}
		}
		seen[page.LastID] = true
		path = "/models?after_id=" + url.QueryEscape(page.LastID)
	}
}

func (c Client) Embeddings(pv platform.AIProvider, model string, input []string) (int, []byte, *platform.AIError) {
	body, _ := json.Marshal(map[string]any{"model": model, "input": input})
	return c.Request(pv, http.MethodPost, "/embeddings", body, aiTimeout)
}
