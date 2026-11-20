package ontology

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"platformserver/platform"
	"platformserver/services/resources/publication"
)

type ObjectChange struct {
	Path      string          `json:"path"`
	Change    string          `json:"change" enum:"added,removed,changed"`
	From      json.RawMessage `json:"from,omitempty"`
	To        json.RawMessage `json:"to,omitempty"`
	FromField *Field          `json:"-"`
	ToField   *Field          `json:"-"`
}

type editableValue struct {
	raw   json.RawMessage
	field *Field
}

func DraftChanges(record any) ([]ObjectChange, error) {
	var draft Object
	switch value := record.(type) {
	case Object:
		draft = value
	default:
		return nil, nil
	}
	if draft.Published == "" {
		return nil, nil
	}
	published, ok := publication.Read[Object](draft.Published)
	if !ok {
		return nil, fmt.Errorf("published object is unavailable")
	}
	before, err := editableObject(published)
	if err != nil {
		return nil, err
	}
	after, err := editableObject(draft)
	if err != nil {
		return nil, err
	}
	if len(published.Fields) == len(draft.Fields) {
		names := func(fields []Field) []string {
			out := make([]string, len(fields))
			for i, field := range fields {
				out[i] = field.Name
			}
			return out
		}
		oldOrder, newOrder := names(published.Fields), names(draft.Fields)
		if !slices.Equal(oldOrder, newOrder) && slices.Equal(slices.Sorted(slices.Values(oldOrder)), slices.Sorted(slices.Values(newOrder))) {
			before["fields.$order"], after["fields.$order"] = editableValue{raw: platform.Raw(oldOrder)}, editableValue{raw: platform.Raw(newOrder)}
		}
	}
	keys := maps.Clone(before)
	maps.Copy(keys, after)
	var changes []ObjectChange
	for _, key := range slices.Sorted(maps.Keys(keys)) {
		from, to := before[key], after[key]
		if bytes.Equal(from.raw, to.raw) {
			continue
		}
		item := ObjectChange{Path: key, Change: "changed", From: from.raw, To: to.raw, FromField: from.field, ToField: to.field}
		if from.raw == nil {
			item.Change = "added"
		} else if to.raw == nil {
			item.Change = "removed"
		}
		changes = append(changes, item)
	}
	return changes, nil
}

func editableObject(value Object) (map[string]editableValue, error) {
	value.ExtensionSource, value.SchemaVersions = nil, nil
	if len(value.ActionTypes) != 0 {
		value.ActionTypes = slices.Clone(value.ActionTypes)
		for i := range value.ActionTypes {
			value.ActionTypes[i].SourceVersion = ""
		}
		value.Actions = nil
	}
	fields := value.Fields
	value.Fields = nil
	raw, err := DefinitionBody(value, value.Type())
	if err != nil {
		return nil, err
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	delete(body, "fields")
	delete(body, "type")
	out := make(map[string]editableValue, len(body)+len(fields))
	for key, raw := range body {
		out[key] = editableValue{raw: raw}
	}
	for _, field := range fields {
		raw, err := json.Marshal(field)
		if err != nil {
			return nil, err
		}
		out["fields."+field.Name] = editableValue{raw: raw, field: &field}
	}
	return out, nil
}
