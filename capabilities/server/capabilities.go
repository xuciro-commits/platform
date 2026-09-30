package platformserver

import (
	"reflect"
	"slices"
	"strconv"
	"strings"

	"platformserver/platform"
)

// Capabilities derives the Block palette from the same permission-filtered
// owner definitions used by pages. It owns no storage or business execution.
func (t *Tenant) Capabilities(m platform.Member) []platform.CapabilityDescriptor {
	defs := t.Definitions(m)
	entities := map[string]platform.EntityInfo{}
	for _, d := range defs {
		if d.Entity != nil {
			entities[d.Entity.Type] = *d.Entity
		}
	}
	out := []platform.CapabilityDescriptor{}
	if m.Roles["build"] == "builder" {
		out = append(out, platform.ControlBlocks()...)
	}
	for _, d := range defs {
		c := platform.CapabilityDescriptor{Ref: d.Ref, Source: d.Source, Version: d.Version, Effects: []string{},
			Ports: []platform.BlockPort{{ID: "in", Title: "In", Direction: "input", Channel: "control", Type: "flow"}, {ID: "next", Title: "Next", Direction: "output", Channel: "control", Type: "flow"}, {ID: "error", Title: "Error", Direction: "output", Channel: "control", Type: "flow"}}}
		switch {
		case d.Action != nil:
			a := d.Action
			c.Kind, c.Title, c.Description, c.Group, c.Icon, c.Tone, c.Target = "action", a.Title, a.Description, "Actions", "zap", "success", a.Target
			input := fieldsSchema(a.Payload)
			c.Parameters = slices.Clone(a.Payload)
			if !slices.ContainsFunc(a.Payload, func(f platform.Field) bool { return f.Type == "json" }) && input.Check() == nil {
				c.Input = &input
			}
			c.Effects = []string{"decision"}
			if a.NeedsApproval {
				c.Effects = append(c.Effects, "approval")
			}
		case d.Query != nil:
			q := d.Query
			c.Kind, c.Title, c.Description, c.Group, c.Icon, c.Tone, c.Target = "query", q.Title, q.Description, "Data", "database", "info", q.Object
			input := platform.ValueSchema{Type: "object", Properties: map[string]platform.ValueSchema{}}
			if q.By != "" {
				input.Properties["for"] = platform.ValueSchema{Type: "string", Description: "The record this query is run for"}
				input.Required = []string{"for"}
			}
			c.Input = &input
			if info, ok := entities[q.Object]; ok {
				record, known := entityValueSchema(info)
				rows := platform.ValueSchema{Type: "array", Items: &record, MaxItems: 200}
				text := platform.ValueSchema{Type: "string"}
				result := platform.ValueSchema{Type: "object", Properties: map[string]platform.ValueSchema{"records": rows, "total": {Type: "integer"}, "sources": {Type: "array", Items: &text}}, Required: []string{"records", "total", "sources"}}
				if known && result.Check() == nil {
					c.Output = &result
				}
			}
		case d.Function != nil:
			f := d.Function
			c.Kind, c.Title, c.Description, c.Group, c.Icon, c.Tone, c.Target = "ai", f.Title, f.Description, "AI", "sparkles", "info", f.Object
			input := platform.ValueSchema{Type: "object", Properties: map[string]platform.ValueSchema{"source": {Type: "string", Description: "A readable source record"}}, Required: []string{"source"}}
			output := fieldsSchema(f.Output)
			c.Input, c.Output = &input, &output
			c.Effects = []string{"model"}
			_, text, found := strings.Cut(d.Version, ".function-")
			if found {
				c.Revision, _ = strconv.Atoi(text)
			}
		case d.Operation != nil:
			o := d.Operation
			c.Kind, c.Title, c.Description, c.Group, c.Icon, c.Tone = "compute", o.Title, o.Description, "Code", "braces", "info"
			c.Input, c.Output = &o.Input, &o.Output
			_, text, found := strings.Cut(d.Version, ".compute-")
			if found {
				c.Revision, _ = strconv.Atoi(text)
			}
		default:
			continue
		}
		out = append(out, c)
	}
	slices.SortFunc(out, func(a, b platform.CapabilityDescriptor) int {
		if n := strings.Compare(a.Group, b.Group); n != 0 {
			return n
		}
		return strings.Compare(a.Ref.String(), b.Ref.String())
	})
	return out
}

func fieldsSchema(fields []platform.Field) platform.ValueSchema {
	s := platform.ValueSchema{Type: "object", Properties: map[string]platform.ValueSchema{}}
	for _, f := range fields {
		s.Properties[f.Name] = fieldValueSchema(f.Type, f.Description, f.Choices)
		if f.Required {
			s.Required = append(s.Required, f.Name)
		}
	}
	return s
}
func fieldValueSchema(kind, description string, choices []string) platform.ValueSchema {
	s := platform.ValueSchema{Type: "string", Description: description, Enum: choices}
	switch kind {
	case "integer":
		s.Type = "integer"
	case "number", "decimal":
		s.Type = "number"
	case "boolean":
		s.Type = "boolean"
	case "string[]", "tags", "references":
		item := platform.ValueSchema{Type: "string"}
		s.Type, s.Items, s.MaxItems = "array", &item, 1000
	case "money":
		s.Type = "object"
		s.Properties = map[string]platform.ValueSchema{"amount": {Type: "integer"}, "currency": {Type: "string"}}
		s.Required = []string{"amount", "currency"}
	}
	if s.Type != "string" {
		s.Enum = nil
	}
	return s
}
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
		return fieldValueSchema(f.Type, f.Help, choices), true
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
