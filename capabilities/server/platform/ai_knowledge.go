package platform

import (
	"encoding/json"
	"strings"
	"unicode"
)

const (
	KnowledgeApp                   = "knowledge"
	KnowledgeDocumentType          = "knowledge.document"
	KnowledgeTermType              = "knowledge.term"
	SettingKnowledgeEmbeddingModel = "embedding-model"
	SettingKnowledgeEmbeddingNext  = "embedding-next-model"
)

type KnowledgeDocument struct {
	Record
	Title  string   `json:"title" field:"required,search"`
	Text   string   `json:"text" field:"required" type:"longtext"`
	Source string   `json:"source,omitempty" title:"Where it comes from"`
	Apps   []string `json:"apps,omitempty" title:"Read by members of"`
}

type KnowledgeTerm struct {
	Record
	Term     string   `json:"term" field:"required,search" help:"The word people here use" example:"PO"`
	Meaning  string   `json:"meaning" field:"required" type:"longtext" help:"What it means in this organisation"`
	Synonyms string   `json:"synonyms,omitempty" help:"Other words for it, comma-separated"`
	RefersTo string   `json:"refersTo,omitempty" title:"Refers to" help:"The declaration it names: an entity type, <type>.<field> or an action" example:"core.material"`
	Apps     []string `json:"apps,omitempty" title:"Read by members of"`
}

type KnowledgeHost interface {
	Declares(name string) bool
	Automation(parent Caller, app string) Caller
}

type Glossary interface {
	Terms(app string) []KnowledgeTerm
	TermsFor(declaration string) []string
}

type AttachedKnowledge interface{ AttachKnowledge(KnowledgeHost) }

type Passage struct {
	Document string  `json:"document"`
	Title    string  `json:"title"`
	Chunk    int     `json:"chunk"`
	Text     string  `json:"text"`
	Score    float64 `json:"score"`
}

func TokenizeWords(s string) []string {
	var out []string
	var cur []rune
	flush := func() {
		if len(cur) > 1 || len(cur) == 1 && unicode.IsDigit(cur[0]) {
			out = append(out, string(cur))
		}
		cur = cur[:0]
	}
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul):
			flush()
			out = append(out, string(r))
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			cur = append(cur, r)
		default:
			flush()
		}
	}
	flush()
	return out
}

type KnowledgeIndexStatus struct {
	NeedsRebuild bool   `json:"needsRebuild,omitempty"`
	Active       string `json:"active"`
	Preparing    string `json:"preparing"`
	Total        int    `json:"total"`
	Completed    int    `json:"completed"`
	Dimensions   int    `json:"dimensions,omitempty"`
	Ready        bool   `json:"ready"`
	Problem      string `json:"problem,omitempty"`
}

type Message struct {
	Role       string          `json:"role"`
	Content    string          `json:"content"`
	ToolCalls  []ToolCall      `json:"toolCalls,omitempty"`
	ToolCallID string          `json:"toolCallId,omitempty"`
	Protocol   json.RawMessage `json:"-"`
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

type ToolCallDelta struct {
	Index     int    `json:"index"`
	ID        string `json:"id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type ChatStreamEvent struct {
	Content  string         `json:"content,omitempty"`
	ToolCall *ToolCallDelta `json:"toolCall,omitempty"`
}

type ChatRequest struct {
	RequestHash string                `json:"-"`
	Call        string                `json:"-"`
	RunTokens   int                   `json:"-"`
	Model       string                `json:"model"`
	Messages    []Message             `json:"messages"`
	Tools       []Tool                `json:"tools,omitempty"`
	MaxTokens   int                   `json:"maxTokens,omitempty"`
	Temperature *float64              `json:"temperature,omitempty"`
	Options     ModelOptions          `json:"options,omitzero"`
	Binding     *AIModelBinding       `json:"-"`
	PromptCache bool                  `json:"promptCache,omitempty"`
	Stream      bool                  `json:"stream,omitempty"`
	Run         string                `json:"-"`
	Delta       func(string)          `json:"-"`
	OnStream    func(ChatStreamEvent) `json:"-"`
}

type ChatAnswer struct {
	Content   string          `json:"content"`
	ToolCalls []ToolCall      `json:"toolCalls,omitempty"`
	Usage     AIUsage         `json:"usage"`
	Protocol  json.RawMessage `json:"-"`
}

type AIError struct {
	Status int    `json:"status,omitempty"`
	Detail string `json:"detail"`
	Quota  bool   `json:"quota,omitempty"`
}

func (e *AIError) Error() string { return e.Detail }
