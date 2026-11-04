package build

import (
	"fmt"
	"reflect"
	"slices"

	"platformserver/platform"
	ontology "platformserver/services/ontology"
)

func (b *Build) checkValueTypeFields(fields []Field) error {
	for _, f := range fields {
		if f.ValueType == nil {
			continue
		}
		if b.host == nil || f.ValueType.Ref.App != ID {
			return fmt.Errorf("value type needs its canonical tenant owner")
		}
		v, ok := b.host.ResolveValueType(*f.ValueType)
		if !ok {
			return fmt.Errorf("value type version is unavailable")
		}
		if err := ontology.CheckValueTypeField(f, v); err != nil {
			return err
		}
	}
	return nil
}

func (b *Build) checkPresentation(o Object) error {
	if err := ontology.CheckDisplay(o); err != nil {
		return err
	}
	if o.Icon == "" && o.Color == "" && ontology.FieldFormats(o.Fields) == nil {
		return nil
	}
	info, err := platform.Describe(ID, Entity(o), func(reflect.Type) string { return "" })
	if err != nil {
		return err
	}
	if b.host == nil {
		return nil
	}
	return b.host.ValidateObjectPresentation(info)
}

var valueKind = resourceKind[ValueType, platform.ValueType]{
	typ: ValueTypeType, prefix: "value", noun: "valueType", title: "Value type", plural: "Value types", capability: "valuetypes",
	description: "A reusable scalar rule set: base type, bounds, pattern and listed values that fields bind to by version.",
	transition:  "Install this value type as its next retained version.",
	immutable:   "A published value type keeps its identity and base type",
	records:     func(b *Build) map[string]ValueType { return b.valueTypes },
	validate:    func(_ *Build, f ValueType) (ValueType, error) { return f, ontology.CheckValueType(f) },
	identity:    func(a, b ValueType) bool { return a.Name == b.Name && a.Base == b.Base },
	fromDeclaration: func(v platform.ValueType) ValueType {
		return ValueType{Name: v.Name, Title: v.Title, Description: v.Description, Base: v.Base, Constraints: v.Constraints, Pattern: v.Pattern, Choices: slices.Clone(v.Choices)}
	},
	install: func(h platform.BuildHost, c platform.Caller, d platform.ValueType, version int) error {
		return h.InstallValueType(c, d, version)
	},
	validateInstall: func(h platform.BuildHost, d platform.ValueType) error { return h.ValidateInstallValueType(d) },
}
