package expr

import (
	"encoding/json"
	"time"
)

func (c Context) fitsBudget() bool {
	if len(c.Windows) != 0 || c.Now.Location() != time.UTC || c.Now.Year() < 0 || c.Now.Year() > 9999 {
		return false
	}
	remaining := MaxContextBytes/6 - 128 - len(c.Me) - len(c.Today)
	fieldsFit := func(fields map[string]Value) bool {
		for key, value := range fields {
			remaining -= 48 + len(key) + len(value.Kind) + len(value.Status) + len(value.Data)
			if remaining < 0 || len(value.Data) != 0 && !json.Valid(value.Data) {
				return false
			}
		}
		return remaining >= 0
	}
	for _, source := range c.Sources {
		remaining -= 4 + len(source)
		if remaining < 0 {
			return false
		}
	}
	if !fieldsFit(c.Refs) {
		return false
	}
	for key, set := range c.Sets {
		remaining -= 64 + len(key) + len(set.Status)
		if remaining < 0 {
			return false
		}
		for _, item := range set.Items {
			remaining -= 24 + len(item.ID)
			if !fieldsFit(item.Fields) {
				return false
			}
		}
	}
	return true
}
