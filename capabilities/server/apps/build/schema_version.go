package build

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"

	buildapi "platformserver/internal/host/buildapi"
	"platformserver/platform"
)

// The object owner declares compatibility before publication. No historical
// definition is rewritten; old publications without this metadata contribute
// their original version-1 identity when their next version is published.
func (b *Build) captureObjectSchema(object *Object) error {
	portable, err := b.dispatchSchemaObject(*object)
	if err != nil {
		return err
	}
	body, err := objectSchemaBody(portable)
	if err != nil {
		return err
	}
	version, err := platform.SchemaContentVersion(body)
	if err != nil {
		return err
	}
	var versions []platform.SchemaVersion
	family := uint32(1)
	if previous, exists := wasPublished[Object](object.Published); exists {
		versions = slices.Clone(previous.SchemaVersions)
		if len(versions) == 0 {
			portable, err := b.dispatchSchemaObject(previous)
			if err != nil {
				return err
			}
			old, err := objectSchemaBody(portable)
			if err != nil {
				return err
			}
			identity, err := platform.SchemaContentVersion(old)
			if err != nil {
				return err
			}
			versions = append(versions, platform.SchemaVersion{Version: identity, Family: 1})
		}
		family = versions[len(versions)-1].Family
		if !compatibleObjectSchema(previous, *object) {
			if family >= 64 {
				return fmt.Errorf("schema family limit reached")
			}
			family++
		}
	}
	for _, retained := range versions {
		if err := retained.Check(); err != nil {
			return err
		}
	}
	if len(versions) > 0 && versions[len(versions)-1] == (platform.SchemaVersion{Version: version, Family: family}) {
		object.SchemaVersions = versions
		return nil
	}
	if len(versions) >= 64 {
		return fmt.Errorf("an object may retain at most 64 schema versions")
	}
	object.SchemaVersions = append(versions, platform.SchemaVersion{Version: version, Family: family})
	return nil
}

func objectSchemaBody(object Object) (json.RawMessage, error) {
	object.SchemaVersions = nil
	return definitionBody(object, buildapi.TypeOf(object.Name))
}

func checkObjectSchemas(object Object) error {
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
	body, err := objectSchemaBody(object)
	if err != nil {
		return err
	}
	version, err := platform.SchemaContentVersion(body)
	if err != nil || version != object.SchemaVersions[len(object.SchemaVersions)-1].Version {
		return fmt.Errorf("object schema version differs from its published definition")
	}
	return nil
}

func compatibleObjectSchema(previous, next Object) bool {
	// Display and publication provenance do not alter a submission contract.
	normalize := func(object Object) Object {
		object.Record = platform.Record{}
		object.Title, object.Plural, object.Description = "", "", ""
		object.State, object.Installed, object.Published = "", "", ""
		object.ExtensionSource, object.SchemaVersions = nil, nil
		object.Fields = nil
		object.States = slices.Clone(object.States)
		for index := range object.States {
			object.States[index].Title, object.States[index].Description, object.States[index].Tone = "", "", ""
		}
		object.Actions = slices.Clone(object.Actions)
		for index := range object.Actions {
			object.Actions[index].Title, object.Actions[index].Description = "", ""
			object.Actions[index].Inputs = slices.Clone(object.Actions[index].Inputs)
			for input := range object.Actions[index].Inputs {
				object.Actions[index].Inputs[input].Title, object.Actions[index].Inputs[input].Group = "", ""
			}
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
		if field.Required || field.Ref != "" || field.Property != nil || field.Formula != "" || field.FormulaExpr != nil || field.ReadOnly || field.AliasOf != "" || field.Choices != "" || !slices.Contains([]string{"text", "longtext", "integer", "decimal", "boolean", "date", "datetime"}, field.Type) {
			return false
		}
	}
	return true
}

func objectActions(object Object, entity platform.Entity) []platform.Action {
	actions := platform.EntityActions(entity)
	var binding platform.SchemaVersion
	if len(object.SchemaVersions) > 0 {
		binding = object.SchemaVersions[len(object.SchemaVersions)-1]
	} else if body, err := objectSchemaBody(object); err == nil {
		// Existing publications keep their bytes and original family.
		binding.Version, _ = platform.SchemaContentVersion(body)
		binding.Family = 1
	}
	if binding.Check() == nil {
		for index := range actions {
			version := binding
			actions[index].SchemaVersion = &version
		}
	}
	return actions
}
