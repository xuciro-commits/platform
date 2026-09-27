package platformserver

import (
	"fmt"
	"slices"
	"strings"

	"platformserver/platform"
)

// registerDefinitions indexes installed code declarations at composition. The
// entity/action owners still validate and execute them. This layer checks only
// cross-descriptor identity and references, without creating a second runtime.
func (t *Tenant) registerDefinitions() error {
	classes := map[string]bool{}
	versions := map[string]string{}
	for _, app := range t.apps {
		manifest := app.Manifest()
		versions[manifest.ID] = manifest.Version
		for _, declaration := range app.Declarations() {
			classes[declaration.GetDataClass()] = true
		}
	}
	objects := map[string]platform.AssetRef{}
	for _, et := range t.records.sortedTypes() {
		info := et.info
		objects[info.Type] = platform.AssetRef{App: info.App, Kind: platform.AssetObject, Name: info.Type}
	}
	seen := map[platform.AssetRef]bool{}
	add := func(def platform.Definition) error {
		if err := def.Ref.Check(); err != nil {
			return err
		}
		if seen[def.Ref] {
			return fmt.Errorf("asset %s is declared twice", def.Ref)
		}
		seen[def.Ref] = true
		t.definitions = append(t.definitions, def)
		return nil
	}
	for _, et := range t.records.sortedTypes() {
		info := et.info
		var requires []platform.AssetRef
		for _, field := range info.Fields {
			if field.Ref == "" {
				continue
			}
			if field.Type != "reference" && field.Type != "references" {
				return fmt.Errorf("asset %s field %s has reference %s with incompatible type %s", objects[info.Type], field.Name, field.Ref, field.Type)
			}
			dep, ok := objects[field.Ref]
			if !ok {
				return fmt.Errorf("asset %s field %s requires missing object %s", objects[info.Type], field.Name, field.Ref)
			}
			requires = append(requires, dep)
		}
		if err := add(platform.Definition{Ref: objects[info.Type], Source: "code", Version: versions[info.App], ContractVersion: 1,
			Requires: uniqueRefs(requires), Entity: &info}); err != nil {
			return err
		}
	}
	for _, app := range t.apps {
		manifest := app.Manifest()
		for _, action := range manifest.Actions.All() {
			ref := platform.AssetRef{App: manifest.ID, Kind: platform.AssetAction, Name: action.Schema}
			var requires []platform.AssetRef
			if dep, ok := objects[action.Target]; ok {
				requires = append(requires, dep)
			} else if !classes[action.Target] {
				return fmt.Errorf("asset %s targets missing data class %s", ref, action.Target)
			}
			for _, field := range action.Payload {
				if field.Ref == "" {
					continue
				}
				if field.Type != "string" {
					return fmt.Errorf("asset %s payload %s has reference %s with incompatible type %s", ref, field.Name, field.Ref, field.Type)
				}
				dep, ok := objects[field.Ref]
				if !ok {
					return fmt.Errorf("asset %s payload %s requires missing object %s", ref, field.Name, field.Ref)
				}
				requires = append(requires, dep)
			}
			if err := add(platform.Definition{Ref: ref, Source: "code", Version: manifest.Version, ContractVersion: 1,
				Requires: uniqueRefs(requires), Action: &action}); err != nil {
				return err
			}
		}
	}
	slices.SortFunc(t.definitions, func(a, b platform.Definition) int { return strings.Compare(a.Ref.String(), b.Ref.String()) })
	return nil
}

func uniqueRefs(refs []platform.AssetRef) []platform.AssetRef {
	out := []platform.AssetRef{}
	for _, ref := range refs {
		if !slices.Contains(out, ref) {
			out = append(out, ref)
		}
	}
	slices.SortFunc(out, func(a, b platform.AssetRef) int { return strings.Compare(a.String(), b.String()) })
	return out
}

// Definitions serves the installed assets the member can discover. Entity
// fields and actions are filtered through the existing read/catalog paths at
// request time, so role and field changes are never cached in the registry.
func (t *Tenant) Definitions(m platform.Member) []platform.Definition {
	entities := map[string]platform.EntityInfo{}
	for _, info := range t.Entities(m) {
		entities[info.Type] = info
	}
	actions := map[string]platform.Action{}
	for _, action := range t.Catalog(m) {
		actions[action.Schema] = action
	}
	out := []platform.Definition{}
	for _, registered := range t.definitions {
		def := registered
		switch def.Ref.Kind {
		case platform.AssetObject:
			info, ok := entities[def.Ref.Name]
			if !ok {
				continue
			}
			def.Entity = &info
		case platform.AssetAction:
			action, ok := actions[def.Ref.Name]
			if !ok {
				continue
			}
			def.Action = &action
		}
		out = append(out, def)
	}
	return out
}
