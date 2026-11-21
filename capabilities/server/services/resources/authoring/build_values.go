package authoring

import (
	"platformserver/kernel/protocol"
	"reflect"

	"platformserver/kernel/records"
	"platformserver/platform"
	ontology "platformserver/services/ontology"
)

func (b *Build) checkPresentation(o ontology.Object) error {
	if err := ontology.CheckDisplay(o); err != nil {
		return err
	}
	if err := ontology.CheckNaming(o); err != nil {
		return err
	}
	if err := ontology.CheckDefaults(o); err != nil {
		return err
	}
	if o.Icon == "" && o.Color == "" && ontology.FieldFormats(o.Fields) == nil {
		return nil
	}
	info, err := records.Describe(ID, Entity(o), func(reflect.Type) string { return "" })
	if err != nil {
		return err
	}
	if b.host == nil {
		return nil
	}
	return b.host.ValidateObjectPresentation(info)
}

var valueKind = resourceKind[ontology.ValueType, platform.ValueType]{
	typ: ValueTypeType, prefix: "value", noun: "valueType", title: "Value type", plural: "Value types", capability: "valuetypes",
	description:     "A reusable scalar rule set: base type, bounds, pattern and listed values that fields bind to by version.",
	transition:      "Install this value type as its next retained version.",
	immutable:       "A published value type keeps its identity and base type",
	records:         func(b *Build) map[string]ontology.ValueType { return b.valueTypes },
	validate:        func(_ *Build, f ontology.ValueType) (ontology.ValueType, error) { return f, ontology.CheckValueType(f) },
	identity:        func(a, b ontology.ValueType) bool { return a.Name == b.Name && a.Base == b.Base },
	fromDeclaration: ontology.ValueTypeFromDeclaration,
	install: func(h protocol.BuildHost, c platform.Caller, d platform.ValueType, version int) error {
		return h.InstallValueType(c, d, version)
	},
	validateInstall: func(h protocol.BuildHost, d platform.ValueType) error { return h.ValidateInstallValueType(d) },
}
