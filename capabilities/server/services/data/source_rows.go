package data

import (
	"encoding/json"
	"fmt"
	"time"

	"platformserver/platform"
)

func DeclaredRows(binding platform.DataSourceBinding, input []map[string]any, now time.Time, resolve func(string) string) ([]SourceRow, error) {
	if len(input) > ValueSourceRows {
		return nil, fmt.Errorf("the answer holds %d rows; a pull takes at most %d", len(input), ValueSourceRows)
	}
	rows := make([]SourceRow, 0, len(input))
	for _, values := range input {
		id := platform.IntegrationScalar(values[binding.Key])
		at := now.UTC()
		row := SourceRow{ID: id, Origin: id, At: &at}
		if id != "" {
			row.ID = resolve(id)
		}
		if id == "" {
			row.Error = "row has no id"
		}
		if raw := platform.IntegrationScalar(values[binding.SourceTime]); binding.SourceTime != "" && raw != "" {
			at, err := time.Parse(time.RFC3339Nano, raw)
			if err != nil {
				at, err = time.Parse(time.DateOnly, raw)
			}
			if err != nil {
				row.Error = "row has an invalid source time"
			} else {
				parsed := at.UTC()
				row.At = &parsed
			}
		}
		payload := map[string]any{}
		for _, column := range binding.Columns {
			if value, exists := values[column.Column]; exists {
				payload[column.Attribute] = value
			}
		}
		if len(payload) == 0 {
			row.Error = "row has no mapped attributes"
		}
		row.Payload, _ = json.Marshal(payload)
		rows = append(rows, row)
	}
	return rows, nil
}

func AssignVerbs(rows []SourceRow, exists func(id string) bool) {
	seen := map[string]bool{}
	for i := range rows {
		row := &rows[i]
		row.Key = fmt.Sprintf("row:%d", i)
		row.Verb = ".create"
		if seen[row.ID] || exists(row.ID) {
			row.Verb = ".edit"
		}
		if row.Error == "" {
			seen[row.ID] = true
		}
	}
}
