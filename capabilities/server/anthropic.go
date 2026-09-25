package platformserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// Anthropic's Messages API through its official Go SDK (ADR-0015 point 3).
// The SDK does not retry: one call is one metered attempt, and a rate limit
// reaches the caller as it does for the OpenAI wire.

const anthropicMaxTokens = 16000 // when the caller names no limit (a non-streaming call)

func (t *Tenant) anthropicClient(pv Provider) (anthropic.Client, *AIError) {
	key, ok := t.secret(pv.Secret)
	if !ok {
		return anthropic.Client{}, &AIError{Detail: "secret " + pv.Secret + " missing"}
	}
	return anthropic.NewClient(option.WithAPIKey(string(key)), option.WithBaseURL(pv.BaseURL),
		option.WithHTTPClient(t.aiHTTP(pv)), option.WithMaxRetries(0), option.WithRequestTimeout(aiTimeout)), nil
}

func (t *Tenant) completeAnthropic(pv Provider, model string, req ChatRequest) (ChatAnswer, Usage, *AIError) {
	client, failure := t.anthropicClient(pv)
	if failure != nil {
		return ChatAnswer{}, Usage{}, failure
	}
	params := anthropic.MessageNewParams{Model: model, MaxTokens: anthropicMaxTokens}
	if req.MaxTokens > 0 {
		params.MaxTokens = int64(req.MaxTokens)
	}
	if req.Temperature != nil {
		params.Temperature = anthropic.Float(*req.Temperature)
	}
	for _, x := range req.Tools { // custom tools (ADR-0021): an agent's catalog actions, reads and built-ins
		tool := anthropic.ToolParam{Name: x.Name, Description: anthropic.String(x.Description),
			InputSchema: anthropic.ToolInputSchemaParam{Properties: x.Properties, Required: x.Required}}
		params.Tools = append(params.Tools, anthropic.ToolUnionParam{OfTool: &tool})
	}
	var results []anthropic.ContentBlockParamUnion // tool results, sent together as one user turn
	flush := func() {
		if len(results) > 0 {
			params.Messages = append(params.Messages, anthropic.NewUserMessage(results...))
			results = nil
		}
	}
	for _, m := range req.Messages {
		switch m.Role {
		case "system":
			params.System = append(params.System, anthropic.TextBlockParam{Text: m.Content})
		case "tool":
			results = append(results, anthropic.NewToolResultBlock(m.ToolCallID, m.Content, false))
		case "assistant":
			flush()
			var blocks []anthropic.ContentBlockParamUnion
			if m.Content != "" {
				blocks = append(blocks, anthropic.NewTextBlock(m.Content))
			}
			for _, c := range m.ToolCalls {
				blocks = append(blocks, anthropic.NewToolUseBlock(c.ID, c.Arguments, c.Name))
			}
			params.Messages = append(params.Messages, anthropic.NewAssistantMessage(blocks...))
		default:
			flush()
			params.Messages = append(params.Messages, anthropic.NewUserMessage(anthropic.NewTextBlock(m.Content)))
		}
	}
	flush()
	resp, err := client.Messages.New(context.Background(), params)
	if err != nil {
		return ChatAnswer{}, Usage{}, anthropicFailure(err)
	}
	u := Usage{Input: int(resp.Usage.InputTokens), Output: int(resp.Usage.OutputTokens)}
	if string(resp.Model) != model {
		u.Served = string(resp.Model)
	}
	if resp.StopReason == anthropic.StopReasonRefusal {
		return ChatAnswer{}, u, &AIError{Status: http.StatusOK, Detail: "refused (" + string(resp.StopDetails.Category) + "): " + resp.StopDetails.Explanation}
	}
	var answer ChatAnswer
	var text []string
	for _, block := range resp.Content {
		switch b := block.AsAny().(type) {
		case anthropic.TextBlock:
			text = append(text, b.Text)
		case anthropic.ToolUseBlock:
			answer.ToolCalls = append(answer.ToolCalls, ToolCall{ID: b.ID, Name: b.Name, Arguments: b.Input})
		}
	}
	answer.Content = strings.Join(text, "")
	return answer, u, nil
}

func (t *Tenant) anthropicModels(pv Provider) ([]CatalogModel, *AIError) {
	client, failure := t.anthropicClient(pv)
	if failure != nil {
		return nil, failure
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	models := []CatalogModel{}
	pages := client.Models.ListAutoPaging(ctx, anthropic.ModelListParams{})
	for pages.Next() {
		m := pages.Current()
		models = append(models, CatalogModel{ID: m.ID, Name: m.DisplayName, Context: int(m.MaxInputTokens)})
	}
	if err := pages.Err(); err != nil {
		return nil, anthropicFailure(err)
	}
	return models, nil
}

// anthropicFailure reads the API's status and message, or says no answer came.
func anthropicFailure(err error) *AIError {
	var apiErr *anthropic.Error
	if !errors.As(err, &apiErr) {
		return &AIError{Detail: "no answer: " + err.Error()}
	}
	var body struct {
		Error struct{ Message string } `json:"error"`
	}
	json.Unmarshal([]byte(apiErr.RawJSON()), &body)
	detail := body.Error.Message
	if detail == "" {
		detail = http.StatusText(apiErr.StatusCode)
	}
	return &AIError{Status: apiErr.StatusCode, Detail: detail}
}
