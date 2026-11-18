package values

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

var inputInstant = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T([01]\d|2[0-3]):[0-5]\d:[0-5]\d(\.\d{1,9})?(Z|[+-]([01]\d|2[0-3]):[0-5]\d)$`)

func AbsoluteDate(typ, text string) (time.Time, error) {
	layout := time.DateOnly
	if typ == "datetime" {
		layout = time.RFC3339Nano
		if !HasInstantFormat(text) || strings.HasSuffix(text, "-00:00") {
			return time.Time{}, fmt.Errorf("invalid datetime bound")
		}
	}
	value, err := time.Parse(layout, text)
	if err != nil || value.Year() < 1 || value.Year() > 9999 {
		return time.Time{}, fmt.Errorf("invalid %s bound", typ)
	}
	return value, nil
}

func HasInstantFormat(text string) bool { return inputInstant.MatchString(text) }
