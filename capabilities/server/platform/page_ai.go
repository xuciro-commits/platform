package platform

import (
	"fmt"
	"slices"
	"strings"
)

type PageAI struct {
	Kind             string   `json:"kind"`
	ReplyField       string   `json:"replyField,omitempty"`
	QuestionVariable string   `json:"questionVariable,omitempty"`
	Suggestions      []string `json:"suggestions,omitempty"`
}

func (s Section) AIVariables() []string {
	if s.AI == nil {
		return nil
	}
	return []string{s.RecordVariable, s.AI.QuestionVariable}
}
func (d *PageDocument) checkAI(s Section) error {
	if s.Widget != "ai-assistant" {
		if s.AI != nil {
			return fmt.Errorf("AI configuration needs its original widget")
		}
		return nil
	}
	c := s.AI
	v := d.Variables[s.RecordVariable]
	if !PageUIProfileSupports(d.UIProfile, "platform.page.v2.86") || c == nil || !slices.Contains([]string{"analyst", "generated", "chatbot"}, c.Kind) || s.Function == nil || s.Function.Ref.Check() != nil || s.Function.Ref.Kind != AssetFunction || s.Function.SourceVersion == "" || v.Type != "record" || v.Mode != "resource" || v.Source == nil || v.Source.Kind != "record" || !slices.Contains([]string{"page", "overlay"}, v.Scope) || s.Selection != "" || s.CollectionVariable != "" || len(s.Actions) > 0 || len(s.Fields) > 0 || s.Operation != nil || len(s.Inputs) > 0 || s.Query != (AssetRef{}) || s.Relation != "" || s.ParentSelection != "" {
		return fmt.Errorf("AI view needs a fixed function and original confirmed record resource")
	}
	if c.Kind != "chatbot" {
		if c.QuestionVariable != "" || c.ReplyField != "" || len(c.Suggestions) > 0 {
			return fmt.Errorf("only chatbot declares question, reply and suggestions")
		}
		return nil
	}
	question := d.Variables[c.QuestionVariable]
	if question.Type != "string" || question.Mode != "state" || question.Scope != v.Scope || question.Owner != v.Owner || !pageNodeID.MatchString(c.ReplyField) || len(c.Suggestions) > 8 {
		return fmt.Errorf("chatbot needs owned question state and one original reply field")
	}
	for _, text := range c.Suggestions {
		if strings.TrimSpace(text) == "" || len(text) > 4096 {
			return fmt.Errorf("chat suggestions need bounded original text")
		}
	}
	return nil
}
func (p Page) CheckAIBinding(s Section, f AIFunction) error {
	if s.Widget != "ai-assistant" {
		return nil
	}
	object := p.RecordResourceObject(s.RecordVariable)
	shown := s.Object
	if shown.Name == "" {
		shown = p.Object
	}
	if object != shown || object.Name != f.Object || object.App != s.Function.Ref.App {
		return fmt.Errorf("AI function source object differs from its original producer")
	}
	if s.AI.Kind == "chatbot" {
		if !f.Conversation || !slices.ContainsFunc(f.Output, func(field Field) bool {
			return field.Name == s.AI.ReplyField && field.Type == "string" && field.Required
		}) {
			return fmt.Errorf("chatbot needs a conversation function with a required text reply")
		}
	} else if f.Conversation {
		return fmt.Errorf("analysis views need a record-only function")
	}
	return nil
}
