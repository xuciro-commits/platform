package providers

import (
	"bufio"
	"cmp"
	"context"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"platformserver/platform"
	"slices"
	"strings"
	"time"
)

const aiTimeout = 120 * time.Second

func OpenAIMessages(ms []platform.Message) []map[string]any {
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

func (c Client) Complete(pv platform.AIProvider, model string, req platform.ChatRequest) (platform.ChatAnswer, platform.AIUsage, *platform.AIError) {
	body := map[string]any{"model": model, "messages": OpenAIMessages(req.Messages)}
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
		return platform.ChatAnswer{}, platform.AIUsage{}, &platform.AIError{Status: 400, Detail: "Temperature cannot be combined with DeepSeek thinking"}
	}
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	if req.Stream || req.Delta != nil || req.OnStream != nil {
		return c.completeStream(pv, model, body, req)
	}
	raw, _ := json.Marshal(body)
	status, answer, failure := c.Request(pv, http.MethodPost, "/chat/completions", raw, aiTimeout)
	if failure != nil {
		return platform.ChatAnswer{}, platform.AIUsage{}, failure
	}
	return Completion(status, answer, model)
}

type completionUsage struct {
	Prompt     *int     `json:"prompt_tokens"`
	Completion *int     `json:"completion_tokens"`
	Cost       *float64 `json:"cost"`
	Details    *struct {
		Cached *int `json:"cached_tokens"`
		Write  *int `json:"cache_write_tokens"`
	} `json:"prompt_tokens_details"`
}

func reportedCompletionUsage(usage *completionUsage) (platform.AIUsage, *platform.AIError) {
	u := platform.AIUsage{}
	if usage == nil {
		return u, nil
	}
	for _, n := range []*int{usage.Prompt, usage.Completion} {
		if n != nil && *n < 0 {
			return u, &platform.AIError{Status: http.StatusBadGateway, Detail: "the provider reported invalid model usage"}
		}
	}
	if usage.Prompt != nil && usage.Completion != nil {
		u.Input, u.Output, u.TokensReported = *usage.Prompt, *usage.Completion, true
	}
	if usage.Cost != nil {
		if *usage.Cost < 0 {
			return u, &platform.AIError{Status: http.StatusBadGateway, Detail: "the provider reported invalid model usage"}
		}
		u.Cost, u.CostReported = *usage.Cost, true
	}
	if usage.Details != nil {
		u.CacheReadInput, u.CacheWriteInput = usage.Details.Cached, usage.Details.Write
		remaining := u.Input
		for _, count := range []*int{u.CacheReadInput, u.CacheWriteInput} {
			if count != nil {
				if *count < 0 || u.TokensReported && *count > remaining {
					return platform.AIUsage{}, &platform.AIError{Status: http.StatusBadGateway, Detail: "the provider reported invalid model usage"}
				}
				remaining -= *count
			}
		}
	}
	return u, nil
}

func streamText(req platform.ChatRequest, text string) {
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

func streamTool(req platform.ChatRequest, call platform.ToolCallDelta) {
	if req.OnStream != nil {
		req.OnStream(platform.ChatStreamEvent{ToolCall: &call})
	}
}

func Completion(status int, answer []byte, model string) (platform.ChatAnswer, platform.AIUsage, *platform.AIError) {
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
		return platform.ChatAnswer{}, platform.AIUsage{}, &platform.AIError{Status: status, Detail: "unreadable answer"}
	}
	if out.Error != nil || status >= 300 || len(out.Choices) == 0 {
		detail := http.StatusText(status)
		if out.Error != nil {
			detail = out.Error.Message
		}
		return platform.ChatAnswer{}, platform.AIUsage{}, &platform.AIError{Status: status, Detail: detail}
	}
	u, failure := reportedCompletionUsage(out.Usage)
	if failure != nil {
		return platform.ChatAnswer{}, u, failure
	}
	if out.Model != "" && out.Model != model {
		u.Served = out.Model
	}
	msg := out.Choices[0].Message
	reply := platform.ChatAnswer{Content: msg.Content}
	if msg.Reasoning != "" {
		reply.Protocol, _ = json.Marshal(msg.Reasoning)
	}
	if len(msg.ToolCalls) > 0 && out.Choices[0].FinishReason == "length" {
		return reply, u, &platform.AIError{Status: http.StatusBadGateway, Detail: "the model's tool calls were cut off"}
	}
	for _, c := range msg.ToolCalls {
		args := json.RawMessage(cmp.Or(c.Function.Arguments, "{}"))
		if c.ID == "" || c.Function.Name == "" || !json.Valid(args) {
			return platform.ChatAnswer{Content: msg.Content}, u, &platform.AIError{Status: http.StatusBadGateway, Detail: "the provider returned an incomplete tool call"}
		}
		reply.ToolCalls = append(reply.ToolCalls, platform.ToolCall{ID: c.ID, Name: c.Function.Name, Arguments: args})
	}
	return reply, u, nil
}

func (c Client) completeStream(pv platform.AIProvider, model string, body map[string]any, req platform.ChatRequest) (platform.ChatAnswer, platform.AIUsage, *platform.AIError) {
	body["stream"], body["stream_options"] = true, map[string]any{"include_usage": true}
	raw, _ := json.Marshal(body)
	ctx, cancel := context.WithTimeout(context.Background(), aiTimeout)
	defer cancel()
	resp, failure := c.Send(ctx, pv, http.MethodPost, "/chat/completions", raw)
	if failure != nil {
		return platform.ChatAnswer{}, platform.AIUsage{}, failure
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		answer, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		reply, u, failure := Completion(resp.StatusCode, answer, model)
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
	calls := map[int]platform.ToolCall{}
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
			return platform.ChatAnswer{Content: content.String()}, u, &platform.AIError{Status: http.StatusBadGateway, Detail: "unreadable stream event"}
		}
		if chunk.Error != nil {
			return platform.ChatAnswer{Content: content.String()}, u, &platform.AIError{Status: http.StatusBadGateway, Detail: chunk.Error.Message}
		}
		if chunk.Usage != nil {
			served := u.Served
			u, failure = reportedCompletionUsage(chunk.Usage)
			u.Served = served
			if failure != nil {
				return platform.ChatAnswer{Content: content.String()}, u, failure
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
					return platform.ChatAnswer{Content: content.String()}, u, &platform.AIError{Status: http.StatusBadGateway, Detail: "invalid tool call index"}
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
				cutOff = true
			}
		}
	}
	if err := lines.Err(); err != nil {
		return platform.ChatAnswer{Content: content.String()}, u, &platform.AIError{Detail: "the stream broke: " + err.Error()}
	}
	if !complete {
		return platform.ChatAnswer{Content: content.String()}, u, &platform.AIError{Status: http.StatusBadGateway, Detail: "the stream ended before the answer was complete"}
	}
	if cutOff {
		return platform.ChatAnswer{Content: content.String()}, u, &platform.AIError{Status: http.StatusBadGateway, Detail: "the model's tool calls were cut off"}
	}
	answer := platform.ChatAnswer{Content: content.String()}
	if reasoning.Len() > 0 {
		answer.Protocol, _ = json.Marshal(reasoning.String())
	}
	for _, index := range slices.Sorted(maps.Keys(calls)) {
		if index != len(answer.ToolCalls) {
			return platform.ChatAnswer{Content: content.String()}, u, &platform.AIError{Status: http.StatusBadGateway, Detail: "invalid tool call index"}
		}
		call := calls[index]
		if len(call.Arguments) == 0 {
			call.Arguments = json.RawMessage("{}")
		}
		if call.ID == "" || call.Name == "" || !json.Valid(call.Arguments) {
			return platform.ChatAnswer{Content: content.String()}, u, &platform.AIError{Status: http.StatusBadGateway, Detail: "the provider returned an incomplete tool call"}
		}
		answer.ToolCalls = append(answer.ToolCalls, call)
	}
	return answer, u, nil
}
