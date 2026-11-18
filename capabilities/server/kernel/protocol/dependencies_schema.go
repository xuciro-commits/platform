package protocol

import (
	"platformserver/platform"
	"slices"
)

func CapabilityFieldsSchema(fields []platform.Field) platform.ValueSchema {
	schema := platform.ValueSchema{Type: "object", Properties: map[string]platform.ValueSchema{}}
	for _, field := range fields {
		value := CapabilityFieldSchema(field.Type, field.Description, field.Choices)
		if value.Type == "" {
			return platform.ValueSchema{}
		}
		schema.Properties[field.Name] = value
		if field.Required {
			schema.Required = append(schema.Required, field.Name)
		}
	}
	slices.Sort(schema.Required)
	return schema
}

func CapabilityFieldSchema(kind, description string, choices []string) platform.ValueSchema {
	value := platform.ValueSchema{Description: description}
	switch kind {
	case "integer":
		value.Type = "integer"
	case "decimal", "number":
		value.Type = "number"
	case "boolean":
		value.Type = "boolean"
	case "string[]", "tags", "references":
		item := platform.ValueSchema{Type: "string"}
		value.Type = "array"
		value.Items = &item
		value.MaxItems = 1000
	case "attachment":
		value.Type = "object"
		value.Properties = map[string]platform.ValueSchema{"mediaSet": {Type: "string"}, "item": {Type: "string"}}
		value.Required = []string{"mediaSet", "item"}
	case "money":
		value.Type = "object"
		value.Properties = map[string]platform.ValueSchema{"amount": {Type: "integer"}, "currency": {Type: "string"}}
		value.Required = []string{"amount", "currency"}
	case "json":
		return platform.ValueSchema{}
	default:
		value.Type = "string"
		value.Enum = slices.Clone(choices)
	}
	return value
}

func CapabilityEntitySchema(info platform.RecordTypeInfo) platform.ValueSchema {
	schema := platform.ValueSchema{Type: "object", Properties: map[string]platform.ValueSchema{"id": {Type: "string"}, "revision": {Type: "integer"}, "archived": {Type: "boolean"}}, Required: []string{"id", "revision"}}
	for _, field := range info.Fields {
		value := capabilityRecordFieldSchema(field)
		if value.Type == "" {
			return platform.ValueSchema{}
		}
		schema.Properties[field.Name] = value
	}
	if schema.Check() != nil {
		return platform.ValueSchema{}
	}
	return schema
}

func capabilityRecordFieldSchema(field platform.FieldInfo) platform.ValueSchema {
	if field.Type != "lines" {
		return CapabilityFieldSchema(field.Type, field.Help, field.Choices)
	}
	row := platform.ValueSchema{Type: "object", Properties: map[string]platform.ValueSchema{}}
	for _, column := range field.Fields {
		value := capabilityRecordFieldSchema(column)
		if value.Type == "" {
			return platform.ValueSchema{}
		}
		row.Properties[column.Name] = value
	}
	return platform.ValueSchema{Type: "array", Items: &row, MaxItems: 1000, Description: field.Help}
}
