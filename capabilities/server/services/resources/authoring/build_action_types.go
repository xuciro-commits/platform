package build

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

const ActionTypeType = "build.actiontype"
const SchemaExtractActions = ObjectType + ".extract-actions"
const SchemaActionType = ActionTypeType + ".publish"

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

func (b *Build) actionTypeEntity() platform.Entity {
	return platform.Entity{Type: ActionTypeType, Title: "Action type", Plural: "Action types", Model: ActionType{}, Display: "title",
		Scope:    platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant}},
		Standard: platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder}, Capability: "objects"},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft", States: []platform.State{{Name: "draft", Title: "Draft"}, {Name: "published", Title: "Published"}},
			Transitions: []platform.Transition{{Name: "publish", Title: "Publish", Description: "Retain this action definition for an object publication.", Roles: []string{Builder}, Capability: "objects", From: []string{"draft", "published"}, To: []string{"published"}, Payload: []platform.Field{},
				Do: func(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
					action := record.(*ActionType)
					objects, err := readDefinitionInventory[Object](c)
					if err != nil {
						return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
					}
					index := slices.IndexFunc(objects, func(object Object) bool { return TypeOf(object.Name) == action.Object && !object.Archived })
					if index < 0 {
						return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose an existing object type")
					}
					object := objects[index]
					if err := b.bindObjectActions(c, &object); err != nil {
						return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
					}
					object.Actions = slices.DeleteFunc(slices.Clone(object.Actions), func(definition Action) bool { return definition.Name == action.Definition.Name })
					object.Actions = append(object.Actions, action.Definition)
					if err := b.Check(object, object.ID, c.Rules()); err != nil {
						return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
					}
					action.State, action.Published = "published", actionSnapshot(*action, action.Version)
					return nil
				}}}},
		Validate: func(c platform.Caller, value any) *kernel.Error {
			if c.Replaying {
				return nil
			}
			action := value.(*ActionType)
			if !Named(action.Definition.Name) || action.Name != action.Object+"."+action.Definition.Name {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "An action type keeps its target object and action API name")
			}
			all, err := readDefinitionInventory[ActionType](c)
			if err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
			}
			for _, other := range all {
				if other.ID != action.ID && other.Name == action.Name {
					return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "This action API name is already declared")
				}
			}
			if previous, found := platform.Get[ActionType](c, action.ID); found {
				if previous.Name != action.Name || previous.Object != action.Object {
					return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "An action type keeps its target object and action API name")
				}
				action.Version, action.Versions = previous.Version, slices.Clone(previous.Versions)
				if string(platform.Raw(previous.Definition)) == string(platform.Raw(action.Definition)) && string(platform.Raw(previous.Commands)) == string(platform.Raw(action.Commands)) {
					action.Title = action.Definition.Title
					return nil
				}
			}
			action.Title = action.Definition.Title
			action.Version++
			action.Versions = append(action.Versions, ActionTypeVersion{Version: action.Version, Definition: action.Definition, Commands: action.Commands})
			return nil
		}}
}

func actionSnapshot(action ActionType, version int) string {
	action.Record = platform.Record{ID: action.ID}
	action.Version, action.Definition, action.Commands = version, action.Versions[version-1].Definition, action.Versions[version-1].Commands
	action.Title, action.State = action.Definition.Title, "published"
	action.Published, action.Versions = "", nil
	return string(platform.Raw(action))
}

func actionTypeImage(raw []byte) error {
	var action ActionType
	if err := json.Unmarshal(raw, &action); err != nil {
		return err
	}
	frozen, found := ReadPublication[ActionType](action.Published)
	if !found || action.State != "published" || frozen.State != "published" || action.ID != frozen.ID || action.Name != frozen.Name || action.Object != frozen.Object || frozen.Version < 1 || frozen.Name != frozen.Object+"."+frozen.Definition.Name {
		return fmt.Errorf("accepted action type has no matching retained version")
	}
	return nil
}

func (b *Build) extractObjectActions(c platform.Caller, r *pb.ChangeRecord, object *Object, now time.Time) *kernel.Error {
	all, err := readDefinitionInventory[ActionType](c)
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
	}
	refs := make([]platform.AssetBinding, 0, len(object.Actions))
	for _, definition := range object.Actions {
		name := TypeOf(object.Name) + "." + definition.Name
		ref := platform.AssetRef{App: ID, Kind: platform.AssetAction, Name: name}
		if slices.ContainsFunc(refs, func(binding platform.AssetBinding) bool { return binding.Ref == ref }) {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "An object cannot repeat an action type")
		}
		action := ActionType{Record: platform.Record{ID: name}, Name: name, Object: TypeOf(object.Name), State: "draft"}
		index := slices.IndexFunc(all, func(item ActionType) bool { return item.Name == name })
		if index >= 0 {
			action = all[index]
		}
		action.Definition = definition
		if refusal := b.actionTypeEntity().Validate(c, &action); refusal != nil {
			return refusal
		}
		rid := platform.ResourceID(c.Tenant, ref)
		resource, known := platform.Get[Resource](c, rid)
		if !known {
			resource = Resource{Record: platform.Record{ID: rid}, Ref: ref, Project: WorkspaceProject}
		}
		resource.Type, resource.Draft, resource.Title = ActionTypeType, action.ID, action.Title
		parent, found := platform.Get[Resource](c, platform.ResourceID(c.Tenant, platform.AssetRef{App: ID, Kind: platform.AssetObject, Name: action.Object}))
		if found {
			resource.Project, resource.Folder, resource.ManagedBy = parent.Project, parent.Folder, parent.ManagedBy
		}
		resource.Version++
		resource.State = "active"
		if index < 0 || action.Version != all[index].Version {
			c.Put(r, resource)
			c.Put(r, action)
		}
		refs = append(refs, platform.AssetBinding{Ref: ref})
	}
	object.Actions, object.ActionTypes = nil, refs
	return nil
}

func (b *Build) writeObject(c platform.Caller, r *pb.ChangeRecord, verb, id string, payload json.RawMessage, now time.Time) *kernel.Error {
	var input map[string]json.RawMessage
	if json.Unmarshal(payload, &input) != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The object definition is one JSON object")
	}
	if _, supplied := input["actions"]; supplied {
		object, _ := platform.Get[Object](c, id)
		if err := json.Unmarshal(payload, &object); err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
		object.ID = id
		if err := b.normalizeObjectRules(&object); err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
		if refusal := b.extractObjectActions(c, r, &object, now); refusal != nil {
			return refusal
		}
		input["actions"], input["actionTypes"] = platform.Raw(nil), platform.Raw(object.ActionTypes)
		payload = platform.Raw(input)
	}
	return b.ledger.ApplyTxnRecord(c, r, b.objectEntity(), verb, id, payload, now)
}

func (b *Build) writeActionType(c platform.Caller, r *pb.ChangeRecord, verb, id string, payload json.RawMessage, now time.Time) *kernel.Error {
	if refusal := b.ledger.ApplyTxnRecord(c, r, b.actionTypeEntity(), verb, id, payload, now); refusal != nil {
		return refusal
	}
	if verb != "create" {
		return nil
	}
	action, _ := platform.Get[ActionType](c, id)
	objects, err := readDefinitionInventory[Object](c)
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
	}
	index := slices.IndexFunc(objects, func(object Object) bool { return TypeOf(object.Name) == action.Object && !object.Archived })
	if index < 0 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose an existing object type")
	}
	object := objects[index]
	ref := platform.AssetRef{App: ID, Kind: platform.AssetAction, Name: action.Name}
	object.ActionTypes = append(slices.Clone(object.ActionTypes), platform.AssetBinding{Ref: ref})
	if refusal := b.ledger.ApplyTxnRecord(c, r, b.objectEntity(), "edit", object.ID, platform.Raw(map[string]any{"actionTypes": object.ActionTypes}), now); refusal != nil {
		return refusal
	}
	b.trackResource(c, r, ObjectType, object.ID, now)
	return nil
}

func (b *Build) submitOntologyWrite(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return b.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		return func(r *pb.ChangeRecord) {
			if s.GetSchema().GetName() == SchemaExtractActions {
				object, found := platform.Get[Object](c, s.GetTarget().GetId())
				if !found || len(object.Actions) == 0 {
					return
				}
				if refusal := b.extractObjectActions(c, r, &object, now); refusal != nil {
					c.FailDecision(refusal)
					return
				}
				c.Put(r, object)
				return
			}
			verb := "edit"
			if s.GetSchema().GetName() == s.GetTarget().GetType()+".create" {
				verb = "create"
			}
			var refusal *kernel.Error
			if s.GetTarget().GetType() == ActionTypeType {
				refusal = b.writeActionType(c, r, verb, s.GetTarget().GetId(), s.GetPayload(), now)
			} else {
				refusal = b.writeObject(c, r, verb, s.GetTarget().GetId(), s.GetPayload(), now)
			}
			if refusal != nil {
				c.FailDecision(refusal)
			}
		}, nil
	})
}

func (b *Build) bindObjectActions(c platform.Caller, object *Object) error {
	if len(object.ActionTypes) == 0 {
		return nil
	}
	object.Actions = nil
	object.ActionTypes = slices.Clone(object.ActionTypes)
	all, err := readDefinitionInventory[ActionType](c)
	if err != nil {
		return err
	}
	commands := slices.Clone(object.DispatchCommands)
	declarations := map[string]platform.DispatchCommand{}
	for i := range object.ActionTypes {
		binding := &object.ActionTypes[i]
		if binding.Ref.App != ID || binding.Ref.Kind != platform.AssetAction {
			return fmt.Errorf("Choose an action type owned by Build")
		}
		index := slices.IndexFunc(all, func(action ActionType) bool { return action.Name == binding.Ref.Name })
		if index < 0 || all[index].Archived || all[index].Object != TypeOf(object.Name) {
			return fmt.Errorf("Action type %s is unavailable for this object", binding.Ref.Name)
		}
		action := all[index]
		version := action.Version
		if binding.SourceVersion != "" {
			var err error
			version, err = strconv.Atoi(binding.SourceVersion)
			if err != nil || version < 1 || version > len(action.Versions) {
				return fmt.Errorf("Action type %s has no version %s", binding.Ref.Name, binding.SourceVersion)
			}
		}
		binding.SourceVersion = strconv.Itoa(version)
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
