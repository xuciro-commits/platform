package platform

import (
	"fmt"
	"regexp"
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
		return nil
	}
	if err := d.checkDeclaredWidgetPorts(s, pageWidget(s.Widget)); err != nil {
		return err
	}
	if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.RecordCalendar.RequiredUIProfile) || s.RecordCalendar == nil || !ValidCalendarMonth(s.RecordCalendar.InitialMonth) {
		return fmt.Errorf("record calendar needs its profile, initial month and ordered original window")
	}
	return nil
}
func (s Section) CheckRecordCalendar(info EntityInfo) error {
	if s.Widget != "record-calendar" {
		return nil
	}
	return s.checkWidgetFieldRoles(info)
}
