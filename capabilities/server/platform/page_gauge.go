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
		if s.Gauge != nil || s.GaugeValueVariable != "" {
			return fmt.Errorf("gauge bindings need their widget")
		}
		return nil
	}
	g := s.Gauge
	if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.Gauge.RequiredUIProfile) || g == nil || s.GaugeValueVariable == "" || d.Variables[s.GaugeValueVariable].Type != "number" || g.Max <= 0 || math.IsNaN(g.Max) || math.IsInf(g.Max, 0) || g.WarnAt != nil && (math.IsNaN(*g.WarnAt) || math.IsInf(*g.WarnAt, 0)) || len(g.Label) > 1024 || len(g.Suffix) > 64 || len(s.Fields) > 0 || len(s.Actions) > 0 || s.CollectionVariable != "" || s.Selection != "" {
		return fmt.Errorf("gauge needs its number port, positive finite maximum and bounded presentation")
	}
	return nil
}
