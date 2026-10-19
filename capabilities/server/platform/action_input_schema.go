package platform

import (
	"encoding/json"
	"slices"
)

// ActionInputSchema describes the original action input for model and MCP
// callers. Standard create/edit inputs use the same writable entity fields as
// generated forms, including nested rows and fields with dedicated editors.
// A nil roles slice describes a compiled agent's declared tool; member-facing
// callers pass their original roles. This is a projection, not a validator.
func ActionInputSchema(action Action, entity *EntityInfo, roles []string) map[string]any {
	properties := map[string]any{}
	required := []string{}
	for _, field := range action.Payload {
		var columns []FieldInfo
		if entity != nil && entity.Type == action.Target && field.Type == "lines" {
			if original, ok := entity.Field(field.Name); ok && original.Type == "lines" {
				columns = original.Fields
			}
		}
		properties[field.Name] = inputFieldSchema(field, columns, roles)
		if field.Required {
			required = append(required, field.Name)
		}
	}
	if entity != nil && action.Target == entity.Type && slices.Contains(entity.Standard, action.Schema) &&
		(action.Schema == entity.Type+".create" || action.Schema == entity.Type+".edit") {
		properties, required = map[string]any{}, []string{}
		create := action.Schema == entity.Type+".create"
		for _, field := range entity.Fields {
			if field.ReadOnly || roles != nil && (!field.ReadsAny(roles) || !field.WritesAny(roles)) {
				continue
			}
			input := Field{Name: field.Name, Type: field.Type, Description: field.Help, Required: create && field.Required && field.When == nil,
				Choices: field.Choices, Ref: field.Ref, Stereotype: field.Stereotype, Stereotypes: field.Stereotypes, Constraints: field.Constraints}
			if i := slices.IndexFunc(action.Payload, func(p Field) bool { return p.Name == field.Name }); i >= 0 {
				// The action owns requiredness (for example fields filled by its
				// create transaction); entity metadata supplies the full shape.
				original := action.Payload[i]
				input.Required, input.Description, input.Default = original.Required, original.Description, original.Default
				input.From, input.Key, input.Label = original.From, original.Key, original.Label
			}
			properties[field.Name] = inputFieldSchema(input, field.Fields, roles)
			if field.When != nil {
				properties[field.Name].(map[string]any)["x-platform-when"] = field.When
			}
			if input.Required {
				required = append(required, field.Name)
			}
		}
	}
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}

func inputFieldSchema(field Field, columns []FieldInfo, roles []string) map[string]any {
	schema := map[string]any{}
	switch field.Type {
	case "integer":
		schema["type"] = "integer"
	case "number", "decimal":
		schema["type"] = "number"
	case "boolean":
		schema["type"] = "boolean"
	case "string[]", "tags", "references":
		schema["type"], schema["items"] = "array", map[string]any{"type": "string"}
	case "money":
		schema = map[string]any{"type": "object", "properties": map[string]any{"amount": map[string]any{"type": "integer"}, "currency": map[string]any{"type": "string"}}, "required": []string{"amount", "currency"}, "additionalProperties": false}
	case "lines":
		row, required := map[string]any{}, []string{}
		for _, column := range columns {
			if column.ReadOnly || roles != nil && (!column.ReadsAny(roles) || !column.WritesAny(roles)) {
				continue
			}
			row[column.Name] = inputFieldSchema(Field{Name: column.Name, Type: column.Type, Description: column.Help,
				Choices: column.Choices, Ref: column.Ref, Stereotype: column.Stereotype, Stereotypes: column.Stereotypes, Constraints: column.Constraints}, column.Fields, roles)
			if column.When != nil {
				row[column.Name].(map[string]any)["x-platform-when"] = column.When
			}
			if column.Required && column.When == nil {
				required = append(required, column.Name)
			}
		}
		items := map[string]any{}
		if len(columns) > 0 {
			items = map[string]any{"type": "object", "properties": row, "required": required, "additionalProperties": false}
		}
		schema["type"], schema["items"] = "array", items
	case "json": // An opaque JSON value must not be advertised as text.
	default:
		schema["type"] = "string"
	}
	if field.Description != "" {
		schema["description"] = field.Description
	}
	if json.Valid(field.Default) {
		schema["default"] = slices.Clone(field.Default)
	}
	if len(field.Choices) > 0 {
		if schema["type"] == "array" {
			schema["items"].(map[string]any)["enum"] = slices.Clone(field.Choices)
		} else {
			schema["enum"] = slices.Clone(field.Choices)
		}
	}
	if field.Type == "date" && (field.Constraints == nil || !field.Constraints.DateTime) {
		schema["format"] = "date"
	} else if field.Type == "datetime" {
		schema["format"] = "date-time"
	}
	if field.Ref != "" {
		schema["x-platform-reference"] = field.Ref
	}
	if field.Stereotype != "" {
		schema["x-platform-stereotype"] = field.Stereotype
	}
	if len(field.Stereotypes) > 0 {
		schema["x-platform-stereotypes"] = slices.Clone(field.Stereotypes)
	}
	if field.From != "" {
		schema["x-platform-source"] = map[string]string{"read": field.From, "key": field.Key, "label": field.Label}
	}
	if c := field.Constraints; c != nil {
		for _, bound := range []struct {
			name  string
			value *float64
		}{{map[bool]string{false: "minimum", true: "exclusiveMinimum"}[c.ExclusiveMin], c.Min}, {map[bool]string{false: "maximum", true: "exclusiveMaximum"}[c.ExclusiveMax], c.Max}} {
			if bound.value != nil {
				schema[bound.name] = *bound.value
			}
		}
		if c.MinLength != nil {
			schema["minLength"] = *c.MinLength
		}
		if c.MaxLength != nil {
			schema["maxLength"] = *c.MaxLength
		}
		if c.Before != "" || c.After != "" || c.DateTime {
			schema["x-platform-constraints"] = c
		}
	}
	return schema
}
