package platform

import (
	"fmt"
	"slices"
)

type PageRecordCard struct {
	LabelField string `json:"labelField"`
	Tone       string `json:"tone"`
}

func (d *PageDocument) checkRecordCard(s Section) error {
	if s.Widget != "record-card" {
		if s.RecordCard != nil {
			return fmt.Errorf("record card configuration needs its widget")
		}
		return nil
	}
	c := s.RecordCard
	v := d.Variables[s.RecordVariable]
	limits := pageWidgets.Runtime.RecordCard
	if !PageUIProfileSupports(d.UIProfile, limits.RequiredUIProfile) || c == nil || c.LabelField == "" || !slices.Contains(limits.Tones, c.Tone) || v.Type != "record" || !(v.Mode == "resource" && v.Source != nil && v.Source.Kind == "record" && slices.Contains([]string{"page", "overlay"}, v.Scope) || d.sharedContextRecord(s.RecordVariable)) || len(s.Fields) > limits.MaxFields || len(s.Actions) > 0 || s.Selection != "" || s.SelectionVariable != "" || s.CollectionVariable != "" {
		return fmt.Errorf("record card needs its original record resource, title, tone and bounded properties")
	}
	return nil
}
func (s Section) CheckRecordCard(info EntityInfo) error {
	if s.Widget != "record-card" {
		return nil
	}
	if s.RecordCard == nil {
		return fmt.Errorf("record card configuration is missing")
	}
	if !visibleContextField(info, s.RecordCard.LabelField, true) {
		return fmt.Errorf("record card title needs a visible original text field or ID")
	}
	if !recordScalarFields(info, s.Fields) {
		return fmt.Errorf("record card properties need unique visible scalar fields")
	}
	return nil
}
