package platform

import "encoding/json"

type Message struct {
	Role       string     `json:"role"` // system, user, assistant, tool
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"toolCalls,omitempty"`
	ToolCallID string     `json:"toolCallId,omitempty"` // a tool turn: the call it answers
}

type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Properties  map[string]any `json:"properties"`
	Required    []string       `json:"required,omitempty"`
}

type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Tools       []Tool    `json:"tools,omitempty"`
	MaxTokens   int       `json:"maxTokens,omitempty"`
	Temperature *float64  `json:"temperature,omitempty"`
	// Stream answers token by token as server-sent events (ADR-0029 D2);
	// usage is journaled once, when the call ends.
	Stream bool         `json:"stream,omitempty"`
	Run    string       `json:"-"` // the agent run or evaluation the call is for, on its transcript
	Delta  func(string) `json:"-"` // where a streamed call's tokens go
}

type ChatAnswer struct {
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"toolCalls,omitempty"`
	Usage     AIUsage    `json:"usage"`
}

type AIError struct {
	Status int    `json:"status,omitempty"` // the provider's HTTP status
	Detail string `json:"detail"`
	Quota  bool   `json:"quota,omitempty"`
}

func (e *AIError) Error() string { return e.Detail }
