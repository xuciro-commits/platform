package build

import (
	"platformserver/platform"
	ontology "platformserver/services/ontology"
)

var propertyKind = resourceKind[ontology.PropertyType, platform.PropertyType]{
	typ: PropertyTypeType, prefix: "property", noun: "propertyType", title: "Property type", plural: "Property types", capability: "propertytypes",
	description: "Reusable scalar meaning; object fields keep their own identity and access.",
	transition:  "Install this shared scalar property as its next retained version.",
	immutable:   "A published property keeps its identity and scalar type",
	records:     func(b *Build) map[string]ontology.PropertyType { return b.propertyTypes },
	validate: func(_ *Build, f ontology.PropertyType) (ontology.PropertyType, error) {
		return f, ontology.CheckPropertyType(f)
	},
	identity:        func(a, b ontology.PropertyType) bool { return a.Name == b.Name && a.Type == b.Type },
	fromDeclaration: ontology.PropertyTypeFromDeclaration,
	install: func(h platform.BuildHost, c platform.Caller, d platform.PropertyType, version int) error {
		return h.InstallPropertyType(c, d, version)
	},
	validateInstall: func(h platform.BuildHost, d platform.PropertyType) error { return h.ValidateInstallPropertyType(d) },
}
