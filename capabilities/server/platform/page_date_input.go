package platform

import (
	"fmt"
	"time"
)

// CivilDateValid validates the original four-digit Gregorian business date.
func CivilDateValid(text string) bool {
	value, err := time.Parse(time.DateOnly, text)
	return err == nil && value.Year() >= 1 && value.Year() <= 9999 && value.Format(time.DateOnly) == text
}

func (d *PageDocument) checkDateInput(s Section) error {
	if s.Widget != "date-input" {
		if s.DateVariable != "" || s.DateLabel != nil {
			return fmt.Errorf("date bindings need their widget")
		}
		return nil
	}
	v := d.Variables[s.DateVariable]
	if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.DateInput.RequiredUIProfile) || s.DateVariable == "" || v.Type != "string" || v.Mode != "state" || v.Scope != "page" && v.Scope != "overlay" || s.DateLabel != nil && len(*s.DateLabel) > 1024 || len(s.Fields) > 0 || len(s.Actions) > 0 || s.CollectionVariable != "" || s.Selection != "" {
		return fmt.Errorf("date input needs original scoped text state and bounded label")
	}
	return nil
}
