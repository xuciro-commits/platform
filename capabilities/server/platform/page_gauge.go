package platform

import (
	"fmt"
	"math"
)

type PageGauge struct {
	Max    float64  `json:"max"`
	WarnAt *float64 `json:"warnAt,omitempty"`
	Label  string   `json:"label,omitempty"`
	Suffix string   `json:"suffix,omitempty"`
}

func (d *PageDocument) checkGauge(s Section) error {
	if s.Widget != "gauge" {
		return nil
	}
	if err := d.checkDeclaredWidgetPorts(s, pageWidget(s.Widget)); err != nil {
		return err
	}
	g := s.Gauge
	if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.Gauge.RequiredUIProfile) || g == nil || g.Max <= 0 || math.IsNaN(g.Max) || math.IsInf(g.Max, 0) || g.WarnAt != nil && (math.IsNaN(*g.WarnAt) || math.IsInf(*g.WarnAt, 0)) || len(g.Label) > 1024 || len(g.Suffix) > 64 {
		return fmt.Errorf("gauge needs its number port, positive finite maximum and bounded presentation")
	}
	return nil
}
