package platform

import (
	"fmt"
	"math"
)

type PageSpacer struct {
	Size *float64 `json:"size,omitempty"`
}

func (d *PageDocument) checkSpacer(s Section) error {
	if s.Widget != "spacer" {
		if s.Spacer != nil {
			return fmt.Errorf("spacer configuration needs its widget")
		}
		return nil
	}
	c := s.Spacer
	if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.Spacer.RequiredUIProfile) || c == nil || c.Size == nil || math.IsNaN(*c.Size) || math.IsInf(*c.Size, 0) || *c.Size < 0 || *c.Size > float64(pageWidgets.Layout.MaxSize) || len(s.Fields) > 0 || len(s.Actions) > 0 || s.CollectionVariable != "" || s.Selection != "" {
		return fmt.Errorf("spacer needs its profile and finite nonnegative size within the layout budget without business bindings")
	}
	return nil
}
