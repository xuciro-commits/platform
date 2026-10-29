package agents

import (
	"cmp"
	"encoding/json"
	"fmt"
	"platformserver/platform"
	"slices"
)

const preamble = `You are an agent of a business platform. You act only through the tools given: each action is a decision recorded with your name, and what cannot be undone waits for a person. Give a one-sentence rationale with every tool call. Read before you act; ask a person when you are unsure; end with finish and the result the goal asks for.`

type HistoryStep struct {
	Calls                                                []platform.ToolCall
	Content, Tool, CallID, Outcome, Rationale, Arguments string
}

type Prompt struct {
	Goal, OnBehalf, Ref, Context              string
	HasContext                                bool
	Instructions, Meaning, Glossary, Language string
	Memories                                  []string
	History                                   []HistoryStep
	Tools                                     map[string]platform.Tool
}

func (p Prompt) Request(model string) platform.ChatRequest {
	goal := "Goal: " + p.Goal
	if p.OnBehalf != "" {
		goal += "\nOn behalf of: " + p.OnBehalf
	}
	if p.HasContext {
		goal += "\nAbout " + p.Ref + ":\n" + Observation(p.Context, 6000)
	}
	system := preamble + "\n\n" + p.Instructions
	if p.Meaning != "" {
		system += "\n\nWhat the records of your app mean:\n" + p.Meaning
	}
	if p.Glossary != "" {
		system += "\n\nThis organisation's own words (its glossary; the records above keep their meaning):\n" + p.Glossary
	}
	if p.Language != "" {
		system += "\n\nWrite your rationale, questions, drafts' free text and result in " + p.Language + ", the language of the person you work for; keep IDs, codes and field names as they are."
	}
	if len(p.Memories) > 0 {
		system += "\n\nWhat you remember from earlier runs:"
		for _, fact := range p.Memories {
			system += "\n- " + fact
		}
	}
	req := platform.ChatRequest{Model: model, PromptCache: true, Messages: []platform.Message{{Role: "system", Content: system}, {Role: "user", Content: goal}}}
	for i, step := range p.History {
		if len(step.Calls) > 0 {
			req.Messages = append(req.Messages, platform.Message{Role: "assistant", Content: step.Content, ToolCalls: step.Calls})
		}
		if step.Tool == "" {
			continue
		}
		if step.CallID != "" {
			req.Messages = append(req.Messages, platform.Message{Role: "tool", ToolCallID: step.CallID, Content: step.Outcome})
			continue
		}
		id := fmt.Sprintf("s%d", i+1)
		req.Messages = append(req.Messages, platform.Message{Role: "assistant", Content: step.Rationale, ToolCalls: []platform.ToolCall{{ID: id, Name: step.Tool, Arguments: json.RawMessage(cmp.Or(step.Arguments, "{}"))}}},
			platform.Message{Role: "tool", ToolCallID: id, Content: step.Outcome})
	}
	for _, name := range slices.Sorted(func(yield func(string) bool) {
		for name := range p.Tools {
			if !yield(name) {
				return
			}
		}
	}) {
		req.Tools = append(req.Tools, p.Tools[name])
	}
	return req
}

func SpentOut(b platform.Budget, steps, tokens int, cost float64) bool {
	return steps >= b.Steps || b.Tokens > 0 && tokens >= b.Tokens || b.Cost > 0 && cost >= b.Cost
}

func BudgetReason(b platform.Budget, steps, tokens int, cost float64) string {
	reason := fmt.Sprintf("%d steps, %d tokens", steps, tokens)
	if b.Cost > 0 {
		reason += fmt.Sprintf(", %.4f USD", cost)
	}
	return "over its budget (" + reason + ")"
}

func LanguageName(language string) string { return languageNames[language] }

var languageNames = map[string]string{"zh-CN": "Simplified Chinese (简体中文)", "zh-TW": "Traditional Chinese (繁體中文)", "ja": "Japanese", "de": "German", "fr": "French", "es": "Spanish"}
