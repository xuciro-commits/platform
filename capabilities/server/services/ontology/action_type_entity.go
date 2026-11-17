package ontology

import (
	"encoding/json"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	"platformserver/services/resources/publication"
)

type ActionTypeEntityEnvironment struct {
	BindActions func(platform.Caller, *Object) error
	CheckObject func(platform.Caller, Object) error
}

func ActionTypeEntity(env ActionTypeEntityEnvironment) platform.Entity {
	return platform.Entity{Type: ActionTypeType, Title: "Action type", Plural: "Action types", Model: ActionType{}, Display: "title",
		Scope:    platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{BuilderRole: platform.ScopeTenant}},
		Standard: platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{BuilderRole}, Capability: "objects"},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft", States: []platform.State{{Name: "draft", Title: "Draft"}, {Name: "published", Title: "Published"}},
			Actions: []platform.ActionType{{
				Name:        "publish",
				Title:       "Publish",
				Description: "Retain this action definition for an object publication.",
				Roles:       []string{BuilderRole},
				Capability:  "objects",
				Parameters:  []platform.Field{},
				Criteria:    platform.ActionCriteria{State: &platform.FieldCondition{Field: "state", In: []string{"draft", "published"}}},
				Rules: []platform.ActionRule{{Function: func(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *platform.Error {
					return PublishActionType(c, record.(*ActionType), env.BindActions, func(object Object) error {
						return env.CheckObject(c, object)
					})
				}}, {Edit: &platform.PropertyRule{Field: "state", Allowed: []string{"published"}}}},
			}}},
		Validate: func(c platform.Caller, value any) *platform.Error {
			return validateActionType(c, value.(*ActionType))
		}}
}

func validateActionType(c platform.Caller, action *ActionType) *platform.Error {
	if c.Replaying {
		return nil
	}
	if err := action.CheckIdentity(); err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	all, err := publication.Inventory[ActionType](c)
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
}
