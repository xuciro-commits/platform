package platform

import (
	"fmt"
	"slices"
	"strings"
)

type PageMetricAnnotation struct {
	Direction string `json:"direction"`
	Text      string `json:"text"`
}

type PageMetricPresentation struct {
	Annotation *PageMetricAnnotation `json:"annotation,omitempty"`
	Prefix     string                `json:"prefix,omitempty"`
	Suffix     string                `json:"suffix,omitempty"`
	Formatter  string                `json:"formatter"`
	Variant    string                `json:"variant"`
	Tone       string                `json:"tone"`
}

func (d *PageDocument) checkMetricPresentation(s Section) error {
	if s.MetricPresentation == nil {
		return nil
	}
	v := s.MetricPresentation
	if v.Annotation != nil && (!PageUIProfileSupports(d.UIProfile, "platform.page.v2.90") || !slices.Contains([]string{"up", "down", "flat"}, v.Annotation.Direction) || len(v.Annotation.Text) > 256) {
		return fmt.Errorf("metric annotation needs a bounded static note and renderer profile")
	}
	c := pageWidgets.Runtime.MetricPresentation
	if s.Widget != "metric" || !PageUIProfileSupports(d.UIProfile, c.RequiredUIProfile) || len(v.Prefix) > c.MaxUnitBytes || len(v.Suffix) > c.MaxUnitBytes || !slices.Contains(c.Formatters, v.Formatter) || !slices.Contains(c.Variants, v.Variant) || !slices.Contains(c.Tones, v.Tone) {
		return fmt.Errorf("metric presentation needs its widget, profile and bounded supported display")
	}
	return nil
}

func (s Section) CheckMetricPresentation(info EntityInfo) error {
	if s.MetricPresentation == nil {
		return nil
	}
	if s.Measure == "count" {
		return nil
	}
	op, name, ok := strings.Cut(s.Measure, ":")
	field, known := info.Field(name)
	if !ok || !slices.Contains([]string{"sum", "avg", "min", "max"}, op) || !known || !slices.Contains([]string{"integer", "decimal", "money"}, field.Type) || field.Type == "money" && s.MetricPresentation.Formatter == "short" {
		return fmt.Errorf("metric presentation needs an original numeric measure; money keeps its original currency display")
	}
	return nil
}
