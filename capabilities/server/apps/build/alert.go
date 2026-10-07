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

const AlertType = "build.alertrule"

// Alert is a rule over a published object (ADR-0077): when a record of it
// comes to match a condition, the people holding a role are told, once per
// record or every time. It is the operations face's "situation": no page has
// to be open for it to be seen.
type Alert struct {
	platform.Record
	Name     string `json:"name" field:"required,search"`
	Title    string `json:"title" field:"required,search"`
	Object   string `json:"object" field:"required" title:"Object" help:"A published object, build.<name>"`
	Field    string `json:"field" field:"required"`
	Operator string `json:"operator" field:"required" choices:"=,!=,<,<=,>,>=,empty,not empty"`
	Value    string `json:"value,omitempty"`
	Message  string `json:"message" field:"required" type:"longtext" help:"What the people told should know or do"`
	Role     string `json:"role,omitempty" title:"Tell" help:"A role of this builder; empty: the builder"`
	Every    bool   `json:"every,omitempty" title:"Every time" help:"Tell again on every change that matches; default once per record"`
	Active   bool   `json:"active" title:"Active"`
}

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

// Matches says whether a record of the alert's object matches it now.
func (a Alert) Matches(record map[string]any, kind string) bool {
	return holds(record[a.Field], kind, a.Operator, a.Value)
}
