package publication

import (
	"encoding/json"
	"fmt"
	"slices"

	"platformserver/platform"
)

type CodeDeclarations struct {
	Manifests       []platform.Manifest
	Entities        []platform.EntityInfo
	DataClasses     map[string]bool
	NarrowedFields  map[string][]string
	NativeExecutors map[string]bool
}

func (c CodeDeclarations) Check(checkPage func(platform.Page) error) error {
	objects := map[string]platform.AssetRef{}
	infos := map[string]platform.EntityInfo{}
	for _, info := range c.Entities {
		objects[info.Type] = platform.AssetRef{App: info.App, Kind: platform.AssetObject, Name: info.Type}
		infos[info.Type] = info
	}
	lookup := func(typ string) (platform.EntityInfo, bool) { info, ok := infos[typ]; return info, ok }
	for _, info := range c.Entities {
		for _, field := range info.Fields {
			if field.Ref == "" {
				continue
			}
			if field.Type != "reference" && field.Type != "references" {
				return fmt.Errorf("asset %s field %s has reference %s with incompatible type %s", objects[info.Type], field.Name, field.Ref, field.Type)
			}
			if field.Ref == "enterprise.element" {
				continue
			}
			if _, ok := objects[field.Ref]; !ok {
				return fmt.Errorf("asset %s field %s requires missing object %s", objects[info.Type], field.Name, field.Ref)
			}
		}
	}
	actions := map[platform.AssetRef]platform.Action{}
	for _, manifest := range c.Manifests {
		for _, action := range manifest.Actions.All() {
			ref := platform.AssetRef{App: manifest.ID, Kind: platform.AssetAction, Name: action.Schema}
			if _, ok := objects[action.Target]; !ok && !c.DataClasses[action.Target] {
				return fmt.Errorf("asset %s targets missing data class %s", ref, action.Target)
			}
			for _, field := range action.Payload {
				if field.Ref == "" {
					continue
				}
				if field.Type != "string" {
					return fmt.Errorf("asset %s payload %s has reference %s with incompatible type %s", ref, field.Name, field.Ref, field.Type)
				}
				if field.Ref == "enterprise.element" {
					continue
				}
				if _, ok := objects[field.Ref]; !ok {
					return fmt.Errorf("asset %s payload %s requires missing object %s", ref, field.Name, field.Ref)
				}
			}
			actions[ref] = action
		}
	}
	for _, manifest := range c.Manifests {
		for _, page := range manifest.Pages {
			ref := platform.AssetRef{App: manifest.ID, Kind: platform.AssetPage, Name: page.Name}
			if err := checkPage(page); err != nil {
				return fmt.Errorf("asset %s: %w", ref, err)
			}
			if page.Object.Kind != platform.AssetObject || objects[page.Object.Name] != page.Object {
				return fmt.Errorf("asset %s requires missing object %s", ref, page.Object)
			}
			boundObjects := map[string]string{"page": page.Object.Name}
			for _, binding := range page.DataBindings {
				object := binding.Object.Name
				if binding.Model != nil {
					object = binding.Model.Type
				}
				if object != "" {
					if _, ok := objects[object]; !ok {
						return fmt.Errorf("asset %s requires missing object %s", ref, object)
					}
				}
				boundObjects[binding.ID] = object
			}
			for _, trigger := range page.ActionTriggers {
				seen := map[platform.AssetRef]bool{}
				for _, actionRef := range trigger.Actions {
					action, ok := actions[actionRef]
					if !ok {
						return fmt.Errorf("asset %s requires missing action %s", ref, actionRef)
					}
					if object := boundObjects[trigger.Binding]; object != "" && action.Target != object {
						return fmt.Errorf("asset %s action %s targets %s, not %s", ref, actionRef, action.Target, object)
					}
					if seen[actionRef] {
						return fmt.Errorf("asset %s repeats an action", ref)
					}
					seen[actionRef] = true
				}
			}
		}
	}
	for _, manifest := range c.Manifests {
		for _, property := range manifest.PropertyTypes {
			if err := property.Check(); err != nil {
				return err
			}
		}
		for _, value := range manifest.ValueTypes {
			if err := value.Check(); err != nil {
				return err
			}
		}
		for _, link := range manifest.LinkTypes {
			parent, ok := infos[link.Parent.Name]
			if !ok {
				return fmt.Errorf("link parent missing")
			}
			child, ok := infos[link.Child.Name]
			if !ok || link.Child.App != manifest.ID {
				return fmt.Errorf("link child owner differs")
			}
			if err := link.CheckSchema(parent, child); err != nil {
				return err
			}
		}
		for _, query := range manifest.Queries {
			if query.Interface != "" {
				continue
			}
			ref := platform.AssetRef{App: manifest.ID, Kind: platform.AssetQuery, Name: query.Name}
			if _, ok := objects[query.Object]; !ok {
				return fmt.Errorf("asset %s reads missing object %s", ref, query.Object)
			}
			if query.By != "" {
				if field, ok := infos[query.Object].Field(query.By); !ok || field.Type != "reference" {
					return fmt.Errorf("asset %s is run for %s, not a reference of %s", ref, query.By, query.Object)
				}
			}
			var terms []any
			if len(query.Domain) > 0 && json.Unmarshal(query.Domain, &terms) != nil {
				return fmt.Errorf("asset %s has a domain that is not a list of conditions", ref)
			}
		}
		for _, function := range manifest.Functions {
			ref := platform.AssetRef{App: manifest.ID, Kind: platform.AssetFunction, Name: function.Name}
			if err := function.Check(); err != nil {
				return err
			}
			if object, ok := objects[function.Object]; !ok || object.App != manifest.ID {
				return fmt.Errorf("asset %s needs its owner's source object", ref)
			}
			for _, role := range function.Roles {
				if !slices.Contains(manifest.AllRoles(), role) {
					return fmt.Errorf("asset %s names an undeclared role", ref)
				}
			}
			for _, name := range function.Fields {
				field, ok := infos[function.Object].Field(name)
				if !ok || !slices.Contains([]string{"text", "longtext", "choice", "integer", "decimal", "boolean"}, field.Type) || slices.Contains(c.NarrowedFields[function.Object], name) {
					return fmt.Errorf("asset %s needs direct scalar source fields", ref)
				}
			}
		}
		for _, operation := range manifest.Operations {
			if err := operation.Check(); err != nil {
				return fmt.Errorf("operation %s: %w", operation.Name, err)
			}
			for _, role := range operation.Roles {
				if !slices.Contains(manifest.AllRoles(), role) {
					return fmt.Errorf("operation %s names an undeclared role", operation.Name)
				}
			}
			if operation.Binding.Kind == "native" && !c.NativeExecutors[manifest.ID] {
				return fmt.Errorf("operation %s has no native executor", operation.Name)
			}
		}
	}
	for _, manifest := range c.Manifests {
		for _, observation := range manifest.Observations {
			if err := platform.CheckObservationDefinition(observation, lookup); err != nil {
				return err
			}
		}
	}
	return nil
}

func ProtocolDescriptor(manifest platform.Manifest, provision platform.Provision) (platform.ProtocolReleaseDescriptor, error) {
	protocol := platform.ProtocolReleaseDescriptor{Name: provision.Protocol.Name, Version: provision.Protocol.Version, Actions: slices.Clone(provision.Protocol.Actions), Reads: slices.Clone(provision.Protocol.Reads), ActionBindings: map[string]platform.AssetRef{}, ActionRoles: map[string][]string{}}
	for i, action := range protocol.Actions {
		name, ok := provision.Actions[action.Schema]
		declared, exists := manifest.Actions.Action(name)
		if !ok || !exists {
			return protocol, fmt.Errorf("protocol %s action %s has no provider declaration", protocol.Name, action.Schema)
		}
		protocol.ActionBindings[action.Schema] = platform.AssetRef{App: manifest.ID, Kind: platform.AssetAction, Name: name}
		protocol.Actions[i].Roles = slices.Clone(declared.Roles)
		protocol.ActionRoles[action.Schema] = slices.Clone(declared.Roles)
	}
	return protocol, nil
}
