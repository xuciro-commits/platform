package platform

import "slices"

func CapabilityFieldsSchema(fields []Field) ValueSchema {
	schema := ValueSchema{Type: "object", Properties: map[string]ValueSchema{}}
	for _, field := range fields {
		value := CapabilityFieldSchema(field.Type, field.Description, field.Choices)
		if value.Type == "" {
			return ValueSchema{}
		}
		schema.Properties[field.Name] = value
		if field.Required {
			schema.Required = append(schema.Required, field.Name)
		}
	}
	slices.Sort(schema.Required)
	return schema
}

func CapabilityFieldSchema(kind, description string, choices []string) ValueSchema {
	value := ValueSchema{Description: description}
	switch kind {
	case "integer":
		value.Type = "integer"
	case "decimal", "number":
		value.Type = "number"
	case "boolean":
		value.Type = "boolean"
	case "string[]", "tags", "references":
		item := ValueSchema{Type: "string"}
		value.Type = "array"
		value.Items = &item
		value.MaxItems = 1000
	case "attachment":
		value.Type = "object"
		value.Properties = map[string]ValueSchema{"mediaSet": {Type: "string"}, "item": {Type: "string"}}
		value.Required = []string{"mediaSet", "item"}
	case "money":
		value.Type = "object"
		value.Properties = map[string]ValueSchema{"amount": {Type: "integer"}, "currency": {Type: "string"}}
		value.Required = []string{"amount", "currency"}
	case "json":
		return ValueSchema{}
	default:
		value.Type = "string"
		value.Enum = slices.Clone(choices)
	}
	return value
}

func CapabilityEntitySchema(info EntityInfo) ValueSchema {
	schema := ValueSchema{Type: "object", Properties: map[string]ValueSchema{"id": {Type: "string"}, "revision": {Type: "integer"}, "archived": {Type: "boolean"}}, Required: []string{"id", "revision"}}
	for _, field := range info.Fields {
		value := capabilityRecordFieldSchema(field)
		if value.Type == "" {
			return ValueSchema{}
		}
		schema.Properties[field.Name] = value
	}
	if schema.Check() != nil {
		return ValueSchema{}
	}
	return schema
}

func capabilityRecordFieldSchema(field FieldInfo) ValueSchema {
	if field.Type != "lines" {
		return CapabilityFieldSchema(field.Type, field.Help, field.Choices)
	}
	row := ValueSchema{Type: "object", Properties: map[string]ValueSchema{}}
	for _, column := range field.Fields {
		value := capabilityRecordFieldSchema(column)
		if value.Type == "" {
			return ValueSchema{}
		}
		row.Properties[column.Name] = value
	}
	return ValueSchema{Type: "array", Items: &row, MaxItems: 1000, Description: field.Help}
}
