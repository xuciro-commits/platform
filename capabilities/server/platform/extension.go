package platform

import (
	"fmt"
	"reflect"
	"slices"
)

// ExtensionSource is publication provenance, not an execution binding: an
// extension remains owned by its original authority when its base is upgraded.
// The recorded base shape lets the release owner check that upgrade without
// rewriting the extension's accepted declaration or its customer records.
type ExtensionSource struct {
	Base   AssetBinding `json:"base"`
	Fields []FieldInfo  `json:"fields"`
}

func (s ExtensionSource) Check(typ string) error {
	if s.Base.Ref.Check() != nil || s.Base.Ref.Kind != AssetObject || s.Base.Ref.Name != typ || s.Base.SourceVersion == "" || len(s.Fields) == 0 {
		return fmt.Errorf("extension %s needs its original base package version and shape", typ)
	}
	seen := map[string]bool{}
	for _, field := range s.Fields {
		if field.Name == "" || field.Type == "" || seen[field.Name] {
			return fmt.Errorf("extension %s has an invalid original base shape", typ)
		}
		seen[field.Name] = true
	}
	return nil
}

// CheckExtension is the one structural check shared by authoring, live writes
// and upgrade admission. Retained aliases preserve historical names;
// a writable extension field may never shadow a field supplied by the base.
func CheckExtension(extension, base EntityInfo) error {
	if extension.Extends == "" || extension.Extends != base.Type || extension.Type == base.Type {
		return fmt.Errorf("extension %s has an unavailable base %s", extension.Type, extension.Extends)
	}
	link, ok := extension.Field("base")
	if !ok || link.Type != "reference" || link.Ref != base.Type || !link.Required {
		return fmt.Errorf("extension %s needs a required base reference to %s", extension.Type, base.Type)
	}
	for _, field := range extension.Fields {
		if field.Name == "base" || field.ReadOnly && field.AliasOf != "" {
			continue
		}
		if _, exists := base.Field(field.Name); exists {
			return fmt.Errorf("extension %s field %s conflicts with base %s; declare a migration to rename the extension field", extension.Type, field.Name, base.Type)
		}
	}
	if source := extension.ExtensionSource; source != nil {
		if err := source.Check(base.Type); err != nil {
			return err
		}
		if base.App != "" && source.Base.Ref.App != base.App {
			return fmt.Errorf("extension %s base authority differs from its source package", extension.Type)
		}
		for _, field := range source.Fields {
			next, exists := base.Field(field.Name)
			if !exists || field.Type != next.Type || field.Ref != next.Ref || field.Required != next.Required || field.ReadOnly != next.ReadOnly || field.AliasOf != next.AliasOf || !slices.Equal(field.Choices, next.Choices) || !reflect.DeepEqual(field.Property, next.Property) {
				return fmt.Errorf("extension %s source %s@%s has an incompatible base field %s; review and republish the extension against the new package", extension.Type, base.Type, source.Base.SourceVersion, field.Name)
			}
		}
	}
	return nil
}
