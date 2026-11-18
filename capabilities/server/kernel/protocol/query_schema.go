package protocol

import (
	"platformserver/platform"
	"reflect"
	"slices"
)

func entityValueSchema(info platform.EntityInfo) (platform.ValueSchema, bool) {
	stamp := platform.ValueSchema{Type: "object", Properties: map[string]platform.ValueSchema{"by": {Type: "string"}, "at": {Type: "string"}, "change": {Type: "string"}}}
	s := platform.ValueSchema{Type: "object", Properties: map[string]platform.ValueSchema{
		"id": {Type: "string"}, "revision": {Type: "integer"}, "created": stamp, "changed": stamp, "archived": {Type: "boolean"},
	}, Required: []string{"id", "revision", "created", "changed"}}
	for _, f := range info.Fields {
		field, known := recordFieldSchema(f)
		if !known {
			return s, false
		}
		if info.Go != nil && info.Go.Kind() == reflect.Struct && len(f.Index) > 0 {
			kind := info.Go.FieldByIndex(f.Index).Type.Kind()
			field.Nullable = kind == reflect.Pointer || kind == reflect.Slice
		}
		s.Properties[f.Name] = field
	}
	return s, true
}

func recordFieldSchema(f platform.FieldInfo) (platform.ValueSchema, bool) {
	if f.Type == "json" {
		return platform.ValueSchema{}, false
	}
	if f.Type != "lines" {
		choices := slices.Clone(f.Choices)
		if len(choices) > 0 && !f.Required && !slices.Contains(choices, "") {
			choices = append(choices, "")
		}
		return platform.CapabilityFieldSchema(f.Type, f.Help, choices), true
	}
	row := platform.ValueSchema{Type: "object", Properties: map[string]platform.ValueSchema{}}
	for _, column := range f.Fields {
		child, known := recordFieldSchema(column)
		if !known {
			return row, false
		}
		row.Properties[column.Name] = child
	}
	return platform.ValueSchema{Type: "array", Nullable: true, Items: &row, MaxItems: 1000, Description: f.Help}, true
}

func QueryInputSchema(q platform.NamedQuery) platform.ValueSchema {
	input := platform.ValueSchema{Type: "object", Properties: map[string]platform.ValueSchema{}}
	if q.By != "" {
		input.Properties["for"] = platform.ValueSchema{Type: "string", Description: "The record this query is run for"}
		input.Required = []string{"for"}
	}
	return input
}
func QueryCapabilitySchemas(q platform.NamedQuery, info platform.EntityInfo) (*platform.ValueSchema, *platform.ValueSchema) {
	input := QueryInputSchema(q)
	if q.InterfaceShape != nil {
		common := platform.EntityInfo{}
		for _, f := range q.InterfaceShape.Fields {
			common.Fields = append(common.Fields, platform.FieldInfo{Name: f.Name, Type: f.Type})
		}
		record, known := entityValueSchema(common)
		for _, f := range q.InterfaceShape.Fields {
			field := record.Properties[f.Name]
			field.Nullable = true
			record.Properties[f.Name] = field
		}
		row := platform.ValueSchema{Type: "object", Properties: map[string]platform.ValueSchema{"type": {Type: "string", Enum: q.Implementations}, "id": {Type: "string"}, "record": record}, Required: []string{"type", "id", "record"}}
		text := platform.ValueSchema{Type: "string"}
		result := platform.ValueSchema{Type: "object", Properties: map[string]platform.ValueSchema{"records": {Type: "array", Items: &row, MaxItems: 200}, "total": {Type: "integer"}, "sources": {Type: "array", Items: &text}}, Required: []string{"records", "total", "sources"}}
		if !known || result.Check() != nil {
			return &input, nil
		}
		return &input, &result
	}
	if info.Type == "" {
		return &input, nil
	}
	record, known := entityValueSchema(info)
	rows := platform.ValueSchema{Type: "array", Items: &record, MaxItems: 200}
	text := platform.ValueSchema{Type: "string"}
	result := platform.ValueSchema{Type: "object", Properties: map[string]platform.ValueSchema{"records": rows, "total": {Type: "integer"}, "sources": {Type: "array", Items: &text}}, Required: []string{"records", "total", "sources"}}
	if !known || result.Check() != nil {
		return &input, nil
	}
	return &input, &result
}
