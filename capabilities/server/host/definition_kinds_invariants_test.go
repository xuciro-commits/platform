package host

import (
	"testing"

	"platformserver/platform"
	"platformserver/services/ontology"
	build "platformserver/services/resources/authoring"
)

func TestInvariantK1OntologyDefinitionFamiliesInstallAndReplay(t *testing.T) {
	live := newInvariantTenant(t, "invariant-definition-kinds")
	live.accept(t, "build.propertytype.create", build.PropertyTypeType, "PROPERTY", map[string]any{
		"name": "priority", "title": "Priority", "description": "Reusable priority", "type": "integer",
	})
	live.accept(t, "build.propertytype.publish", build.PropertyTypeType, "PROPERTY", map[string]any{})
	live.accept(t, "build.propertytype.edit", build.PropertyTypeType, "PROPERTY", map[string]any{"title": "Priority score"})
	live.accept(t, "build.propertytype.publish", build.PropertyTypeType, "PROPERTY", map[string]any{})
	live.accept(t, "build.valuetype.create", build.ValueTypeType, "VALUE", map[string]any{
		"name": "score", "title": "Score", "description": "Bounded score", "base": "integer",
	})
	live.accept(t, "build.valuetype.publish", build.ValueTypeType, "VALUE", map[string]any{})
	live.accept(t, "build.valuetype.edit", build.ValueTypeType, "VALUE", map[string]any{"title": "Bounded score"})
	live.accept(t, "build.valuetype.publish", build.ValueTypeType, "VALUE", map[string]any{})
	live.accept(t, "build.interface.create", build.InterfaceType, "INTERFACE", map[string]any{
		"name": "build.described", "title": "Described", "description": "Something with a note",
		"fields": []platform.InterfaceField{{Name: "note", Title: "Note", Type: "text"}},
	})
	live.accept(t, "build.interface.publish", build.InterfaceType, "INTERFACE", map[string]any{})
	live.accept(t, "build.interface.edit", build.InterfaceType, "INTERFACE", map[string]any{"title": "Described v2"})
	live.accept(t, "build.interface.publish", build.InterfaceType, "INTERFACE", map[string]any{})
	bound := func(name string) map[string]any {
		object := invariantObject(name, "Bound")
		object["fields"] = []ontology.Field{
			{Name: "note", Title: "Note", Type: "text"},
			{Name: "priority", Title: "Priority score", Type: "integer", Property: &platform.AssetBinding{Ref: platform.AssetRef{App: build.ID, Kind: platform.AssetPropertyType, Name: "priority"}, SourceVersion: "1.property-2"}},
			{Name: "score", Title: "Bounded score", Type: "integer", ValueType: &platform.AssetBinding{Ref: platform.AssetRef{App: build.ID, Kind: platform.AssetValueType, Name: "score"}, SourceVersion: "1.value-2"}},
		}
		object["implements"] = []string{"build.described"}
		return object
	}
	live.accept(t, "build.object.create", build.ObjectType, "BOUND", bound("bound"))
	live.accept(t, "build.object.publish", build.ObjectType, "BOUND", map[string]any{})

	alpha := invariantObject("alpha", "Alpha")
	beta := invariantObject("beta", "Beta")
	beta["fields"] = append(beta["fields"].([]ontology.Field), ontology.Field{Name: "parent", Title: "Parent", Type: "reference", Ref: "build.alpha"})
	live.accept(t, "build.object.create", build.ObjectType, "ALPHA", alpha)
	live.accept(t, "build.object.create", build.ObjectType, "BETA", beta)
	live.accept(t, "build.object.publish", build.ObjectType, "ALPHA", map[string]any{})
	live.accept(t, "build.object.publish", build.ObjectType, "BETA", map[string]any{})
	live.accept(t, "build.objecttypegroup.create", build.ObjectTypeGroupType, "GROUP", map[string]any{
		"name": "operations", "title": "Operations", "description": "Operational objects",
		"members": []platform.AssetRef{{App: build.ID, Kind: platform.AssetObject, Name: "build.alpha"}, {App: build.ID, Kind: platform.AssetObject, Name: "build.beta"}},
	})
	live.accept(t, "build.objecttypegroup.publish", build.ObjectTypeGroupType, "GROUP", map[string]any{})
	live.accept(t, "build.objecttypegroup.edit", build.ObjectTypeGroupType, "GROUP", map[string]any{"title": "All operations"})
	live.accept(t, "build.objecttypegroup.publish", build.ObjectTypeGroupType, "GROUP", map[string]any{})
	live.accept(t, "build.linktype.create", build.LinkTypeType, "LINK", map[string]any{
		"name": "children", "title": "Children", "description": "Child objects", "parent": "build.alpha", "child": "build.beta",
		"via": "parent", "forward": "children", "reverse": "parent",
	})
	live.accept(t, "build.linktype.publish", build.LinkTypeType, "LINK", map[string]any{})
	live.accept(t, "build.linktype.edit", build.LinkTypeType, "LINK", map[string]any{"title": "Child objects v2"})
	live.accept(t, "build.linktype.publish", build.LinkTypeType, "LINK", map[string]any{})

	replayed := live.restore(t, live.entries)
	replayed.accept(t, "build.object.create", build.ObjectType, "REPLAYBOUND", bound("replaybound"))
	replayed.accept(t, "build.object.publish", build.ObjectType, "REPLAYBOUND", map[string]any{})
	replayed.accept(t, "build.objecttypegroup.create", build.ObjectTypeGroupType, "REPLAYGROUP", map[string]any{
		"name": "replayops", "title": "Replay operations", "description": "Replay operational objects",
		"members": []platform.AssetRef{{App: build.ID, Kind: platform.AssetObject, Name: "build.alpha"}, {App: build.ID, Kind: platform.AssetObject, Name: "build.beta"}},
	})
	replayed.accept(t, "build.objecttypegroup.publish", build.ObjectTypeGroupType, "REPLAYGROUP", map[string]any{})
	replayed.accept(t, "build.linktype.create", build.LinkTypeType, "REPLAYLINK", map[string]any{
		"name": "replaychildren", "title": "Replay children", "description": "Replay child objects", "parent": "build.alpha", "child": "build.beta",
		"via": "parent", "forward": "children", "reverse": "parent",
	})
	replayed.accept(t, "build.linktype.publish", build.LinkTypeType, "REPLAYLINK", map[string]any{})
}
