package build

import (
	"fmt"

	"platformserver/platform"
	ontology "platformserver/services/ontology"
)

func (b *Build) checkPropertyFields(fields []Field) error {
	for _, f := range fields {
		if f.Property == nil {
			continue
		}
		if b.host == nil || f.Property.Ref.App != ID {
			return fmt.Errorf("shared property needs its canonical tenant owner")
		}
		p, ok := b.host.ResolvePropertyType(*f.Property)
		if !ok {
			return fmt.Errorf("shared property version is unavailable")
		}
		if err := ontology.CheckPropertyField(f, p); err != nil {
			return err
		}
	}
	return nil
}

var propertyKind = resourceKind[PropertyType, platform.PropertyType]{
	typ: PropertyTypeType, prefix: "property", noun: "propertyType", title: "Property type", plural: "Property types", capability: "propertytypes",
	description: "Reusable scalar meaning; object fields keep their own identity and access.",
	transition:  "Install this shared scalar property as its next retained version.",
	immutable:   "A published property keeps its identity and scalar type",
	records:     func(b *Build) map[string]PropertyType { return b.propertyTypes },
	validate:    func(_ *Build, f PropertyType) (PropertyType, error) { return f, ontology.CheckPropertyType(f) },
	identity:    func(a, b PropertyType) bool { return a.Name == b.Name && a.Type == b.Type },
	fromDeclaration: func(p platform.PropertyType) PropertyType {
		return PropertyType{Name: p.Name, Title: p.Title, Description: p.Description, Type: p.Type}
	},
	install: func(h platform.BuildHost, c platform.Caller, d platform.PropertyType, version int) error {
		return h.InstallPropertyType(c, d, version)
	},
	validateInstall: func(h platform.BuildHost, d platform.PropertyType) error { return h.ValidateInstallPropertyType(d) },
}
