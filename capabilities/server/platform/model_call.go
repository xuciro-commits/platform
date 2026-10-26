package platform

import "encoding/json"

type Message struct {
	Role       string          `json:"role"` // system, user, assistant, tool
	Content    string          `json:"content"`
	ToolCalls  []ToolCall      `json:"toolCalls,omitempty"`
	ToolCallID string          `json:"toolCallId,omitempty"` // a tool turn: the call it answers
	Protocol   json.RawMessage `json:"-"`                    // protected adapter continuation, never a public reasoning field
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

// ToolCallDelta is a preview of one provider call while its arguments are still
// arriving. Arguments is text, not JSON yet, and must never be executed.
type ToolCallDelta struct {
	Index     int    `json:"index"` // position in the final ChatAnswer.ToolCalls
	ID        string `json:"id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type ChatStreamEvent struct {
	Content  string         `json:"content,omitempty"`
	ToolCall *ToolCallDelta `json:"toolCall,omitempty"`
}

type ChatRequest struct {
	Model       string          `json:"model"`
	Messages    []Message       `json:"messages"`
	Tools       []Tool          `json:"tools,omitempty"`
	MaxTokens   int             `json:"maxTokens,omitempty"`
	Temperature *float64        `json:"temperature,omitempty"`
	Options     ModelOptions    `json:"options,omitzero"`
	Binding     *AIModelBinding `json:"-"` // supplied only by the accepting owner, never trusted from HTTP
	// PromptCache opts into an explicit cache breakpoint for providers that
	// require one. It does not disable providers' automatic prompt caching.
	PromptCache bool `json:"promptCache,omitempty"`
	// Stream answers token by token as server-sent events;
	// usage is journaled once, when the call ends.
	Stream   bool                  `json:"stream,omitempty"`
	Run      string                `json:"-"` // the agent run or evaluation the call is for, on its transcript
	Delta    func(string)          `json:"-"` // where a streamed call's text goes
	OnStream func(ChatStreamEvent) `json:"-"` // text and tool previews; ChatAnswer owns the complete calls
}

type ChatAnswer struct {
	Content   string          `json:"content"`
	ToolCalls []ToolCall      `json:"toolCalls,omitempty"`
	Usage     AIUsage         `json:"usage"`
	Protocol  json.RawMessage `json:"-"`
}

type AIError struct {
	Status int    `json:"status,omitempty"` // the provider's HTTP status
	Detail string `json:"detail"`
	Quota  bool   `json:"quota,omitempty"`
}

func (e *AIError) Error() string { return e.Detail }
