package platform

import (
	"fmt"
	"slices"
)

type PageButton struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Variant string `json:"variant,omitempty"`
	Icon    string `json:"icon,omitempty"`
}

func (d *PageDocument) checkButtonGroup(s Section) error {
	if s.Widget != "button-group" {
		if len(s.Buttons) > 0 {
			return fmt.Errorf("button controls need their group widget")
		}
		return nil
	}
	limits := pageWidgets.Runtime.ButtonGroup
	if !PageUIProfileSupports(d.UIProfile, limits.RequiredUIProfile) || len(s.Buttons) == 0 || len(s.Buttons) > limits.MaxButtons {
		return fmt.Errorf("button group needs its profile and bounded controls")
	}
	seen := map[string]bool{}
	for _, b := range s.Buttons {
		if !pageNodeID.MatchString(b.ID) || seen[b.ID] || b.Title == "" || len(b.Title) > limits.MaxTitleBytes || b.Variant != "" && !slices.Contains(limits.Variants, b.Variant) || b.Icon != "" && !slices.Contains(limits.Icons, b.Icon) {
			return fmt.Errorf("button needs a stable identity, title and supported presentation")
		}
		seen[b.ID] = true
	}
	return nil
}
