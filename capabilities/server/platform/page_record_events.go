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
		return nil
	}
	if err := d.checkDeclaredWidgetPorts(s, pageWidget(s.Widget)); err != nil {
		return err
	}
	if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.RecordEvents.RequiredUIProfile) || s.RecordEvents == nil {
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
	if err := s.checkWidgetFieldRoles(info); err != nil {
		return err
	}
	e := s.RecordEvents
	severity, _ := info.Field(e.SeverityField)
	for _, tone := range e.Tones {
		if !slices.Contains(severity.Choices, tone.Value) {
			return fmt.Errorf("event tone does not name an original severity value")
		}
	}
	return nil
}
