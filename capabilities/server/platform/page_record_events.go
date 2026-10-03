package platform

import (
	"fmt"
	"slices"
)

type PageEventTone struct {
	Value string `json:"value"`
	Tone  string `json:"tone"`
}
type PageRecordEvents struct {
	TimeField     string          `json:"timeField"`
	TitleField    string          `json:"titleField"`
	SeverityField string          `json:"severityField"`
	Tones         []PageEventTone `json:"tones"`
}

func (d *PageDocument) checkRecordEvents(s Section) error {
	if s.Widget != "record-events" {
		if s.RecordEvents != nil {
			return fmt.Errorf("event configuration needs its widget")
		}
		return nil
	}
	v := d.Variables[s.CollectionVariable]
	if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.RecordEvents.RequiredUIProfile) || s.RecordEvents == nil || v.Mode != "resource" || v.Source == nil || v.Source.Kind != "plan" || !slices.Contains([]string{"page", "overlay"}, v.Scope) || len(d.Queries[v.Source.Query].Sort) == 0 || len(s.Fields) > 0 || len(s.Actions) > 0 || s.Selection != "" {
		return fmt.Errorf("record events need their profile and ordered original plan window")
	}
	seen := map[string]bool{}
	if len(s.RecordEvents.Tones) > pageWidgets.Runtime.RecordEvents.MaxTones {
		return fmt.Errorf("event tones exceed their bound")
	}
	for _, tone := range s.RecordEvents.Tones {
		if tone.Value == "" || seen[tone.Value] || !slices.Contains(pageWidgets.Runtime.RecordEvents.Tones, tone.Tone) {
			return fmt.Errorf("event tones need unique values and supported tones")
		}
		seen[tone.Value] = true
	}
	return nil
}
func (s Section) CheckRecordEvents(info EntityInfo) error {
	if s.Widget != "record-events" {
		return nil
	}
	if s.RecordEvents == nil {
		return fmt.Errorf("event configuration is missing")
	}
	e := s.RecordEvents
	time, ok := info.Field(e.TimeField)
	if !ok || !slices.Contains([]string{"date", "datetime"}, time.Type) {
		return fmt.Errorf("events need an original visible business time field")
	}
	title, titleOK := info.Field(e.TitleField)
	severity, severityOK := info.Field(e.SeverityField)
	if e.TitleField != "id" && (!titleOK || !slices.Contains([]string{"text", "longtext", "choice", "reference"}, title.Type)) || !severityOK || severity.Type != "choice" {
		return fmt.Errorf("events need visible titles and choice severity")
	}
	for _, tone := range e.Tones {
		if !slices.Contains(severity.Choices, tone.Value) {
			return fmt.Errorf("event tone does not name an original severity value")
		}
	}
	return nil
}
