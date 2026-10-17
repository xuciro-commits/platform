package build

import (
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"reflect"
	"slices"
	"strings"
	"time"
)

const SchemaAlertPublish = AlertType + ".publish"

func (b *Build) alertEntity() platform.Entity {
	return platform.Entity{Type: AlertType, Title: "Alert rule", Plural: "Alert rules", Model: Alert{}, Display: "title",
		Description: "Tells a role when a record of an object comes to match a condition.",
		Scope:       platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant}},
		Standard:    platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder}},
		Validate: func(c platform.Caller, record any) *kernel.Error {
			a := record.(*Alert)
			if c.Replaying {
				return nil
			}
			if previous, ok := platform.Get[Alert](c, a.ID); ok {
				if legacyAlert(previous) {
					if err := freezeLegacyAlert(&previous); err != nil {
						return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
					}
					a.State, a.Version, a.DefinitionVersion, a.Versions, a.Published = previous.State, previous.Version, previous.DefinitionVersion, previous.Versions, previous.Published
				}
				if previous.Published != "" && previous.Name != a.Name {
					return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A published alert keeps its name")
				}
			}
			if err := b.checkAlert(*a); err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
			}
			return nil
		},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft", States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning"}, {Name: "published", Title: "Published", Tone: "success"}},
			Transitions: []platform.Transition{{Name: "publish", Title: "Publish", From: []string{"draft", "published"}, To: []string{"published"}, Roles: []string{Builder}, Payload: []platform.Field{}, Do: b.publishAlert}}}}
}

func (b *Build) checkAlert(a Alert) error {
	if !named(a.Name) {
		return fmt.Errorf("The alert name %s must be lower-case letters and digits", a.Name)
	}
	info, ok := b.lookupEntity(a.Object)
	if !ok {
		objects, err := readDefinitionInventory[Object](b.host.Automation(platform.Caller{}, ID))
		if err != nil {
			return err
		}
		for _, object := range objects {
			if !object.Archived && "build."+object.Name == a.Object {
				var err error
				info, err = platform.Describe(ID, Entity(object), func(reflect.Type) string { return "" })
				ok = err == nil
				break
			}
		}
	}
	if !ok {
		return fmt.Errorf("%s is not a published object of this builder", a.Object)
	}
	if _, ok := info.Field(a.Field); !ok {
		return fmt.Errorf("%s has no field %s", a.Object, a.Field)
	}
	if !slices.Contains(Operators, a.Operator) {
		return fmt.Errorf("%q is not an operator", a.Operator)
	}
	if a.Role != "" && !slices.Contains(b.Manifest().AllRoles(), a.Role) {
		return fmt.Errorf("The role %s is not a role of this builder", a.Role)
	}
	if strings.TrimSpace(a.Message) == "" {
		return fmt.Errorf("The alert needs a message")
	}
	return nil
}

func (b *Build) publishAlert(_ platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	a := record.(*Alert)
	if err := b.checkAlert(*a); err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	if err := freezeLegacyAlert(a); err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	asset, err := alertReleaseAsset(*a, a.Version+1)
	if err == nil {
		err = retainAlertAsset(a, asset)
	}
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	return nil
}

const AlertType = "build.alertrule"

// Alert is a rule over a published object (ADR-0077): when a record of it
// comes to match a condition, the people holding a role are told, once per
// record or every time. It is the operations face's "situation": no page has
// to be open for it to be seen.
type Alert struct {
	platform.Record
	Name              string   `json:"name" field:"required,search"`
	Title             string   `json:"title" field:"required,search"`
	Object            string   `json:"object" field:"required" title:"Object" help:"A published object, build.<name>"`
	Field             string   `json:"field" field:"required"`
	Operator          string   `json:"operator" field:"required" choices:"=,!=,<,<=,>,>=,empty,not empty"`
	Value             string   `json:"value,omitempty"`
	Message           string   `json:"message" field:"required" type:"longtext" help:"What the people told should know or do"`
	Role              string   `json:"role,omitempty" title:"Tell" help:"A role of this builder; empty: the builder"`
	Every             bool     `json:"every,omitempty" title:"Every time" help:"Tell again on every change that matches; default once per record"`
	Active            bool     `json:"active" title:"Active"`
	State             string   `json:"state,omitempty" field:"readonly"`
	Version           int      `json:"version,omitempty" field:"readonly"`
	DefinitionVersion int      `json:"definitionVersion,omitempty" field:"readonly"`
	Versions          []string `json:"versions,omitempty" field:"readonly" type:"json"`
	Published         string   `json:"published,omitempty" field:"readonly" type:"json"`
}

func (a Alert) Definition() platform.AlertRule {
	return platform.AlertRule{Name: a.Name, Title: a.Title, Object: platform.AssetRef{App: ID, Kind: platform.AssetObject, Name: a.Object}, Field: a.Field, Operator: a.Operator, Value: a.Value, Message: a.Message, Role: a.Role, Every: a.Every, Active: a.Active}
}

// Matches says whether a record of the alert's object matches it now.
func (a Alert) Matches(record map[string]any, kind string) bool {
	return ValueHolds(record[a.Field], kind, a.Operator, a.Value)
}
