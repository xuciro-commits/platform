package platform

import "fmt"

type PageSeparator struct {
	Label *string `json:"label,omitempty"`
}

func (d *PageDocument) checkSeparator(s Section) error {
	if s.Widget != "separator" {
		return nil
	}
	c := s.Separator
	limits := pageWidgets.Runtime.Separator
	if !PageUIProfileSupports(d.UIProfile, limits.RequiredUIProfile) || c == nil || c.Label != nil && len(*c.Label) > limits.MaxLabelBytes {
		return fmt.Errorf("separator needs its profile and bounded plain label without business bindings")
	}
	return nil
}
