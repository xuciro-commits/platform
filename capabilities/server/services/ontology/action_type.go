package ontology

import (
	"fmt"
	"slices"
	"strconv"

	"platformserver/platform"
)

type ActionTypeVersion struct {
	Version    int                        `json:"version"`
	Definition Action                     `json:"definition"`
	Commands   []platform.DispatchCommand `json:"commands,omitempty"`
}

type ActionType struct {
	platform.Record
	Name       string                     `json:"name" field:"required,search"`
	Title      string                     `json:"title" field:"readonly,search"`
	Object     string                     `json:"object" field:"required"`
	Definition Action                     `json:"definition" type:"json" field:"required"`
	Commands   []platform.DispatchCommand `json:"commands,omitempty" type:"json"`
	Version    int                        `json:"version" field:"readonly"`
	Versions   []ActionTypeVersion        `json:"versions,omitempty" type:"json" field:"readonly"`
	State      string                     `json:"state" field:"readonly" choices:"draft,published"`
	Published  string                     `json:"published,omitempty" field:"readonly" type:"longtext"`
}

func (a *ActionType) Revise(previous *ActionType) error {
	if err := a.CheckIdentity(); err != nil {
		return err
	}
	if previous != nil {
		if previous.Name != a.Name || previous.Object != a.Object {
			return fmt.Errorf("An action type keeps its target object and action API name")
		}
		a.Version, a.Versions = previous.Version, slices.Clone(previous.Versions)
		if string(platform.Raw(previous.Definition)) == string(platform.Raw(a.Definition)) && string(platform.Raw(previous.Commands)) == string(platform.Raw(a.Commands)) {
			a.Title = a.Definition.Title
			return nil
		}
	}
	a.Title = a.Definition.Title
	a.Version++
	a.Versions = append(a.Versions, ActionTypeVersion{Version: a.Version, Definition: a.Definition, Commands: a.Commands})
	return nil
}

func (a ActionType) CheckIdentity() error {
	if !Named(a.Definition.Name) || a.Name != a.Object+"."+a.Definition.Name {
		return fmt.Errorf("An action type keeps its target object and action API name")
	}
	return nil
}

func (a ActionType) Snapshot(version int) string {
	a.Record = platform.Record{ID: a.ID}
	a.Version, a.Definition, a.Commands = version, a.Versions[version-1].Definition, a.Versions[version-1].Commands
	a.Title, a.State = a.Definition.Title, "published"
	a.Published, a.Versions = "", nil
	return string(platform.Raw(a))
}

func ValidateActionTypeImage(action, frozen ActionType) error {
	if action.State != "published" || frozen.State != "published" || action.ID != frozen.ID || action.Name != frozen.Name || action.Object != frozen.Object || frozen.Version < 1 || frozen.Name != frozen.Object+"."+frozen.Definition.Name {
		return fmt.Errorf("accepted action type has no matching retained version")
	}
	return nil
}

func SameActions(left, right []Action) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if string(platform.Raw(left[i])) != string(platform.Raw(right[i])) {
			return false
		}
	}
	return true
}

func BindActionTypes(object *Object, app string, available []ActionType) error {
	if len(object.ActionTypes) == 0 {
		return nil
	}
	object.Actions = nil
	bindings := slices.Clone(object.ActionTypes)
	object.ActionTypes = nil
	commands := slices.Clone(object.DispatchCommands)
	declarations := map[string]platform.DispatchCommand{}
	for i := range bindings {
		binding := bindings[i]
		if binding.Ref.App != app || binding.Ref.Kind != platform.AssetAction {
			return fmt.Errorf("Choose an action type owned by Build")
		}
		index := slices.IndexFunc(available, func(action ActionType) bool { return action.Name == binding.Ref.Name })
		if index < 0 || available[index].Object != app+"."+object.Name {
			return fmt.Errorf("Action type %s is unavailable for this object", binding.Ref.Name)
		}
		if available[index].Archived {
			continue
		}
		action := available[index]
		version := action.Version
		if binding.SourceVersion != "" {
			var err error
			version, err = strconv.Atoi(binding.SourceVersion)
			if err != nil || version < 1 || version > len(action.Versions) {
				return fmt.Errorf("Action type %s has no version %s", binding.Ref.Name, binding.SourceVersion)
			}
		}
		binding.SourceVersion = strconv.Itoa(version)
		object.ActionTypes = append(object.ActionTypes, binding)
		object.Actions = append(object.Actions, action.Versions[version-1].Definition)
		for _, command := range action.Versions[version-1].Commands {
			if previous, exists := declarations[command.Name]; exists && string(platform.Raw(previous)) != string(platform.Raw(command)) {
				return fmt.Errorf("Action types declare different versions of command %s", command.Name)
			}
			declarations[command.Name] = command
			index := slices.IndexFunc(commands, func(existing platform.DispatchCommand) bool { return existing.Name == command.Name })
			if index < 0 {
				commands = append(commands, command)
			} else {
				commands[index] = command
			}
		}
	}
	object.DispatchCommands = commands
	return nil
}
