package host

import (
	"encoding/json"
	"slices"
	"testing"

	"platformserver/platform"
	build "platformserver/services/resources/authoring"
)

func TestInvariantC10ObjectSetAndRecordSetReadModels(t *testing.T) {
	live := newInvariantTenant(t, "invariant-c10-object-set")
	shape := map[string]any{
		"name": "build.coded", "title": "Coded", "description": "Coded records.",
		"fields": []platform.InterfaceField{{Name: "code", Type: "text", Title: "Code"}, {Name: "name", Type: "text", Title: "Name"}},
	}
	live.accept(t, build.InterfaceType+".create", build.InterfaceType, "IFACE1", shape)
	live.accept(t, build.InterfaceType+".publish", build.InterfaceType, "IFACE1", map[string]any{})
	object := invariantObject("codedorder", "Coded order")
	object["fields"] = []build.Field{
		{Name: "code", Title: "Code", Type: "text", Search: true},
		{Name: "name", Title: "Name", Type: "text"},
		{Name: "rank", Title: "Rank", Type: "integer"},
	}
	object["implements"] = []string{"build.coded"}
	live.accept(t, build.ObjectType+".create", build.ObjectType, "CODED", object)
	live.accept(t, build.ObjectType+".publish", build.ObjectType, "CODED", map[string]any{})
	for _, row := range []struct {
		id, code, name string
		rank           int
	}{{"C1", "b", "Beta", 1}, {"C2", "a", "Alpha", 2}, {"C3", "c", "Gamma", 3}} {
		live.accept(t, "build.codedorder.create", "build.codedorder", row.id, map[string]any{"code": row.code, "name": row.name, "rank": row.rank})
	}

	holder := invariantObject("codedholder", "Coded holder")
	holder["fields"] = []build.Field{{Name: "item", Title: "Item", Type: "reference", Ref: "build.codedorder"}}
	holder["sets"] = []platform.RecordSet{{
		Name: "chosen", From: "item", Direction: "reference", Object: "build.codedorder",
		Order: []platform.RecordSetSort{{Expr: platform.Expr{Kind: "text", Ref: "code"}}}, Limit: 10,
	}}
	live.accept(t, build.ObjectType+".create", build.ObjectType, "HOLDER", holder)
	live.accept(t, build.ObjectType+".publish", build.ObjectType, "HOLDER", map[string]any{})
	live.accept(t, "build.codedholder.create", "build.codedholder", "H1", map[string]any{"item": "C1"})

	member := live.member(t)
	caller := live.objectCaller(member, "")
	all := platform.ObjectSet{Object: "build.codedorder"}
	page, objectType, refusal := live.loadObjectSet(live.Projection, caller, all, objectWindow{Sort: []string{"-rank"}, Offset: 1, Limit: 1, Bounded: true}, invariantClock)
	if refusal != nil || objectType != "build.codedorder" || page.Total != 3 || len(page.Records) != 1 {
		t.Fatalf("object set window: object=%q total=%d records=%d refusal=%v", objectType, page.Total, len(page.Records), refusal)
	}
	var firstRecord struct {
		ID string `json:"id"`
	}
	raw, _ := json.Marshal(page.Records[0])
	if json.Unmarshal(raw, &firstRecord) != nil || firstRecord.ID != "C2" {
		t.Fatalf("object set sort and offset returned %s", raw)
	}
	filtered := platform.ObjectSet{Object: "build.codedorder", Domain: platform.Raw([]any{[]any{"rank", ">=", 2}})}
	count, refusal := live.countObjectSet(live.Projection, caller, filtered, invariantClock)
	if refusal != nil || count.Count != 2 || count.Partial {
		t.Fatalf("object set count: %+v: %v", count, refusal)
	}
	aggregate, refusal := live.aggregateObjectSet(member, ObjectSetRequest{Set: all, Groups: []string{"code"}, Measures: []string{"sum:rank"}}, invariantClock)
	if refusal != nil || len(aggregate.Rows) != 3 {
		t.Fatalf("object set aggregate: %+v: %v", aggregate, refusal)
	}
	interfacePage, objectType, refusal := live.loadObjectSet(live.Projection, caller, platform.ObjectSet{Interface: "build.coded"}, objectWindow{Sort: []string{"code"}, Limit: 10, Bounded: true}, invariantClock)
	if refusal != nil || objectType != "" || interfacePage.Total != 3 || len(interfacePage.Records) != 3 {
		t.Fatalf("interface object set: object=%q total=%d records=%d refusal=%v", objectType, interfacePage.Total, len(interfacePage.Records), refusal)
	}
	first, ok := interfacePage.Records[0].(InterfaceRecord)
	if !ok || first.Type != "build.codedorder" || first.Record["code"] != "a" {
		t.Fatalf("interface object set first row: %#v", interfacePage.Records[0])
	}

	declared, refusal := caller.ReadRecordSet("build.codedholder", "H1", "chosen", invariantClock)
	if refusal != nil || declared.Object != "build.codedorder" || len(declared.Records) != 1 {
		t.Fatalf("declared record set: %+v: %v", declared, refusal)
	}
	var selected struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(declared.Records[0], &selected) != nil || selected.ID != "C1" {
		t.Fatalf("declared record set row: %s", declared.Records[0])
	}
	if declared.Authorization == nil || !declared.Authorization.Decision.Allow || len(declared.Authorization.Elements) != 1 || !declared.Authorization.Elements[0].Decision.Allow {
		t.Fatalf("declared record set authorization: %+v", declared.Authorization)
	}
	if !slices.Contains(declared.Sources, "build.codedholder/H1") || !slices.Contains(declared.Sources, "build.codedorder/C1") {
		t.Fatalf("declared record set sources: %v", declared.Sources)
	}
}
