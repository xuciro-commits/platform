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
		return nil
	}
	if err := d.checkDeclaredWidgetPorts(s, pageWidget(s.Widget)); err != nil {
		return err
	}
	limits := pageWidgets.Runtime.AlertBanner
	c := s.AlertBanner
	if !PageUIProfileSupports(d.UIProfile, limits.RequiredUIProfile) || c == nil {
		return fmt.Errorf("alert needs its profile, original scoped decimal value and configuration")
	}
	threshold, err := ParseDecimal(c.Threshold)
	if err != nil || threshold.Value != c.Threshold || !slices.Contains(limits.Tones, c.Tone) || c.Message == "" || len(c.Message) > limits.MaxMessageBytes {
		return fmt.Errorf("alert needs a canonical threshold, declared tone and bounded plain message")
	}
	return nil
}
