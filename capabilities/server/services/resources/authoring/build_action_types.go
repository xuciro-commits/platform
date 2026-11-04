package build

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	ontology "platformserver/services/ontology"
)

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
					action.State, action.Published = "published", action.Snapshot(action.Version)
					return nil
				}}}},
		Validate: func(c platform.Caller, value any) *kernel.Error {
			if c.Replaying {
				return nil
			}
			action := value.(*ActionType)
			if err := action.CheckIdentity(); err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
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
			var previous *ActionType
			if stored, found := platform.Get[ActionType](c, action.ID); found {
				previous = &stored
			}
			if err := action.Revise(previous); err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
			return nil
		}}
}

func actionTypeImage(raw []byte) error {
	var action ActionType
	if err := json.Unmarshal(raw, &action); err != nil {
		return err
	}
	frozen, found := ReadPublication[ActionType](action.Published)
	if !found {
		return fmt.Errorf("accepted action type has no matching retained version")
	}
	return ontology.ValidateActionTypeImage(action, frozen)
}

func (b *Build) extractObjectActions(c platform.Caller, r *pb.ChangeRecord, object *Object, now time.Time) *kernel.Error {
	all, err := readDefinitionInventory[ActionType](c)
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
	}
	refs := slices.Clone(object.ActionTypes)
	seen := map[string]bool{}
	for _, definition := range object.Actions {
		name := TypeOf(object.Name) + "." + definition.Name
		if seen[name] {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "An object cannot repeat an action type")
		}
		seen[name] = true
		ref := platform.AssetRef{App: ID, Kind: platform.AssetAction, Name: name}
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
		parent, found := platform.Get[Resource](c, platform.ResourceID(c.Tenant, platform.AssetRef{App: ID, Kind: platform.AssetObject, Name: action.Object}))
		project := submissionProject(r.GetSubmission())
		if found && parent.Project != "" {
			project = parent.Project
		}
		if project == "" {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The action's object has no project")
		}
		if !known {
			resource = Resource{Record: platform.Record{ID: rid}, Ref: ref, Project: project}
			if found {
				resource.Folder, resource.ManagedBy = parent.Folder, parent.ManagedBy
			}
		}
		resource.Type, resource.Draft, resource.Title = ActionTypeType, action.ID, action.Title
		resource.Version++
		resource.State = "active"
		if index < 0 || action.Version != all[index].Version {
			c.Put(r, resource)
			c.Put(r, action)
		}
		if !slices.ContainsFunc(refs, func(binding platform.AssetBinding) bool { return binding.Ref == ref }) {
			refs = append(refs, platform.AssetBinding{Ref: ref})
		}
	}
	object.Actions, object.ActionTypes = nil, refs
	return nil
}

func (b *Build) writeObject(c platform.Caller, r *pb.ChangeRecord, verb, id string, payload json.RawMessage, now time.Time) *kernel.Error {
	var input map[string]json.RawMessage
	if json.Unmarshal(payload, &input) != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The object definition is one JSON object")
	}
	if _, supplied := input["actions"]; supplied && c.Replaying {
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
	if raw, supplied := input["actions"]; supplied && !c.Replaying {
		current, found := platform.Get[Object](c, id)
		var actions []Action
		if json.Unmarshal(raw, &actions) != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The object actions are one JSON array")
		}
		if (found || len(actions) > 0) && !ontology.SameActions(actions, current.Actions) {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Actions are edited as action type resources; save the object without changing its actions")
		}
		delete(input, "actions")
		payload = platform.Raw(input)
	}
	return b.ledger.ApplyTxnRecord(c, r, b.objectEntity(), verb, id, payload, now)
}

func (b *Build) writeActionType(c platform.Caller, r *pb.ChangeRecord, verb, id string, payload json.RawMessage, now time.Time) *kernel.Error {
	if refusal := b.ledger.ApplyTxnRecord(c, r, b.actionTypeEntity(), verb, id, payload, now); refusal != nil {
		return refusal
	}
	action, _ := platform.Get[ActionType](c, id)
	if !c.Replaying {
		revision := r.GetRevision()
		if revision == 0 {
			revision = uint32(action.Version)
		}
		if action.Record.Revision != revision {
			action.Record.Revision = revision
			c.Put(r, action)
		}
	}
	if verb != "create" {
		return nil
	}
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
	if !slices.ContainsFunc(object.ActionTypes, func(binding platform.AssetBinding) bool { return binding.Ref == ref }) {
		object.ActionTypes = append(slices.Clone(object.ActionTypes), platform.AssetBinding{Ref: ref})
		if refusal := b.ledger.ApplyTxnRecord(c, r, b.objectEntity(), "edit", object.ID, platform.Raw(map[string]any{"actionTypes": object.ActionTypes}), now); refusal != nil {
			return refusal
		}
		b.trackResource(c, r, ObjectType, object.ID, now)
	}
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
	all, err := readDefinitionInventory[ActionType](c)
	if err != nil {
		return err
	}
	return ontology.BindActionTypes(object, ID, all)
}
