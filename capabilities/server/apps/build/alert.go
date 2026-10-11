package build

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func (b *Build) alertEntity() platform.Entity {
	return platform.Entity{Type: AlertType, Title: "Alert rule", Plural: "Alert rules", Model: Alert{}, Display: "title",
		Description: "Tells a role when a record of an object comes to match a condition.",
		Scope:       platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant}},
		Standard:    platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder}},
		Validate: func(c platform.Caller, record any) *kernel.Error {
			a := record.(*Alert)
			if err := b.checkAlert(*a); err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
			}
			return nil
		}}
}

func (b *Build) checkAlert(a Alert) error {
	if !named(a.Name) {
		return fmt.Errorf("The alert name %s must be lower-case letters and digits", a.Name)
	}
	e, ok := b.installed[a.Object]
	if !ok {
		return fmt.Errorf("%s is not a published object of this builder", a.Object)
	}
	info, err := platform.Describe(ID, e, func(reflect.Type) string { return "" })
	if err != nil {
		return err
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
