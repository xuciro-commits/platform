package platform

import (
	"fmt"
	"slices"
)

// CheckTimeline keeps presentation mappings in the original object schema.
// Window ownership and record authorization remain their existing contracts.
func (s Section) CheckTimeline(info EntityInfo) error {
	if s.Widget != "record-timeline" {
		return nil
	}
	start, ok := info.Field(s.TimeStart)
	if s.CollectionVariable == "" || !ok || !slices.Contains([]string{"date", "datetime"}, start.Type) {
		return fmt.Errorf("record timeline needs a plan window and date or datetime start field")
	}
	if s.TimeEnd != "" {
		end, ok := info.Field(s.TimeEnd)
		if !ok || end.Type != start.Type {
			return fmt.Errorf("timeline end must have the start field's time type")
		}
	}
	label := func(name string) bool {
		if name == "id" {
			return true
		}
		f, ok := info.Field(name)
		return ok && slices.Contains([]string{"text", "longtext", "choice", "reference"}, f.Type)
	}
	if !label(s.TimeLabel) || s.TimeGroup != "" && !label(s.TimeGroup) {
		return fmt.Errorf("timeline label and resource need visible scalar text fields")
	}
	return nil
}
