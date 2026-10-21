package platform

import (
	"fmt"
)

type PageHistogram struct {
	Field string `json:"field"`
	Bins  int    `json:"bins"`
}

func (d *PageDocument) checkHistogram(s Section) error {
	if s.Widget != "histogram" {
		return nil
	}
	if err := d.checkDeclaredWidgetPorts(s, pageWidget(s.Widget)); err != nil {
		return err
	}
	h := s.Histogram
	if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.Histogram.RequiredUIProfile) || h == nil || h.Field == "" || h.Bins < 1 || h.Bins > pageWidgets.Runtime.Histogram.MaxBins {
		return fmt.Errorf("histogram needs scoped original plan, numeric field and bounded bins")
	}
	return nil
}
func (s Section) CheckHistogram(info EntityInfo) error {
	if s.Widget != "histogram" {
		return nil
	}
	return s.checkWidgetFieldRoles(info)
}
