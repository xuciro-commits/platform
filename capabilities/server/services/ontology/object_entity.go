package ontology

import (
	"encoding/json"
	"platformserver/kernel/protocol"
	"reflect"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

type ObjectRuntimeEnvironment struct {
	Lookup      func(string) (platform.RecordTypeInfo, bool)
	Transaction func(platform.Caller, *pb.ChangeRecord, *platform.BusinessTxn, time.Time) *platform.Error
}

func CompileObjectEntity(o Object, env ObjectRuntimeEnvironment) platform.RecordType {
	entity := CompileObjectShape(o, AppID, BuilderRole)
	std, scope, roles := CompileAccess(o, BuilderRole, UserRole)
	std.Restore = std.Archive && !slices.ContainsFunc(o.Actions, func(action Action) bool { return action.Name == "restore" })
	entity.Standard, entity.Scope = std, scope
	entity.Lifecycle = compileLifecycle(o, roles, env)
	entity.TransactionDestination = TransactionDestination
	entity.Validate = validateObject(o, entity.Model, env.Lookup)
	entity.CreateTxn = o.CreateTransaction
	entity.EnterpriseUse = o.EnterpriseUse
	if o.CreateTransaction != nil {
		entity.ApplyCreateTxn = func(c platform.Caller, r *pb.ChangeRecord, now time.Time) *platform.Error {
			if env.Transaction == nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Creation transactions need their original owner")
			}
			return env.Transaction(c, r, o.CreateTransaction, now)
		}
	}
	return entity
}

func compileLifecycle(o Object, roles []string, env ObjectRuntimeEnvironment) *platform.Lifecycle {
	if len(o.States) == 0 {
		return nil
	}
	life := &platform.Lifecycle{Field: "state", Initial: o.States[0].Name}
	for _, state := range o.States {
		life.States = append(life.States, platform.State{Name: state.Name, Title: state.Title, Tone: state.Tone, Description: state.Description})
	}
	for _, action := range o.Actions {
		payload := []platform.Field{}
		for _, input := range action.Inputs {
			payload = append(payload, input.Field())
		}
		takers := slices.Clone(roles)
		if len(action.Roles) > 0 {
			takers = append([]string{BuilderRole}, action.Roles...)
		}
		var approval *platform.Approval
		if action.Approval != nil {
			approval = &platform.Approval{Pending: action.Approval.Pending, Rejected: action.Approval.Rejected}
			for _, level := range action.Approval.Levels {
				approval.Levels = append(approval.Levels, platform.ApprovalLevel{Title: level.Title, AppRole: level.Role, All: level.All})
			}
		}
		criteria, _ := ActionCriteria(o, AppID, BuilderRole, action, env.Lookup)
		description := action.Description
		if description == "" {
			description = "Apply this business transaction."
		}
		life.Actions = append(life.Actions, platform.ActionType{ConfirmationOnly: action.ConfirmationOnly, Adoption: action.Adoption,
			Criteria: platform.ActionCriteria{State: &platform.FieldCondition{Field: life.Field, In: slices.Clone(action.From)}, Business: criteria},
			Name:     action.Name, Title: action.Title, Description: description, Roles: takers, Capability: o.Name, Parameters: payload, Approval: approval, DeferRequired: true,
			Rules: []platform.ActionRule{{Transaction: &platform.TransactionRule{Spec: action.Transaction, Run: func(c platform.Caller, r *pb.ChangeRecord, now time.Time) *platform.Error {
				if env.Transaction == nil {
					return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Business transactions need their original owner")
				}
				return env.Transaction(c, r, action.Transaction, now)
			}}}}})
	}
	return life
}

func validateObject(o Object, model any, lookup func(string) (platform.RecordTypeInfo, bool)) func(platform.Caller, any) *platform.Error {
	if o.Extends == "" {
		return nil
	}
	typ := reflect.TypeOf(model)
	return func(c platform.Caller, record any) *platform.Error {
		if !c.Replaying && lookup != nil {
			base, exists := lookup(o.Extends)
			if !exists {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Extension base {type} is unavailable", o.Extends)
			}
			shape := platform.RecordTypeInfo{Type: AppID + "." + o.Name, Extends: o.Extends, ExtensionSource: o.ExtensionSource}
			for _, field := range o.Fields {
				shape.Fields = append(shape.Fields, platform.FieldInfo{Name: field.Name, Type: field.Type, Ref: field.Ref, Required: field.Required, ReadOnly: field.ReadOnly, AliasOf: field.AliasOf})
			}
			if err := protocol.CheckExtension(shape, base); err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
			}
		}
		value := reflect.ValueOf(record).Elem()
		base := value.FieldByName(GoName(BaseField))
		if !base.IsValid() || base.String() == "" {
			return nil
		}
		id := value.Field(0).Interface().(platform.Record).ID
		domain, _ := json.Marshal([][]any{{BaseField, "=", base.String()}})
		found, _, err := c.FindOf(typ, platform.Query{Domain: domain})
		if err != nil {
			return nil
		}
		for _, record := range found {
			if other := reflect.ValueOf(record).Field(0).Interface().(platform.Record); other.ID != id && !other.Archived {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{base} already has its {object} record", base.String(), o.Title)
			}
		}
		return nil
	}
}

func objectPublished(value Object) string {
	value.Published = ""
	return string(platform.Raw(value))
}

type ObjectEntityEnvironment struct {
	NormalizeRules       func(*Object) error
	BindActions          func(platform.Caller, *Object) error
	CaptureExtension     func(*Object) error
	CaptureSchema        func(*Object) error
	Check                func(Object, string, platform.RuleEngine) error
	ValidateInstallation func(platform.Caller, Object) error
	Install              func(platform.Caller, Object) *platform.Error
}

func ObjectEntity(env ObjectEntityEnvironment) platform.RecordType {
	return platform.RecordType{Type: ObjectType, Title: "Object type", Plural: "Object types", Model: Object{}, Display: "title",
		Description: "An object this organisation defines itself: its name, what people call it and its fields. Publishing it installs it, and records of it are kept like any other app's.",
		Validate: func(c platform.Caller, record any) *platform.Error {
			if c.Replaying {
				return nil
			}
			object := record.(*Object)
			if err := checkDefaultView(object.View); err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
			if err := env.NormalizeRules(object); err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
			return nil
		},
		Standard: platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{BuilderRole}, Capability: "objects"},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft",
			States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning", Description: "Being defined; nothing is installed yet."},
				{Name: "published", Title: "Published", Tone: "success", Description: "Installed in this tenant: people work with its records."}},
			Actions: []platform.ActionType{{
				Name:        "publish",
				Title:       "Publish",
				Roles:       []string{BuilderRole},
				Capability:  "objects",
				Parameters:  []platform.Field{},
				Description: "Install the object as it stands. Published again, it takes its new fields, and records keep the values of the fields that remain.",
				Criteria:    platform.ActionCriteria{State: &platform.FieldCondition{Field: "state", In: []string{"draft", "published"}}},
				Rules: []platform.ActionRule{{Function: func(c platform.Caller, record any, _ json.RawMessage, now time.Time) *platform.Error {
					var object *Object
					switch value := record.(type) {
					case *Object:
						object = value
					default:
						return &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
					}
					compiled := *object
					if !c.Replaying {
						if err := env.BindActions(c, &compiled); err != nil {
							return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
						}
					}
					object = &compiled
					if !c.Replaying {
						if err := env.CaptureExtension(object); err != nil {
							return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
						}
						if err := env.NormalizeRules(object); err != nil {
							return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
						}
						if err := env.CaptureSchema(object); err != nil {
							return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
						}
					}
					if c.Staging() {
						if err := env.Check(*object, object.ID, c.Rules()); err != nil {
							return &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
						}
						if err := env.ValidateInstallation(c, *object); err != nil {
							return &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
						}
					} else if err := env.Install(c, *object); err != nil {
						return err
					}
					object.Installed = AppID + "." + object.Name
					object.Published = objectPublished(*object)
					if len(object.ActionTypes) > 0 {
						object.Actions = nil
						for i := range object.ActionTypes {
							object.ActionTypes[i].SourceVersion = ""
						}
					}
					*record.(*Object) = *object
					return nil
				}}, {Edit: &platform.PropertyRule{Field: "state", Allowed: []string{"published"}}}},
			}}}}
}
