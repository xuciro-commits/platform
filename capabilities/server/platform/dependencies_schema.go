package platform

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
)

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

func pageDependencies(original Page) []AssetRef {
	page, err := original.Safety()
	if err != nil {
		return nil
	}
	refs := []AssetRef{}
	if page.Object.Name != "" {
		refs = append(refs, page.Object)
	}
	refs = append(refs, page.ModelDependencies()...)
	refs = append(refs, page.QueryReferences()...)
	refs = append(refs, page.NavigationTargets()...)
	for _, compute := range page.ComputeResources() {
		refs = append(refs, compute.Operation.Ref)
	}
	for _, selection := range page.InputSchema.Selections {
		refs = append(refs, selection.Object)
	}
	for _, binding := range page.DataBindings {
		if binding.Object.Name != "" {
			refs = append(refs, binding.Object)
		}
		if binding.Query != nil {
			refs = append(refs, binding.Query.Ref)
		}
		for _, resource := range binding.Resources {
			if resource.Asset != nil {
				refs = append(refs, resource.Asset.Ref)
			}
			if resource.Object != nil {
				refs = append(refs, *resource.Object)
			}
		}
	}
	for _, trigger := range page.ActionTriggers {
		refs = append(refs, trigger.Actions...)
		if trigger.InlineEdit != nil {
			refs = append(refs, trigger.InlineEdit.Action)
		}
		if trigger.Function != nil {
			refs = append(refs, trigger.Function.Ref)
		}
		if trigger.Operation != nil {
			refs = append(refs, trigger.Operation.Ref)
		}
		for _, event := range trigger.Events {
			for _, effect := range event.Effects {
				if effect.Action != nil {
					refs = append(refs, effect.Action.Ref)
				}
			}
		}
	}
	return refs
}

func checkReleaseBindings(ref AssetRef, body []byte, declared []AssetRef) error {
	description, err := describeAsset(ReleaseAsset{Ref: ref, Body: body}, true)
	if err != nil {
		return err
	}
	for _, dep := range description.Requires {
		if !slices.Contains(declared, dep) {
			return fmt.Errorf("release asset %s omits bound dependency %s", ref, dep)
		}
	}
	return nil
}

func CheckAssetDependencies(asset ReleaseAsset) error { return checkAssetDescription(asset) }

func checkAssetDescription(asset ReleaseAsset) error {
	if asset.Provides == nil {
		return checkReleaseBindings(asset.Ref, asset.Body, asset.Requires)
	}
	description, err := DescribeAsset(asset)
	if err != nil {
		return err
	}
	actual := slices.Clone(asset.Requires)
	slices.SortFunc(actual, compareRef)
	for _, dependency := range description.Requires {
		if !slices.Contains(actual, dependency) {
			return fmt.Errorf("release asset %s omits bound dependency %s", asset.Ref, dependency)
		}
	}
	if !slices.Equal(actual, description.Requires) {
		return fmt.Errorf("%s requires list differs from its body references", asset.Ref)
	}
	for _, capability := range asset.Provides {
		if err := capability.Check(); err != nil {
			return err
		}
	}
	actualCapabilities, _ := json.Marshal(asset.Provides)
	expectedCapabilities, _ := json.Marshal(description.Provides)
	if !bytes.Equal(actualCapabilities, expectedCapabilities) {
		return fmt.Errorf("%s provides list differs from its owner declaration", asset.Ref)
	}
	return nil
}
