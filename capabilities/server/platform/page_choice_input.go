package platform

import (
	"fmt"
	"slices"
	"strconv"
)

type PageChoiceInput struct {
	Clearable    bool     `json:"clearable,omitempty"`
	Variant      string   `json:"variant"`
	Options      []string `json:"options"`
	OptionLabels []string `json:"optionLabels,omitempty"`
	Label        *string  `json:"label,omitempty"`
}

func (d *PageDocument) checkChoiceInput(s Section) error {
	if s.Widget != "choice-input" {
		if s.ChoiceVariable != "" || s.ChoiceSetVariable != "" || s.ChoiceInput != nil {
			return fmt.Errorf("choice input bindings need their widget")
		}
		return nil
	}
	c := s.ChoiceInput
	limits := pageWidgets.Runtime.ChoiceInput
	if c == nil || !PageUIProfileSupports(d.UIProfile, limits.RequiredUIProfile) || !slices.Contains(limits.Variants, c.Variant) || c.Options == nil || len(c.Options) > limits.MaxOptions || c.Label != nil && len(*c.Label) > 1024 || len(s.Fields) > 0 || len(s.Actions) > 0 || s.CollectionVariable != "" || s.Selection != "" {
		return fmt.Errorf("choice input needs scoped text state, bounded static options and a supported presentation")
	}
	indexed := c.Variant == "steps" || c.Variant == "tabs"
	if indexed {
		if !PageUIProfileSupports(d.UIProfile, limits.IndexedRequiredUIProfile) || len(c.Options) == 0 || len(c.OptionLabels) != len(c.Options) {
			return fmt.Errorf("indexed choice needs its profile and one label for each original index")
		}
	} else if c.OptionLabels != nil {
		return fmt.Errorf("choice option labels need the indexed presentation")
	}
	if c.Clearable && (c.Variant != "multiple" || !PageUIProfileSupports(d.UIProfile, limits.ClearRequiredUIProfile)) {
		return fmt.Errorf("clearing a choice set needs its multiple presentation and supported profile")
	}
	variable, typ := s.ChoiceVariable, "string"
	if c.Variant == "multiple" {
		if !PageUIProfileSupports(d.UIProfile, limits.MultipleRequiredUIProfile) || s.ChoiceVariable != "" {
			return fmt.Errorf("multiple choices need their string-set profile and port")
		}
		variable, typ = s.ChoiceSetVariable, "string-set"
	} else if s.ChoiceSetVariable != "" {
		return fmt.Errorf("single choice cannot bind a string-set port")
	}
	v := d.Variables[variable]
	if variable == "" || v.Type != typ || v.Mode != "state" && !(indexed && v.Mode == "constant") || v.Scope != "page" && v.Scope != "overlay" {
		return fmt.Errorf("choice needs its original scoped state type")
	}
	seen := map[string]bool{}
	for index, option := range c.Options {
		if option == "" || seen[option] || len(option) > limits.MaxOptionBytes {
			return fmt.Errorf("choice options must be unique nonempty bounded strings")
		}
		seen[option] = true
		if indexed && (option != strconv.Itoa(index) || len(c.OptionLabels[index]) > limits.MaxOptionBytes) {
			return fmt.Errorf("indexed choices need canonical original indices and bounded labels")
		}
	}
	return nil
}
