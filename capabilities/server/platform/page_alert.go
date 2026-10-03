package platform

import (
	"fmt"
	"slices"
)

type PageAlertBanner struct {
	Threshold string `json:"threshold"`
	Tone      string `json:"tone"`
	Message   string `json:"message"`
}

func (d *PageDocument) checkAlertBanner(s Section) error {
	if s.Widget != "alert-banner" {
		if s.AlertBanner != nil || s.AlertValueVariable != "" {
			return fmt.Errorf("alert bindings require their widget")
		}
		return nil
	}
	limits := pageWidgets.Runtime.AlertBanner
	c := s.AlertBanner
	v := d.Variables[s.AlertValueVariable]
	if !PageUIProfileSupports(d.UIProfile, limits.RequiredUIProfile) || c == nil || s.AlertValueVariable == "" || v.Type != "decimal" || v.Scope != "page" && v.Scope != "overlay" || len(s.Fields) > 0 || len(s.Actions) > 0 || s.CollectionVariable != "" || s.Selection != "" {
		return fmt.Errorf("alert needs its profile, original scoped decimal value and configuration")
	}
	threshold, err := ParseDecimal(c.Threshold)
	if err != nil || threshold.Value != c.Threshold || !slices.Contains(limits.Tones, c.Tone) || c.Message == "" || len(c.Message) > limits.MaxMessageBytes {
		return fmt.Errorf("alert needs a canonical threshold, declared tone and bounded plain message")
	}
	return nil
}
