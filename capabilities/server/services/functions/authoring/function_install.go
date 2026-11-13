package authoring

import (
	"fmt"
	"slices"
	"strconv"

	"platformserver/platform"
)

type FunctionInstall struct {
	Manifest   platform.Manifest
	Source     func(object string) (platform.EntityInfo, bool)
	Narrowable func(object string) []string

	Check       func(platform.AIFunction) error
	Describe    func(platform.Definition) (platform.Definition, error)
	Definitions func() []platform.Definition
	Store       func(index int, def platform.Definition)
}

func (i FunctionInstall) Install(f platform.AIFunction, version int) error {
	if version < 1 {
		return fmt.Errorf("function %s needs a published version", f.Name)
	}
	if err := i.Check(f); err != nil {
		return err
	}
	ref := platform.AssetRef{App: i.Manifest.ID, Kind: platform.AssetFunction, Name: f.Name}
	def := platform.Definition{Ref: ref, Source: "tenant", Version: i.Manifest.Version + ".function-" + strconv.Itoa(version), ContractVersion: 1, Function: &f}
	def, err := i.Describe(def)
	if err != nil {
		return err
	}
	definitions := i.Definitions()
	at := slices.IndexFunc(definitions, func(d platform.Definition) bool { return d.Ref == ref })
	if at >= 0 && definitions[at].Source != "tenant" {
		return fmt.Errorf("function %s is owned by code", ref)
	}
	for n := range definitions {
		d := &definitions[n]
		if d.Ref.App == ref.App && d.Source == "code" && d.Action != nil {
			if action, ok := i.Manifest.Actions.Action(d.Ref.Name); ok {
				d.Action = &action
			}
		}
	}
	i.Store(at, def)
	return nil
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
