package providers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"platformserver/platform"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// Anthropic's Messages API through its official Go SDK.
// The SDK does not retry: one call is one metered attempt, and a rate limit
// reaches the caller as it does for the OpenAI wire.

const anthropicMaxTokens = 16000 // when the caller names no limit (a non-streaming call)

func (c Client) anthropicClient(pv platform.AIProvider) (anthropic.Client, *platform.AIError) {
	key, err := c.Credential(pv)
	if err != nil {
		return anthropic.Client{}, &platform.AIError{Detail: err.Error()}
	}
	return anthropic.NewClient(option.WithAPIKey(string(key)), option.WithBaseURL(pv.BaseURL),
		option.WithHTTPClient(c.HTTP), option.WithMaxRetries(0), option.WithRequestTimeout(aiTimeout)), nil
}

func (c Client) CompleteAnthropic(pv platform.AIProvider, model string, req platform.ChatRequest) (platform.ChatAnswer, platform.AIUsage, *platform.AIError) {
	client, failure := c.anthropicClient(pv)
	if failure != nil {
		return platform.ChatAnswer{}, platform.AIUsage{}, failure
	}
	params := anthropic.MessageNewParams{Model: model, MaxTokens: anthropicMaxTokens}
	if req.MaxTokens > 0 {
		params.MaxTokens = int64(req.MaxTokens)
	}
	if req.Temperature != nil {
		params.Temperature = anthropic.Float(*req.Temperature)
	}
	switch req.Options.Thinking {
	case "adaptive":
		params.Thinking = anthropic.ThinkingConfigParamUnion{OfAdaptive: &anthropic.ThinkingConfigAdaptiveParam{}}
	case "disabled":
		params.Thinking = anthropic.ThinkingConfigParamUnion{OfDisabled: &anthropic.ThinkingConfigDisabledParam{}}
	}
	if req.Options.Effort != "" {
		params.OutputConfig.Effort = anthropic.OutputConfigEffort(req.Options.Effort)
	}
	if req.Options.Thinking == "adaptive" && req.Temperature != nil {
		return platform.ChatAnswer{}, platform.AIUsage{}, &platform.AIError{Status: 400, Detail: "Temperature cannot be combined with adaptive thinking"}
	}
	for _, x := range req.Tools { // custom tools: an agent's catalog actions, reads and built-ins
		tool := anthropic.ToolParam{Name: x.Name, Description: anthropic.String(x.Description),
			InputSchema: anthropic.ToolInputSchemaParam{Properties: x.Properties, Required: x.Required, ExtraFields: map[string]any{"additionalProperties": false}}}
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
			if len(m.Protocol) > 0 {
				if err := json.Unmarshal(m.Protocol, &blocks); err != nil {
					return platform.ChatAnswer{}, platform.AIUsage{}, &platform.AIError{Status: 400, Detail: "Invalid protected model continuation"}
				}
			}
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
	if req.PromptCache {
		// Cache the stable tool/system prefix, never silently choose a
		// breakpoint inside the member's changing conversation.
		marker := anthropic.CacheControlEphemeralParam{Type: "ephemeral"}
		if n := len(params.System); n > 0 {
			params.System[n-1].CacheControl = marker
		} else if n := len(params.Tools); n > 0 {
			params.Tools[n-1].OfTool.CacheControl = marker
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), aiTimeout)
	defer cancel()
	if req.Stream || req.Delta != nil || req.OnStream != nil {
		return completeAnthropicStream(ctx, client, params, req, model)
	}
	resp, err := client.Messages.New(ctx, params)
	if err != nil {
		return platform.ChatAnswer{}, platform.AIUsage{}, anthropicFailure(err)
	}
	return AnthropicAnswer(*resp, model)
}

func AnthropicAnswer(resp anthropic.Message, model string) (platform.ChatAnswer, platform.AIUsage, *platform.AIError) {
	u, failure := reportedAnthropicUsage(resp.Usage)
	if failure != nil {
		return platform.ChatAnswer{}, u, failure
	}
	if string(resp.Model) != model {
		u.Served = string(resp.Model)
	}
	if resp.StopReason == anthropic.StopReasonRefusal {
		return platform.ChatAnswer{}, u, &platform.AIError{Status: http.StatusOK, Detail: "refused (" + string(resp.StopDetails.Category) + "): " + resp.StopDetails.Explanation}
	}
	var answer platform.ChatAnswer
	var text []string
	for _, block := range resp.Content {
		switch block.Type {
		case "thinking", "redacted_thinking":
			// Keep signed blocks only in protected session state; text/stream
			// projections never receive private reasoning.
			var blocks []json.RawMessage
			_ = json.Unmarshal(answer.Protocol, &blocks)
			blocks = append(blocks, json.RawMessage(block.RawJSON()))
			answer.Protocol, _ = json.Marshal(blocks)
		case "text":
			text = append(text, block.Text)
		case "tool_use":
			if block.ID == "" || block.Name == "" || !json.Valid(block.Input) {
				return platform.ChatAnswer{Content: strings.Join(text, "")}, u, &platform.AIError{Status: http.StatusBadGateway, Detail: "the provider returned an incomplete tool call"}
			}
			answer.ToolCalls = append(answer.ToolCalls, platform.ToolCall{ID: block.ID, Name: block.Name, Arguments: block.Input})
		}
	}
	answer.Content = strings.Join(text, "")
	if len(answer.ToolCalls) > 0 && resp.StopReason == anthropic.StopReasonMaxTokens {
		return platform.ChatAnswer{Content: answer.Content}, u, &platform.AIError{Status: http.StatusBadGateway, Detail: "the model's tool calls were cut off"}
	}
	return answer, u, nil
}

func reportedAnthropicUsage(usage anthropic.Usage) (platform.AIUsage, *platform.AIError) {
	u := platform.AIUsage{Input: int(usage.InputTokens + usage.CacheReadInputTokens + usage.CacheCreationInputTokens), Output: int(usage.OutputTokens),
		TokensReported: usage.JSON.InputTokens.Valid() && usage.JSON.OutputTokens.Valid()}
	if usage.InputTokens < 0 || usage.OutputTokens < 0 || usage.CacheReadInputTokens < 0 || usage.CacheCreationInputTokens < 0 {
		return platform.AIUsage{}, &platform.AIError{Status: http.StatusBadGateway, Detail: "the provider reported invalid model usage"}
	}
	if usage.JSON.CacheReadInputTokens.Valid() {
		n := int(usage.CacheReadInputTokens)
		u.CacheReadInput = &n
	}
	if usage.JSON.CacheCreationInputTokens.Valid() {
		n := int(usage.CacheCreationInputTokens)
		u.CacheWriteInput = &n
	}
	return u, nil
}

func completeAnthropicStream(ctx context.Context, client anthropic.Client, params anthropic.MessageNewParams, req platform.ChatRequest, model string) (platform.ChatAnswer, platform.AIUsage, *platform.AIError) {
	stream := client.Messages.NewStreaming(ctx, params)
	defer stream.Close()
	var message anthropic.Message
	complete := false
	finalUsage := false
	toolIndexes := map[int64]int{}
	failed := func(failure *platform.AIError) (platform.ChatAnswer, platform.AIUsage, *platform.AIError) {
		answer, usage, _ := AnthropicAnswer(message, model)
		answer.ToolCalls = nil // previews and partial calls are never executable
		usage.TokensReported = usage.TokensReported && finalUsage
		return answer, usage, failure
	}
	for stream.Next() {
		event := stream.Current()
		if event.Type == "content_block_stop" && event.Index >= 0 && event.Index < int64(len(message.Content)) {
			block := message.Content[event.Index]
			if block.Type == "tool_use" && !json.Valid(block.Input) {
				return failed(&platform.AIError{Status: http.StatusBadGateway, Detail: "the provider returned an incomplete tool call"})
			}
		}
		if err := message.Accumulate(event); err != nil {
			return failed(&platform.AIError{Status: http.StatusBadGateway, Detail: "unreadable stream event: " + err.Error()})
		}
		switch event.Type {
		case "content_block_start":
			block := event.ContentBlock
			if block.Type == "tool_use" {
				index := len(toolIndexes)
				toolIndexes[event.Index] = index
				streamTool(req, platform.ToolCallDelta{Index: index, ID: block.ID, Name: block.Name})
			} else if block.Type == "text" {
				streamText(req, block.Text)
			}
		case "content_block_delta":
			switch event.Delta.Type {
			case "text_delta":
				streamText(req, event.Delta.Text)
			case "input_json_delta":
				if index, ok := toolIndexes[event.Index]; ok {
					streamTool(req, platform.ToolCallDelta{Index: index, Arguments: event.Delta.PartialJSON})
				}
			}
		case "message_delta":
			// Accumulate updates counts; retain the presence of fields reported
			// only in the final event as well as those in message_start.
			if event.Usage.JSON.InputTokens.Valid() {
				message.Usage.JSON.InputTokens = event.Usage.JSON.InputTokens
			}
			if event.Usage.JSON.OutputTokens.Valid() {
				message.Usage.JSON.OutputTokens = event.Usage.JSON.OutputTokens
				finalUsage = true
			}
			if event.Usage.JSON.CacheReadInputTokens.Valid() {
				message.Usage.JSON.CacheReadInputTokens = event.Usage.JSON.CacheReadInputTokens
			}
			if event.Usage.JSON.CacheCreationInputTokens.Valid() {
				message.Usage.JSON.CacheCreationInputTokens = event.Usage.JSON.CacheCreationInputTokens
			}
		case "message_stop":
			complete = true
		}
	}
	if err := stream.Err(); err != nil {
		return failed(anthropicFailure(err))
	}
	if !complete {
		return failed(&platform.AIError{Status: http.StatusBadGateway, Detail: "the stream ended before the answer was complete"})
	}
	return AnthropicAnswer(message, model)
}

func (c Client) anthropicModels(pv platform.AIProvider) ([]json.RawMessage, *platform.AIError) {
	client, failure := c.anthropicClient(pv)
	if failure != nil {
		return nil, failure
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	models := []json.RawMessage{}
	pages := client.Models.ListAutoPaging(ctx, anthropic.ModelListParams{})
	for pages.Next() {
		m := pages.Current()
		models = append(models, json.RawMessage(m.RawJSON()))
	}
	if err := pages.Err(); err != nil {
		return nil, anthropicFailure(err)
	}
	return models, nil
}

// anthropicFailure reads the API's status and message, or says no answer came.
func anthropicFailure(err error) *platform.AIError {
	var apiErr *anthropic.Error
	if !errors.As(err, &apiErr) {
		return &platform.AIError{Detail: "no answer: " + err.Error()}
	}
	var body struct {
		Error struct{ Message string } `json:"error"`
	}
	json.Unmarshal([]byte(apiErr.RawJSON()), &body)
	detail := body.Error.Message
	if detail == "" {
		detail = http.StatusText(apiErr.StatusCode)
	}
	return &platform.AIError{Status: apiErr.StatusCode, Detail: detail}
}
