package build

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
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
