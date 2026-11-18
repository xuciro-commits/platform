package authoring

import (
	"encoding/json"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	ontology "platformserver/services/ontology"
)

func (b *Build) actionTypeEntity() platform.RecordType {
	return ontology.ActionTypeEntity(ontology.ActionTypeEntityEnvironment{
		BindActions: b.bindObjectActions,
		CheckObject: func(c platform.Caller, object ontology.Object) error {
			return b.Check(object, object.ID, c.Rules())
		},
	})
}

func (b *Build) writeObject(c platform.Caller, r *pb.ChangeRecord, verb, id string, payload json.RawMessage, now time.Time) *platform.Error {
	var input map[string]json.RawMessage
	if json.Unmarshal(payload, &input) != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The object definition is one JSON object")
	}
	if raw, supplied := input["actions"]; supplied {
		current, found := platform.Get[ontology.Object](c, id)
		var actions []ontology.Action
		if json.Unmarshal(raw, &actions) != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The object actions are one JSON array")
		}
		if (found || len(actions) > 0) && !ontology.SameActions(actions, current.Actions) {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Actions are edited as action type resources; save the object without changing its actions")
		}
		delete(input, "actions")
		payload = platform.Raw(input)
	}
	return b.authority.ApplyTxnRecord(c, r, b.objectEntity(), verb, id, payload, now)
}

func (b *Build) writeActionType(c platform.Caller, r *pb.ChangeRecord, verb, id string, payload json.RawMessage, now time.Time) *platform.Error {
	if refusal := b.authority.ApplyTxnRecord(c, r, b.actionTypeEntity(), verb, id, payload, now); refusal != nil {
		return refusal
	}
	action, _ := platform.Get[ontology.ActionType](c, id)
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
	objects, err := readDefinitionInventory[ontology.Object](c)
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
	}
	index := slices.IndexFunc(objects, func(object ontology.Object) bool { return TypeOf(object.Name) == action.Object && !object.Archived })
	if index < 0 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose an existing object type")
	}
	object := objects[index]
	ref := platform.AssetRef{App: ID, Kind: platform.AssetAction, Name: action.Name}
	if !slices.ContainsFunc(object.ActionTypes, func(binding platform.AssetBinding) bool { return binding.Ref == ref }) {
		object.ActionTypes = append(slices.Clone(object.ActionTypes), platform.AssetBinding{Ref: ref})
		if refusal := b.authority.ApplyTxnRecord(c, r, b.objectEntity(), "edit", object.ID, platform.Raw(map[string]any{"actionTypes": object.ActionTypes}), now); refusal != nil {
			return refusal
		}
		b.trackResource(c, r, ObjectType, object.ID, now)
	}
	return nil
}

func (b *Build) submitOntologyWrite(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *platform.Error) {
	return b.authority.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *platform.Error) {
		return func(r *pb.ChangeRecord) {
			verb := "edit"
			if s.GetSchema().GetName() == s.GetTarget().GetType()+".create" {
				verb = "create"
			}
			var refusal *platform.Error
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

func (b *Build) bindObjectActions(c platform.Caller, object *ontology.Object) error {
	if len(object.ActionTypes) == 0 {
		return nil
	}
	all, err := readDefinitionInventory[ontology.ActionType](c)
	if err != nil {
		return err
	}
	return ontology.BindActionTypes(object, ID, all)
}
