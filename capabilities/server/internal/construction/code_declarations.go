package construction

import (
	"encoding/json"
	"fmt"
	"slices"

	"platformserver/platform"
)

// CodeDeclarations checks references between installed declarations. Every
// input is a value supplied by its existing owner; this checker cannot read
// tenant state, install definitions, execute callbacks or grant permissions.
type CodeDeclarations struct {
	Manifests       []platform.Manifest
	Entities        []platform.EntityInfo
	DataClasses     map[string]bool
	NarrowedFields  map[string][]string
	NativeExecutors map[string]bool
}

func (c CodeDeclarations) Check() error {
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
			// Enterprise identities belong to the directory. An optional reference
			// remains declarable before that owner is mounted.
			if field.Ref == "enterprise.element" {
				continue
			}
			if _, ok := objects[field.Ref]; !ok {
				return fmt.Errorf("asset %s field %s requires missing object %s", objects[info.Type], field.Name, field.Ref)
			}
		}
	}
	actions := map[platform.AssetRef]platform.Action{}
	for _, m := range c.Manifests {
		for _, action := range m.Actions.All() {
			ref := platform.AssetRef{App: m.ID, Kind: platform.AssetAction, Name: action.Schema}
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
	for _, m := range c.Manifests {
		for _, page := range m.Pages {
			ref := platform.AssetRef{App: m.ID, Kind: platform.AssetPage, Name: page.Name}
			if page.Layout != "list-detail" && page.Layout != "composed" {
				return fmt.Errorf("asset %s has unsupported layout %q", ref, page.Layout)
			}
			if page.Document != nil && page.Layout != "composed" {
				return fmt.Errorf("asset %s: a structured layout needs composed sections", ref)
			}
			if err := page.Document.Check(page.Sections); err != nil {
				return fmt.Errorf("asset %s: %w", ref, err)
			}
			if page.Object.Kind != platform.AssetObject || objects[page.Object.Name] != page.Object {
				return fmt.Errorf("asset %s requires missing object %s", ref, page.Object)
			}
			if len(page.Selections) != 0 && len(page.Sections) == 0 {
				return fmt.Errorf("asset %s: selections need composed sections", ref)
			}
			info := infos[page.Object.Name]
			if len(page.Sections) == 0 {
				for _, fields := range [][]string{page.ListFields, page.DetailFields} {
					if len(fields) == 0 {
						return fmt.Errorf("asset %s needs list and detail fields", ref)
					}
					seen := map[string]bool{}
					for _, field := range fields {
						if seen[field] {
							return fmt.Errorf("asset %s repeats field %s", ref, field)
						}
						seen[field] = true
						if _, ok := info.Field(field); !ok {
							return fmt.Errorf("asset %s requires missing field %s on %s", ref, field, page.Object)
						}
					}
				}
			}
			seen := map[platform.AssetRef]bool{}
			for _, actionRef := range page.Actions {
				action, ok := actions[actionRef]
				if !ok {
					return fmt.Errorf("asset %s requires missing action %s", ref, actionRef)
				}
				if action.Target != page.Object.Name {
					return fmt.Errorf("asset %s action %s targets %s, not %s", ref, actionRef, action.Target, page.Object.Name)
				}
				if seen[actionRef] {
					return fmt.Errorf("asset %s repeats an action", ref)
				}
				seen[actionRef] = true
			}
		}
	}
	for _, m := range c.Manifests {
		for _, p := range m.PropertyTypes {
			if err := p.Check(); err != nil {
				return err
			}
		}
		for _, l := range m.LinkTypes {
			parent, ok := infos[l.Parent.Name]
			if !ok {
				return fmt.Errorf("link parent missing")
			}
			child, ok := infos[l.Child.Name]
			if !ok || l.Child.App != m.ID {
				return fmt.Errorf("link child owner differs")
			}
			if err := l.CheckSchema(parent, child); err != nil {
				return err
			}
		}
		for _, q := range m.Queries {
			if q.Interface != "" {
				continue
			} // its original owner binds retained implementers
			ref := platform.AssetRef{App: m.ID, Kind: platform.AssetQuery, Name: q.Name}
			if _, ok := objects[q.Object]; !ok {
				return fmt.Errorf("asset %s reads missing object %s", ref, q.Object)
			}
			if q.By != "" {
				if f, ok := infos[q.Object].Field(q.By); !ok || f.Type != "reference" {
					return fmt.Errorf("asset %s is run for %s, not a reference of %s", ref, q.By, q.Object)
				}
			}
			var terms []any
			if len(q.Domain) > 0 && json.Unmarshal(q.Domain, &terms) != nil {
				return fmt.Errorf("asset %s has a domain that is not a list of conditions", ref)
			}
		}
		for _, f := range m.Functions {
			ref := platform.AssetRef{App: m.ID, Kind: platform.AssetFunction, Name: f.Name}
			if err := f.Check(); err != nil {
				return err
			}
			if object, ok := objects[f.Object]; !ok || object.App != m.ID {
				return fmt.Errorf("asset %s needs its owner's source object", ref)
			}
			for _, role := range f.Roles {
				if !slices.Contains(m.AllRoles(), role) {
					return fmt.Errorf("asset %s names an undeclared role", ref)
				}
			}
			for _, name := range f.Fields {
				field, ok := infos[f.Object].Field(name)
				if !ok || !slices.Contains([]string{"text", "longtext", "choice", "integer", "decimal", "boolean"}, field.Type) || slices.Contains(c.NarrowedFields[f.Object], name) {
					return fmt.Errorf("asset %s needs direct scalar source fields", ref)
				}
			}
		}
		for _, o := range m.Operations {
			if err := o.Check(); err != nil {
				return fmt.Errorf("operation %s: %w", o.Name, err)
			}
			for _, role := range o.Roles {
				if !slices.Contains(m.AllRoles(), role) {
					return fmt.Errorf("operation %s names an undeclared role", o.Name)
				}
			}
			if o.Binding.Kind == "native" && !c.NativeExecutors[m.ID] {
				return fmt.Errorf("operation %s has no native executor", o.Name)
			}
		}
	}
	for _, m := range c.Manifests {
		for _, observation := range m.Observations {
			if err := platform.CheckObservationDefinition(observation, lookup); err != nil {
				return err
			}
		}
	}
	return nil
}

// AgentDescriptor seals declaration data without invoking a guard, test or
// handoff callback. The original agent owner remains responsible for running it.
func AgentDescriptor(agent platform.Agent) (platform.AgentReleaseDescriptor, error) {
	cases := []map[string]any{}
	for _, test := range agent.Cases {
		cases = append(cases, map[string]any{"name": test.Name, "goal": test.Goal, "ref": test.Ref, "check": test.Check != nil})
	}
	raw, err := json.Marshal(cases)
	if err != nil {
		return platform.AgentReleaseDescriptor{}, err
	}
	descriptor := platform.AgentReleaseDescriptor{Model: agent.Model, ModelOptions: agent.ModelOptions, Name: agent.Name, Title: agent.Title, Description: agent.Description, Instructions: agent.Instructions, Tools: slices.Clone(agent.Tools), Budget: agent.Budget, Checkpoints: slices.Clone(agent.Checkpoints), Native: true, Guard: agent.Guard != nil, Handoff: agent.To != nil, Cases: raw}
	return descriptor, descriptor.Check()
}

// ProtocolDescriptor binds the protocol's public actions to their original
// provider declarations. No protocol execution or cross-owner call occurs here.
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
