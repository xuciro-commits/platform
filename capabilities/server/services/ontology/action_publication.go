package ontology

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	"platformserver/services/resources/publication"
)

func ActionTypePublication() publication.Hooks {
	return publication.Hooks{
		Kinds: []platform.AssetKind{platform.AssetAction}, Schemas: []publication.SchemaHooks{{Name: SchemaActionType, Validate: ValidateActionTypePublication, Install: ValidateActionTypePublication}}, FreezeOn: platform.AssetObject,
		Freeze: FreezeActionTypes, Closure: ActionTypeClosure,
		ReadFrozen: ReadActionTypeFrozen,
	}
}

func PublishActionType(c platform.Caller, action *ActionType, bind func(platform.Caller, *Object) error, check func(Object) error) *platform.Error {
	objects, err := publication.Inventory[Object](c)
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
	}
	index := slices.IndexFunc(objects, func(object Object) bool { return AppID+"."+object.Name == action.Object && !object.Archived })
	if index < 0 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose an existing object type")
	}
	object := objects[index]
	if err := bind(c, &object); err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	object.Actions = slices.DeleteFunc(slices.Clone(object.Actions), func(definition Action) bool { return definition.Name == action.Definition.Name })
	object.Actions = append(object.Actions, action.Definition)
	if err := check(object); err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	action.State, action.Published = "published", action.Snapshot(action.Version)
	return nil
}

func ReadActionTypeFrozen(image json.RawMessage) (json.RawMessage, error) {
	var action ActionType
	if err := json.Unmarshal(image, &action); err != nil {
		return nil, err
	}
	return json.RawMessage(action.Published), nil
}

func ValidateActionTypePublication(image json.RawMessage) error {
	var action ActionType
	if err := json.Unmarshal(image, &action); err != nil {
		return err
	}
	frozen, found := publication.Read[ActionType](action.Published)
	if !found {
		return fmt.Errorf("accepted action type has no matching retained version")
	}
	return ValidateActionTypeImage(action, frozen)
}

func ActionTypeClosure(c platform.Caller, id string, _ platform.Member) (publication.DraftClosure, error) {
	var closure publication.DraftClosure
	action, found := platform.Get[ActionType](c, id)
	if !found || action.Archived {
		return closure, fmt.Errorf("action type %s is unavailable", id)
	}
	objects, err := publication.Inventory[Object](c)
	if err != nil {
		return closure, err
	}
	i := slices.IndexFunc(objects, func(object Object) bool { return AppID+"."+object.Name == action.Object && !object.Archived })
	if i < 0 {
		return closure, fmt.Errorf("action type %s has no target object", id)
	}
	closure.Requires = []publication.DraftRef{{Kind: platform.AssetObject, ID: objects[i].ID}}
	closure.Next = platform.AssetRef{App: AppID, Kind: platform.AssetAction, Name: action.Name}
	original, _ := platform.Get[Object](c, objects[i].ID)
	if installed, found := publication.Read[Object](original.Published); found && slices.ContainsFunc(installed.Actions, func(definition Action) bool { return definition.Name == action.Definition.Name }) {
		closure.Prior, closure.HasPrior = closure.Next, true
	}
	return closure, nil
}

func FreezeActionTypes(c platform.Caller, _ string, asset platform.ReleaseAsset, assets []platform.ReleaseAsset) (publication.Frozen, error) {
	var frozen Object
	if err := json.Unmarshal(asset.Body, &frozen); err != nil {
		return publication.Frozen{}, err
	}
	all, err := publication.Inventory[ActionType](c)
	if err != nil {
		return publication.Frozen{}, err
	}
	var plans []platform.AssetInstallation
	add := func(action ActionType) error {
		raw, err := json.Marshal(action)
		if err != nil {
			return fmt.Errorf("%s: %w", SchemaActionType, err)
		}
		plans = append(plans, platform.AssetInstallation{Ref: platform.AssetRef{App: AppID, Kind: platform.AssetAction, Name: action.Name}, Schema: SchemaActionType, Record: ActionTypeType + "/" + action.ID, Image: raw})
		return nil
	}
	bindings := slices.Clone(frozen.ActionTypes)
	legacyBindings := len(bindings) == 0
	if len(bindings) == 0 {
		for _, definition := range frozen.Actions {
			name := AppID + "." + frozen.Name + "." + definition.Name
			index := slices.IndexFunc(all, func(action ActionType) bool { return action.Name == name })
			if index >= 0 {
				bindings = append(bindings, platform.AssetBinding{Ref: platform.AssetRef{App: AppID, Kind: platform.AssetAction, Name: name}, SourceVersion: strconv.Itoa(all[index].Version)})
			}
		}
	}
	for _, binding := range bindings {
		if !slices.ContainsFunc(assets, func(asset platform.ReleaseAsset) bool { return asset.Ref == binding.Ref }) {
			continue
		}
		index := slices.IndexFunc(all, func(action ActionType) bool { return action.Name == binding.Ref.Name })
		if index < 0 {
			return publication.Frozen{}, fmt.Errorf("action type %s is unavailable", binding.Ref.Name)
		}
		version, err := strconv.Atoi(binding.SourceVersion)
		if err != nil || version < 1 {
			return publication.Frozen{}, fmt.Errorf("action type %s has no retained version %s", binding.Ref.Name, binding.SourceVersion)
		}
		definition := slices.IndexFunc(frozen.Actions, func(definition Action) bool { return AppID+"."+frozen.Name+"."+definition.Name == binding.Ref.Name })
		if definition < 0 {
			return publication.Frozen{}, fmt.Errorf("action type %s has no frozen definition", binding.Ref.Name)
		}
		action := all[index]
		if !legacyBindings {
			if version > len(action.Versions) {
				return publication.Frozen{}, fmt.Errorf("action type %s has no retained version %d", binding.Ref.Name, version)
			}
			retained := action.Versions[version-1]
			selected := ActionType{Record: platform.Record{ID: action.ID}, Name: action.Name, Object: action.Object, Title: retained.Definition.Title, Definition: retained.Definition, Commands: retained.Commands, State: "published", Version: version, Versions: slices.Clone(action.Versions[:version])}
			selected.Published = selected.Snapshot(version)
			if err := add(selected); err != nil {
				return publication.Frozen{}, err
			}
			continue
		}
		selected := ActionType{Record: platform.Record{ID: action.ID}, Name: action.Name, Object: action.Object, Title: frozen.Actions[definition].Title, Definition: frozen.Actions[definition], State: "published", Version: version}
		if version <= len(action.Versions) && string(platform.Raw(action.Versions[version-1].Definition)) == string(platform.Raw(selected.Definition)) {
			selected.Commands = action.Versions[version-1].Commands
		}
		action.State, action.Published = "published", string(platform.Raw(selected))
		if err := add(action); err != nil {
			return publication.Frozen{}, err
		}
	}
	return publication.Frozen{Asset: asset, Installations: plans}, nil
}
