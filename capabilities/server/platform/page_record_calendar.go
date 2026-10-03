package platform

import (
	"fmt"
	"regexp"
	"slices"
)

type PageRecordCalendar struct {
	DateField    string `json:"dateField"`
	LabelField   string `json:"labelField"`
	InitialMonth string `json:"initialMonth"`
}

var calendarMonth = regexp.MustCompile(`^[0-9]{4}-(0[1-9]|1[0-2])$`)

func ValidCalendarMonth(month string) bool {
	return calendarMonth.MatchString(month) && month[:4] != "0000"
}
func (d *PageDocument) checkRecordCalendar(s Section) error {
	if s.Widget != "record-calendar" {
		if s.RecordCalendar != nil {
			return fmt.Errorf("calendar configuration needs its widget")
		}
		return nil
	}
	v := d.Variables[s.CollectionVariable]
	if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.RecordCalendar.RequiredUIProfile) || s.RecordCalendar == nil || !ValidCalendarMonth(s.RecordCalendar.InitialMonth) || v.Mode != "resource" || v.Source == nil || v.Source.Kind != "plan" || !slices.Contains([]string{"page", "overlay"}, v.Scope) || len(d.Queries[v.Source.Query].Sort) == 0 || len(s.Fields) > 0 || len(s.Actions) > 0 {
		return fmt.Errorf("record calendar needs its profile, initial month and ordered original window")
	}
	return nil
}
func (s Section) CheckRecordCalendar(info EntityInfo) error {
	if s.Widget != "record-calendar" {
		return nil
	}
	if s.RecordCalendar == nil {
		return fmt.Errorf("calendar configuration is missing")
	}
	c := s.RecordCalendar
	date, ok := info.Field(c.DateField)
	label, labelOK := info.Field(c.LabelField)
	if !ok || !slices.Contains([]string{"date", "datetime"}, date.Type) || c.LabelField != "id" && (!labelOK || !slices.Contains([]string{"text", "longtext", "choice", "reference"}, label.Type)) {
		return fmt.Errorf("calendar needs original visible business date and title fields")
	}
	return nil
}
