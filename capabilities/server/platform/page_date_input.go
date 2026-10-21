package platform

import (
	"fmt"
	"regexp"
	"time"
)

// CivilDateValid validates the original four-digit Gregorian business date.
func CivilDateValid(text string) bool {
	value, err := time.Parse(time.DateOnly, text)
	return err == nil && value.Year() >= 1 && value.Year() <= 9999 && value.Format(time.DateOnly) == text
}

var timestampPattern = regexp.MustCompile(`^([0-9]{4}-[0-9]{2}-[0-9]{2})T([0-9]{2}):([0-9]{2}):([0-9]{2})(\.[0-9]{1,9})?(Z|[+-][0-9]{2}:[0-9]{2})$`)
var offsetPattern = regexp.MustCompile(`^(Z|[+-]([01][0-9]|2[0-3]):[0-5][0-9])$`)

func TimestampOffsetValid(value string) bool {
	return value != "-00:00" && offsetPattern.MatchString(value)
}
func TimestampValid(value string) bool {
	parts := timestampPattern.FindStringSubmatch(value)
	if parts == nil || !CivilDateValid(parts[1]) || !TimestampOffsetValid(parts[6]) {
		return false
	}
	_, err := time.Parse(time.RFC3339Nano, value)
	return err == nil
}

func (d *PageDocument) checkDateInput(s Section) error {
	if s.Widget != "date-input" {
		return nil
	}
	if err := d.checkDeclaredWidgetPorts(s, pageWidget(s.Widget)); err != nil {
		return err
	}
	if s.DateKind != "" && s.DateKind != "date" && s.DateKind != "datetime" || s.DateKind == "datetime" && (!PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.DateInput.DateTimeRequiredUIProfile) || !TimestampOffsetValid(s.DateOffset)) || s.DateKind != "datetime" && s.DateOffset != "" {
		return fmt.Errorf("datetime input needs its profile and explicit offset; civil date cannot carry an offset")
	}
	if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.DateInput.RequiredUIProfile) || s.DateVariable == "" || s.DateLabel != nil && len(*s.DateLabel) > 1024 {
		return fmt.Errorf("date input needs original scoped text state and bounded label")
	}
	return nil
}
