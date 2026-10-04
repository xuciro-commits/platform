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
	if s.RecordCard.LabelField != "id" {
		f, ok := info.Field(s.RecordCard.LabelField)
		if !ok || !slices.Contains([]string{"text", "longtext", "choice", "reference"}, f.Type) {
			return fmt.Errorf("record card title needs a visible original text field or ID")
		}
	}
	seen := map[string]bool{}
	for _, name := range s.Fields {
		f, ok := info.Field(name)
		if !ok || seen[name] || !slices.Contains([]string{"text", "longtext", "choice", "reference", "integer", "decimal", "money", "date", "datetime", "boolean"}, f.Type) {
			return fmt.Errorf("record card properties need unique visible scalar fields")
		}
		seen[name] = true
	}
	return nil
}
