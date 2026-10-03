package platform

import (
	"fmt"
	"slices"
	"time"
)

type PageRecordGantt struct {
	StartField  string          `json:"startField"`
	EndField    string          `json:"endField"`
	TitleField  string          `json:"titleField"`
	StatusField string          `json:"statusField"`
	RangeStart  string          `json:"rangeStart"`
	RangeEnd    string          `json:"rangeEnd"`
	Tones       []PageEventTone `json:"tones"`
}

// Fixed civil-day bounds describe the viewport, never a query predicate.
func ValidGanttRange(start, end string) bool {
	a, errA := time.Parse("2006-01-02", start)
	b, errB := time.Parse("2006-01-02", end)
	return errA == nil && errB == nil && a.Year() > 0 && b.Year() > 0 && a.Before(b)
}
func (d *PageDocument) checkRecordGantt(s Section) error {
	if s.Widget != "record-gantt" {
		if s.RecordGantt != nil {
			return fmt.Errorf("gantt configuration needs its widget")
		}
		return nil
	}
	v := d.Variables[s.CollectionVariable]
	g := s.RecordGantt
	limits := pageWidgets.Runtime.RecordGantt
	if !PageUIProfileSupports(d.UIProfile, limits.RequiredUIProfile) || g == nil || !ValidGanttRange(g.RangeStart, g.RangeEnd) || v.Mode != "resource" || v.Source == nil || v.Source.Kind != "plan" || !slices.Contains([]string{"page", "overlay"}, v.Scope) || len(d.Queries[v.Source.Query].Sort) == 0 || len(s.Fields) > 0 || len(s.Actions) > 0 || s.Selection != "" {
		return fmt.Errorf("record gantt needs its profile, fixed range and ordered original window")
	}
	seen := map[string]bool{}
	if len(g.Tones) > limits.MaxTones {
		return fmt.Errorf("gantt tones exceed their bound")
	}
	for _, tone := range g.Tones {
		if tone.Value == "" || seen[tone.Value] || !slices.Contains(limits.Tones, tone.Tone) {
			return fmt.Errorf("gantt tones need unique values and supported tones")
		}
		seen[tone.Value] = true
	}
	return nil
}
func (s Section) CheckRecordGantt(info EntityInfo) error {
	if s.Widget != "record-gantt" {
		return nil
	}
	g := s.RecordGantt
	if g == nil {
		return fmt.Errorf("gantt configuration is missing")
	}
	for _, name := range []string{g.StartField, g.EndField} {
		f, ok := info.Field(name)
		if !ok || !slices.Contains([]string{"date", "datetime"}, f.Type) {
			return fmt.Errorf("gantt needs original visible business start and end fields")
		}
	}
	title, titleOK := info.Field(g.TitleField)
	status, statusOK := info.Field(g.StatusField)
	if g.TitleField != "id" && (!titleOK || !slices.Contains([]string{"text", "longtext", "choice", "reference"}, title.Type)) || !statusOK || status.Type != "choice" {
		return fmt.Errorf("gantt needs original visible title and choice status")
	}
	for _, tone := range g.Tones {
		if !slices.Contains(status.Choices, tone.Value) {
			return fmt.Errorf("gantt tone does not name an original status value")
		}
	}
	return nil
}
