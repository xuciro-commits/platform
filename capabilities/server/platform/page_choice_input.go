package platform

import (
	"fmt"
	"slices"
)

type PageChoiceInput struct {
	Variant string   `json:"variant"`
	Options []string `json:"options"`
	Label   *string  `json:"label,omitempty"`
}

func (d *PageDocument) checkChoiceInput(s Section) error {
	if s.Widget != "choice-input" {
		if s.ChoiceVariable != "" || s.ChoiceInput != nil {
			return fmt.Errorf("choice input bindings need their widget")
		}
		return nil
	}
	c, v := s.ChoiceInput, d.Variables[s.ChoiceVariable]
	limits := pageWidgets.Runtime.ChoiceInput
	if c == nil || !PageUIProfileSupports(d.UIProfile, limits.RequiredUIProfile) || !slices.Contains(limits.Variants, c.Variant) || c.Options == nil || len(c.Options) > limits.MaxOptions || c.Label != nil && len(*c.Label) > 1024 || s.ChoiceVariable == "" || v.Type != "string" || v.Mode != "state" || v.Scope != "page" && v.Scope != "overlay" || len(s.Fields) > 0 || len(s.Actions) > 0 || s.CollectionVariable != "" || s.Selection != "" {
		return fmt.Errorf("choice input needs scoped text state, bounded static options and a supported presentation")
	}
	seen := map[string]bool{}
	for _, option := range c.Options {
		if option == "" || seen[option] || len(option) > limits.MaxOptionBytes {
			return fmt.Errorf("choice options must be unique nonempty bounded strings")
		}
		seen[option] = true
	}
	return nil
}
