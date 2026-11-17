package ontology

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"

	"platformserver/platform"
	"platformserver/services/resources/publication"
)

func BusinessVersionInstallOrder(objects []Object) ([]Object, error) {
	byType := map[string]Object{}
	for _, record := range objects {
		if object, ok := publication.Read[Object](record.Published); ok && !record.Archived {
			byType["build."+object.Name] = object
		}
	}
	status := map[string]int{}
	ordered := []Object{}
	var visit func(string) error
	visit = func(typ string) error {
		if status[typ] == 2 {
			return nil
		}
		if status[typ] == 1 {
			return fmt.Errorf("business version pin definitions form a cycle")
		}
		object, known := byType[typ]
		if !known {
			return nil
		}
		status[typ] = 1
		for _, pin := range object.VersionPins {
			for _, field := range object.Fields {
				if field.Name == pin.Reference {
					if err := visit(field.Ref); err != nil {
						return err
					}
				}
			}
		}
		status[typ] = 2
		ordered = append(ordered, object)
		return nil
	}
	for _, record := range objects {
		if err := visit("build." + record.Name); err != nil {
			return nil, err
		}
	}
	return ordered, nil
}

func DefinitionBody(value Object, typ string) (json.RawMessage, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	for _, key := range []string{"id", "revision", "created", "changed", "archived", "state", "installed", "published"} {
		delete(body, key)
	}
	body["type"], err = json.Marshal(typ)
	if err != nil {
		return nil, err
	}
	return json.Marshal(body)
}

func ObjectSchemaBody(object Object) (json.RawMessage, error) {
	object.SchemaVersions = nil
	return DefinitionBody(object, AppID+"."+object.Name)
}

func CheckObjectSchemas(object Object) error {
	if len(object.SchemaVersions) == 0 {
		return nil
	}
	if len(object.SchemaVersions) > 64 {
		return fmt.Errorf("object schema version budget exceeded")
	}
	family := uint32(1)
	for index, binding := range object.SchemaVersions {
		if binding.Check() != nil || binding.Family < family || binding.Family > family+1 || index == 0 && binding.Family != 1 {
			return fmt.Errorf("object schema history is inconsistent")
		}
		family = binding.Family
	}
	body, err := ObjectSchemaBody(object)
	if err != nil {
		return err
	}
	version, err := platform.SchemaContentVersion(body)
	if err != nil || version != object.SchemaVersions[len(object.SchemaVersions)-1].Version {
		return fmt.Errorf("object schema version differs from its published definition")
	}
	return nil
}

func CompatibleObjectSchema(previous, next Object) bool {
	normalize := func(object Object) Object {
		object.Record = platform.Record{}
		object.Title, object.Plural, object.Description = "", "", ""
		object.Icon, object.Color, object.Display = "", "", ""
		object.ObjectView = nil
		object.State, object.Installed, object.Published = "", "", ""
		object.ExtensionSource, object.SchemaVersions = nil, nil
		object.ActionTypes, object.Actions = nil, nil
		object.Fields = nil
		object.States = slices.Clone(object.States)
		for index := range object.States {
			object.States[index].Title, object.States[index].Description, object.States[index].Tone = "", "", ""
		}
		return object
	}
	if !reflect.DeepEqual(normalize(previous), normalize(next)) {
		return false
	}
	fieldContract := func(field Field) Field {
		field.Title, field.SourceType = "", ""
		field.AIInput = nil
		field.Search = false
		field.Format = nil
		return field
	}
	for _, old := range previous.Fields {
		index := slices.IndexFunc(next.Fields, func(field Field) bool { return field.Name == old.Name })
		if index < 0 || !reflect.DeepEqual(fieldContract(old), fieldContract(next.Fields[index])) {
			return false
		}
	}
	for _, field := range next.Fields {
		if slices.ContainsFunc(previous.Fields, func(old Field) bool { return old.Name == field.Name }) {
			continue
		}
		if field.Required || field.Ref != "" || field.Property != nil || field.ValueType != nil || field.Formula != "" || field.FormulaExpr != nil || field.ReadOnly || field.AliasOf != "" || field.Choices != "" || !slices.Contains([]string{"text", "longtext", "integer", "decimal", "boolean", "date", "datetime"}, field.Type) {
			return false
		}
	}
	return true
}

func CompatibleObjectExpansion(previous, next Object) bool {
	if previous.Name != next.Name || len(next.Fields) <= len(previous.Fields) || len(previous.States) != len(next.States) || len(previous.Access) != len(next.Access) {
		return false
	}
	if !reflect.DeepEqual(previous.Constraints, next.Constraints) {
		return false
	}
	for i, state := range previous.States {
		if !reflect.DeepEqual(state, next.States[i]) {
			return false
		}
	}
	for i, access := range previous.Access {
		if !reflect.DeepEqual(access, next.Access[i]) {
			return false
		}
	}
	equal := func(a, b any) bool {
		x, xe := json.Marshal(a)
		y, ye := json.Marshal(b)
		return xe == nil && ye == nil && string(x) == string(y)
	}
	for _, field := range previous.Fields {
		index := slices.IndexFunc(next.Fields, func(other Field) bool { return other.Name == field.Name })
		if index < 0 || !equal(field, next.Fields[index]) {
			return false
		}
	}
	for _, field := range next.Fields {
		if slices.ContainsFunc(previous.Fields, func(old Field) bool { return old.Name == field.Name }) {
			continue
		}
		if field.Required || field.Ref != "" || field.Property != nil || field.ValueType != nil || field.Choices != "" || !slices.Contains([]string{"text", "longtext", "integer", "decimal", "boolean", "date", "datetime"}, field.Type) {
			return false
		}
	}
	return true
}
