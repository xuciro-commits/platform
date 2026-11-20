package host

import (
	"slices"
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
	beta["fields"] = append(beta["fields"].([]ontology.Field), ontology.Field{Name: "parent", Title: "Parent", Type: "reference", Ref: "invariantproject.alpha"})
	live.accept(t, "build.object.create", build.ObjectType, "ALPHA", alpha)
	live.accept(t, "build.object.create", build.ObjectType, "BETA", beta)
	live.accept(t, "build.object.publish", build.ObjectType, "ALPHA", map[string]any{})
	live.accept(t, "build.object.publish", build.ObjectType, "BETA", map[string]any{})
	live.accept(t, "build.objecttypegroup.create", build.ObjectTypeGroupType, "GROUP", map[string]any{
		"name": "operations", "title": "Operations", "description": "Operational objects",
		"members": []platform.AssetRef{{App: build.ID, Kind: platform.AssetObject, Name: "invariantproject.alpha"}, {App: build.ID, Kind: platform.AssetObject, Name: "invariantproject.beta"}},
	})
	live.accept(t, "build.objecttypegroup.publish", build.ObjectTypeGroupType, "GROUP", map[string]any{})
	live.accept(t, "build.objecttypegroup.edit", build.ObjectTypeGroupType, "GROUP", map[string]any{"title": "All operations"})
	live.accept(t, "build.objecttypegroup.publish", build.ObjectTypeGroupType, "GROUP", map[string]any{})
	live.accept(t, "build.linktype.create", build.LinkTypeType, "LINK", map[string]any{
		"name": "children", "title": "Children", "description": "Child objects", "parent": "invariantproject.alpha", "child": "invariantproject.beta",
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
		"members": []platform.AssetRef{{App: build.ID, Kind: platform.AssetObject, Name: "invariantproject.alpha"}, {App: build.ID, Kind: platform.AssetObject, Name: "invariantproject.beta"}},
	})
	replayed.accept(t, "build.objecttypegroup.publish", build.ObjectTypeGroupType, "REPLAYGROUP", map[string]any{})
	replayed.accept(t, "build.linktype.create", build.LinkTypeType, "REPLAYLINK", map[string]any{
		"name": "replaychildren", "title": "Replay children", "description": "Replay child objects", "parent": "invariantproject.alpha", "child": "invariantproject.beta",
		"via": "parent", "forward": "children", "reverse": "parent",
	})
	replayed.accept(t, "build.linktype.publish", build.LinkTypeType, "REPLAYLINK", map[string]any{})
}

func TestInvariantK1QueriesPublishBindRejectAndReplay(t *testing.T) {
	live := newInvariantTenant(t, "invariant-query-kinds")
	for _, object := range []struct {
		id      string
		payload map[string]any
	}{
		{id: "ALPHA", payload: invariantObject("alpha", "Alpha")},
		{id: "BETA", payload: invariantObject("beta", "Beta")},
	} {
		live.accept(t, "build.object.create", build.ObjectType, object.id, object.payload)
		live.accept(t, "build.object.publish", build.ObjectType, object.id, map[string]any{})
	}

	live.accept(t, "build.interface.create", build.InterfaceType, "DESCRIBED", map[string]any{
		"name": "build.described", "title": "Described", "description": "Something with a note",
		"fields": []platform.InterfaceField{{Name: "note", Title: "Note", Type: "text"}},
	})
	live.accept(t, "build.interface.publish", build.InterfaceType, "DESCRIBED", map[string]any{})
	gamma := invariantObject("gamma", "Gamma")
	gamma["implements"] = []string{"build.described"}
	live.accept(t, "build.object.create", build.ObjectType, "GAMMA", gamma)
	live.accept(t, "build.object.publish", build.ObjectType, "GAMMA", map[string]any{})

	live.accept(t, "build.query.create", build.QueryType, "ACTIVE", map[string]any{
		"name": "active", "title": "Active", "description": "Active alpha records", "object": "invariantproject.alpha", "sort": []string{"id"}, "limit": 50,
	})
	live.accept(t, "build.query.publish", build.QueryType, "ACTIVE", map[string]any{})
	live.accept(t, "build.query.edit", build.QueryType, "ACTIVE", map[string]any{"object": "invariantproject.beta"})
	if _, refusal := live.Submit(live.member(t), live.submission("build.query.publish", build.QueryType, "ACTIVE", map[string]any{}), invariantClock); refusal == nil {
		t.Fatal("a published query changed its source object")
	}
	live.accept(t, "build.query.edit", build.QueryType, "ACTIVE", map[string]any{"object": "invariantproject.alpha", "title": "Active v2"})
	live.accept(t, "build.query.publish", build.QueryType, "ACTIVE", map[string]any{})

	live.accept(t, "build.query.create", build.QueryType, "DESCRIBEDSET", map[string]any{
		"name": "describedset", "title": "Described set", "description": "Described records", "interface": "build.described", "domain": [][]any{{"note", "=", "ready"}}, "limit": 25,
	})
	live.accept(t, "build.query.publish", build.QueryType, "DESCRIBEDSET", map[string]any{})

	member := live.member(t)
	for _, version := range []string{"", "1.query-1", "1.query-2"} {
		page, refusal := live.queryWindowRead(member, build.ID, "active", "", version, platform.Query{}, invariantClock)
		if refusal != nil || len(page.Records) != 0 {
			t.Fatalf("query version %q: page=%+v refusal=%v", version, page, refusal)
		}
	}
	interfaceQuery, ok := live.namedQuery(build.ID, "describedset")
	if !ok || interfaceQuery.InterfaceShape == nil || !slices.Contains(interfaceQuery.Implementations, "invariantproject.gamma") {
		t.Fatalf("interface query binding: %+v, found=%v", interfaceQuery, ok)
	}

	replayed := live.restore(t, live.entries)
	member = replayed.member(t)
	page, refusal := replayed.queryWindowRead(member, build.ID, "active", "", "1.query-1", platform.Query{}, invariantClock)
	if refusal != nil || len(page.Records) != 0 {
		t.Fatalf("replayed historical query: page=%+v refusal=%v", page, refusal)
	}
	interfaceQuery, ok = replayed.namedQuery(build.ID, "describedset")
	if !ok || interfaceQuery.InterfaceShape == nil || !slices.Contains(interfaceQuery.Implementations, "invariantproject.gamma") {
		t.Fatalf("replayed interface query binding: %+v, found=%v", interfaceQuery, ok)
	}
}
