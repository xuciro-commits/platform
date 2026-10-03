package platform

import (
	"fmt"
	"slices"
)

type PageHistogram struct {
	Field string `json:"field"`
	Bins  int    `json:"bins"`
}

func (d *PageDocument) checkHistogram(s Section) error {
	if s.Widget != "histogram" {
		if s.Histogram != nil {
			return fmt.Errorf("histogram configuration needs its widget")
		}
		return nil
	}
	h := s.Histogram
	v := d.Variables[s.CollectionVariable]
	if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.Histogram.RequiredUIProfile) || h == nil || h.Field == "" || h.Bins < 1 || h.Bins > pageWidgets.Runtime.Histogram.MaxBins || v.Mode != "resource" || v.Source == nil || v.Source.Kind != "plan" || !slices.Contains([]string{"page", "overlay"}, v.Scope) || len(s.Fields) > 0 || len(s.Actions) > 0 || s.Selection != "" {
		return fmt.Errorf("histogram needs scoped original plan, numeric field and bounded bins")
	}
	return nil
}
func (s Section) CheckHistogram(info EntityInfo) error {
	if s.Widget != "histogram" {
		return nil
	}
	if s.Histogram == nil {
		return fmt.Errorf("histogram field is missing")
	}
	f, ok := info.Field(s.Histogram.Field)
	if !ok || !slices.Contains([]string{"integer", "decimal"}, f.Type) {
		return fmt.Errorf("histogram needs a visible original numeric field")
	}
	return nil
}
