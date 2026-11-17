package build

import (
	"encoding/json"
	"platformserver/services/ontology"
	"slices"
	"strings"

	"platformserver/platform"
)

func (b *Build) declaredInterfaces() []platform.Interface {
	if b.host == nil {
		return nil
	}
	byName := map[string]platform.Interface{}
	for _, shape := range b.host.Interfaces() {
		byName[shape.Name] = shape
	}
	for _, record := range b.interfaces {
		byName[record.Name] = platform.Interface(record.Definition())
	}
	for ref, asset := range b.jointDeclarations {
		if ref.App != ID || ref.Kind != platform.AssetInterface {
			continue
		}
		var shape platform.Interface
		if json.Unmarshal(asset.Body, &shape) == nil && shape.Name == ref.Name {
			byName[shape.Name] = shape
		}
	}
	out := make([]platform.Interface, 0, len(byName))
	for _, shape := range byName {
		out = append(out, shape)
	}
	slices.SortFunc(out, func(a, b platform.Interface) int { return strings.Compare(a.Name, b.Name) })
	return out
}

var interfaceKind = resourceKind[ontology.InterfaceRecord, ontology.InterfaceDefinition]{
	typ: InterfaceType, prefix: "interface", noun: "interface", title: "Interface", plural: "Interfaces", capability: "interfaces", joint: true,
	description: "A shape several object types share; pages and object sets written against it work for every implementing object type.",
	transition:  "Install this interface shape as its next retained version.",
	immutable:   "A published interface keeps its name and every published field name and type",
	records:     func(b *Build) map[string]ontology.InterfaceRecord { return b.interfaces },
	validate: func(_ *Build, i ontology.InterfaceRecord) (ontology.InterfaceRecord, error) {
		return i, i.Definition().Check()
	},
	identity:        func(a, b ontology.InterfaceRecord) bool { return a.Name == b.Name },
	stable:          func(a, b ontology.InterfaceRecord) bool { return a.KeepsPublishedFields(b) },
	guarantees:      func(a, b ontology.InterfaceRecord) bool { return a.KeepsPublishedFields(b) },
	fromDeclaration: ontology.InterfaceRecordFromDeclaration,
	install: func(h platform.BuildHost, c platform.Caller, i ontology.InterfaceDefinition, version int) error {
		return h.InstallInterface(c, platform.Interface(i), version)
	},
	validateInstall: func(h platform.BuildHost, i ontology.InterfaceDefinition) error {
		return h.ValidateInstallInterface(platform.Interface(i))
	},
}
