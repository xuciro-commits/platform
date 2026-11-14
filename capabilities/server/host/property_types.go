package host

import (
	"platformserver/platform"
	"platformserver/services/objects"
	"platformserver/services/ontology"
)

func (h hostView) definitionInstall() ontology.DefinitionInstall {
	return ontology.DefinitionInstall{
		Manifest:    func() platform.Manifest { return h.app.Manifest() },
		Definitions: func() []platform.Definition { return h.t.definitions },
		Describe:    func(d platform.Definition) (platform.Definition, error) { return h.t.describer().DescribeDefinition(d) },
		Store:       h.t.storeDefinition,
		Entity:      h.t.Entity,
		Types: func() []platform.EntityInfo {
			var types []platform.EntityInfo
			for _, et := range h.t.Projection.SortedTypes() {
				types = append(types, et.Info())
			}
			return types
		},
		Bind: func(q platform.NamedQuery) (platform.NamedQuery, error) { return h.t.bindInterfaceQuery(q) },
		Query: func(q platform.NamedQuery, info platform.EntityInfo) error {
			return objects.CheckNamedQuery(q, info, h.t.services.Rules)
		},
		Staged:    unsupportedStagedEffect,
		Link:      func(l platform.LinkType) error { return h.t.Projection.CheckLinkInstall(l) },
		Constrain: func(l platform.LinkType) error { return h.t.Projection.InstallLinkConstraint(l) },
	}
}

func (h hostView) ResolvePropertyType(binding platform.AssetBinding) (platform.PropertyType, bool) {
	if binding.Ref.Kind != platform.AssetPropertyType || binding.SourceVersion == "" {
		return platform.PropertyType{}, false
	}
	for _, d := range h.t.definitions {
		if d.Ref == binding.Ref {
			if selected := d.PropertyVersion(binding.SourceVersion); selected != nil {
				return *selected.PropertyType, true
			}
			break
		}
	}
	return platform.PropertyType{}, false
}

func (h hostView) ValidateInstallPropertyType(p platform.PropertyType) error { return p.Check() }

func (h hostView) InstallPropertyType(c platform.Caller, p platform.PropertyType, version int) error {
	return h.definitionInstall().InstallPropertyType(c, p, version)
}

func (t *Tenant) checkObjectProperties(info platform.EntityInfo) error {
	view := hostView{t: t}
	return ontology.CheckObjectBindings(info, view.ResolvePropertyType, view.ResolveValueType)
}
