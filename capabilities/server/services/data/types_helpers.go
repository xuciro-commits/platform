package data

import (
	"fmt"
	"reflect"
	"slices"

	"platformserver/platform"
)

func Guarded(marking string) bool {
	return platform.MarkingRank(marking) >= platform.MarkingRank("confidential")
}

func UnguardedFields(info platform.EntityInfo, columns []string) []string {
	var open []string
	for _, field := range info.Fields {
		if len(field.Read) == 0 && (columns == nil || slices.Contains(columns, field.Name)) {
			open = append(open, field.Name)
		}
	}
	return open
}

func readInventoryOf(c platform.Caller, typ reflect.Type) ([]any, error) {
	var all []any
	for {
		rows, count, err := c.FindOf(typ, platform.Query{Limit: 500, Offset: len(all), Sort: []string{"id"}})
		if err != nil {
			return nil, err
		}
		if count > 1000 {
			return nil, fmt.Errorf("integration inventory exceeds 1000 records of one kind")
		}
		all = append(all, rows...)
		if len(all) >= count {
			return all, nil
		}
	}
}
