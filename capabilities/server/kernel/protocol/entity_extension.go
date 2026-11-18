package protocol

import (
	"fmt"
	"maps"
	"platformserver/platform"
	"reflect"
	"slices"
	"strings"
)

func CheckExtension(extension, base platform.EntityInfo) error {
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
			if !exists || field.Type != next.Type || field.Ref != next.Ref || field.Required != next.Required || field.ReadOnly != next.ReadOnly || field.AliasOf != next.AliasOf || !slices.Equal(field.Choices, next.Choices) || !reflect.DeepEqual(field.Property, next.Property) || !reflect.DeepEqual(field.ValueType, next.ValueType) {
				return fmt.Errorf("extension %s source %s@%s has an incompatible base field %s; review and republish the extension against the new package", extension.Type, base.Type, source.Base.SourceVersion, field.Name)
			}
		}
	}
	return nil
}

func CheckInterfaceRegistry(interfaces []platform.Interface, infos map[string]platform.EntityInfo) error {
	declared := map[string]platform.Interface{}
	for _, i := range interfaces {
		app, _, _ := strings.Cut(i.Name, ".")
		if err := i.Check(app); err != nil {
			return err
		}
		if _, dup := declared[i.Name]; dup {
			return fmt.Errorf("interface %s is declared twice", i.Name)
		}
		declared[i.Name] = i
	}
	types := slices.Sorted(maps.Keys(infos))
	for _, typ := range types {
		info := infos[typ]
		for k, name := range info.Implements {
			if slices.Contains(info.Implements[:k], name) {
				return fmt.Errorf("%s implements %s twice", typ, name)
			}
			i, ok := declared[name]
			if !ok {
				return fmt.Errorf("%s implements %s, which is not in the interface registry", typ, name)
			}
			if err := i.Implements(info); err != nil {
				return err
			}
		}
	}
	return nil
}
