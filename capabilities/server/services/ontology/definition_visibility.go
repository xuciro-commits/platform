package ontology

import (
	"maps"
	"slices"

	"platformserver/platform"
)

func ProjectDefinition(def *platform.Definition, entities map[string]platform.EntityInfo, builder bool) bool {
	switch def.Ref.Kind {
	case platform.AssetPropertyType:
		if def.PropertyType == nil {
			return false
		}
		visible := func(version string) bool {
			return builder || boundVersion(entities, def.Ref, version, func(f platform.FieldInfo) *platform.AssetBinding { return f.Property })
		}
		versions, currentVisible := visibleVersions(def.PropertyVersions, def.Version, visible)
		def.PropertyVersions = versions
		if !currentVisible {
			def.PropertyType = nil
			return len(versions) > 0
		}
	case platform.AssetValueType:
		if def.ValueType == nil {
			return false
		}
		visible := func(version string) bool {
			return builder || boundVersion(entities, def.Ref, version, func(f platform.FieldInfo) *platform.AssetBinding { return f.ValueType })
		}
		versions, currentVisible := visibleVersions(def.ValueVersions, def.Version, visible)
		def.ValueVersions = versions
		if !currentVisible {
			def.ValueType = nil
			return len(versions) > 0
		}
	case platform.AssetLinkType:
		if def.LinkType == nil {
			return false
		}
		def.LinkVersions = maps.Clone(def.LinkVersions)
		visible := func(l platform.LinkType) bool {
			parent, pok := entities[l.Parent.Name]
			child, cok := entities[l.Child.Name]
			return pok && cok && l.CheckSchema(parent, child) == nil
		}
		for version, l := range def.LinkVersions {
			if !visible(l) {
				delete(def.LinkVersions, version)
			}
		}
		if !visible(*def.LinkType) {
			def.LinkType = nil
			return len(def.LinkVersions) > 0
		}
	case platform.AssetInterface:
		return def.Interface != nil && (builder || slices.ContainsFunc(slices.Collect(maps.Values(entities)), func(info platform.EntityInfo) bool { return slices.Contains(info.Implements, def.Ref.Name) }))
	}
	return true
}

func boundVersion(entities map[string]platform.EntityInfo, ref platform.AssetRef, version string, pick func(platform.FieldInfo) *platform.AssetBinding) bool {
	for _, entity := range entities {
		for _, field := range entity.Fields {
			if binding := pick(field); binding != nil && binding.Ref == ref && binding.SourceVersion == version {
				return true
			}
		}
	}
	return false
}

func visibleVersions[V any](versions map[string]V, current string, visible func(string) bool) (map[string]V, bool) {
	retained := maps.Clone(versions)
	for version := range retained {
		if !visible(version) {
			delete(retained, version)
		}
	}
	return retained, visible(current)
}
