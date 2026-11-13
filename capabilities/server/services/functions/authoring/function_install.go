package authoring

import (
	"fmt"
	"slices"

	"platformserver/platform"
)

type FunctionInstall struct {
	Manifest   platform.Manifest
	Source     func(object string) (platform.EntityInfo, bool)
	Narrowable func(object string) []string
}

func (i FunctionInstall) Validate(f platform.AIFunction) error {
	if err := f.Check(); err != nil {
		return err
	}
	info, ok := i.Source(f.Object)
	if !ok || info.App != i.Manifest.ID {
		return fmt.Errorf("function %s needs its owner's source object", f.Name)
	}
	for _, role := range f.Roles {
		if !slices.Contains(i.Manifest.AllRoles(), role) {
			return fmt.Errorf("function %s names an undeclared role", f.Name)
		}
	}
	for _, name := range f.Fields {
		field, ok := info.Field(name)
		if !ok || field.AIInput != nil && !*field.AIInput || !slices.Contains([]string{"text", "longtext", "choice", "integer", "decimal", "boolean"}, field.Type) || slices.Contains(i.Narrowable(f.Object), name) {
			return fmt.Errorf("function %s needs direct scalar source fields", f.Name)
		}
	}
	return nil
}
