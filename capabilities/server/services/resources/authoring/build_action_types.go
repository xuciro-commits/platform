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

func (b *Build) writeObject(c platform.Caller, r *pb.ChangeRecord, verb, id string, payload json.RawMessage, now time.Time) *kernel.Error {
	var input map[string]json.RawMessage
	if json.Unmarshal(payload, &input) != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The object definition is one JSON object")
	}
	if raw, supplied := input["actions"]; supplied {
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
