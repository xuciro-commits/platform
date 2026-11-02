package data

import (
	"encoding/json"
	"fmt"
	"maps"
)

func DatasetTransaction(key, kind string, prior, rows []map[string]any) ([]map[string]any, error) {
	if (kind == "update" || kind == "delete") && key == "" {
		return nil, fmt.Errorf("Configure a dataset key before updating or deleting rows")
	}
	if key == "" {
		if kind == "snapshot" {
			return rows, nil
		}
		return append(prior, rows...), nil
	}
	identify := func(row map[string]any) (string, error) {
		value, ok := row[key]
		if !ok || value == nil || value == "" {
			return "", fmt.Errorf("Every dataset row needs a nonempty key")
		}
		switch value.(type) {
		case string, float64, json.Number:
		default:
			return "", fmt.Errorf("A dataset key is text or a number")
		}
		raw, _ := json.Marshal(value)
		return string(raw), nil
	}
	incoming := map[string]bool{}
	for _, row := range rows {
		id, err := identify(row)
		if err != nil {
			return nil, err
		}
		if incoming[id] {
			return nil, fmt.Errorf("Dataset keys must be unique")
		}
		incoming[id] = true
	}
	if kind == "snapshot" {
		return rows, nil
	}
	out := make([]map[string]any, 0, len(prior)+len(rows))
	positions := map[string]int{}
	for _, row := range prior {
		id, err := identify(row)
		if err != nil {
			return nil, err
		}
		if _, ok := positions[id]; ok {
			return nil, fmt.Errorf("Dataset keys must be unique")
		}
		if kind == "delete" && incoming[id] {
			continue
		}
		positions[id] = len(out)
		out = append(out, row)
	}
	if kind == "delete" {
		return out, nil
	}
	for _, row := range rows {
		id, _ := identify(row)
		if at, ok := positions[id]; ok {
			if kind == "append" {
				return nil, fmt.Errorf("Dataset keys must be unique")
			}
			merged := maps.Clone(out[at])
			maps.Copy(merged, row)
			out[at] = merged
		} else {
			positions[id] = len(out)
			out = append(out, row)
		}
	}
	return out, nil
}
