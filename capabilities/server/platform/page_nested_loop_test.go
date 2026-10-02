package platform

import (
	"encoding/json"
	"testing"
)

func nestedPage() Page {
	parent := AssetRef{App: "sample", Kind: AssetObject, Name: "sample.parent"}
	child := AssetRef{App: "sample", Kind: AssetObject, Name: "sample.child"}
	return Page{Object: parent, Sections: []Section{{ID: "parentDetail", Widget: "detail", ConfigVersion: 1, Fields: []string{"name"}, RecordVariable: "parentItem"}, {ID: "childDetail", Widget: "detail", ConfigVersion: 1, Fields: []string{"name"}, RecordVariable: "childItem"}}, Document: &PageDocument{FormatVersion: 2, UIProfile: PageUIProfile(), Root: "root", Nodes: map[string]PageLayoutNode{
		"root": {Kind: "rows", Children: []string{"parents"}}, "parents": {Kind: "loop", Children: []string{"parentDetail", "children"}, Loop: &PageLoop{Collection: "parentWindow", ItemVariable: "parentItem", Limit: 2}}, "parentDetail": {Kind: "widget", Section: "parentDetail"}, "children": {Kind: "loop", Children: []string{"childDetail"}, Loop: &PageLoop{Collection: "childWindow", ItemVariable: "childItem", Limit: 3}}, "childDetail": {Kind: "widget", Section: "childDetail"}}, Variables: map[string]PageVariable{
		"parentWindow": {Scope: "page", Type: "object-set", Mode: "resource", Source: &PageResourceSource{Kind: "plan", Query: "parents"}}, "parentItem": {Scope: "loop-item", Owner: "parents", Type: "record", Mode: "resource", Source: &PageResourceSource{Kind: "item", Node: "parents"}}, "childWindow": {Scope: "loop-item", Owner: "parents", Type: "object-set", Mode: "resource", Source: &PageResourceSource{Kind: "plan", Query: "children"}}, "childItem": {Scope: "loop-item", Owner: "children", Type: "record", Mode: "resource", Source: &PageResourceSource{Kind: "item", Node: "children"}}}, Queries: map[string]PageQuery{
		"parents": {Object: parent, Limit: 2}, "children": {ItemOwner: "parents", Object: child, Limit: 3, Query: &AssetBinding{Ref: AssetRef{App: "sample", Kind: AssetQuery, Name: "children"}, SourceVersion: "q1"}, For: &PageValue{Variable: "parentItem"}}}}}
}

func TestNestedLoopUsesTypedParentAndExpandedBudgets(t *testing.T) {
	p := nestedPage()
	if err := p.Document.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	q := p.Document.Queries["children"]
	named := Definition{Ref: q.Query.Ref, Version: "q1", Query: &NamedQuery{Object: q.Object.Name, By: "parent"}}
	info := EntityInfo{Type: q.Object.Name, Fields: []FieldInfo{{Name: "parent", Type: "reference", Ref: p.Object.Name}}}
	if err := p.CheckQuerySchema(q, info, &named); err != nil {
		t.Fatal(err)
	}
	info.Fields[0].Ref = "sample.unrelated"
	if p.CheckQuerySchema(q, info, &named) == nil {
		t.Fatal("incompatible parent object accepted")
	}
	info.Fields[0].Ref = p.Object.Name
	named.Query.By = ""
	if p.CheckQuerySchema(q, info, &named) == nil {
		t.Fatal("parent query lost its reference boundary")
	}
	for _, change := range []func(*PageDocument){
		func(d *PageDocument) { d.UIProfile = "platform.page.v2.19" },
		func(d *PageDocument) {
			n := d.Nodes["children"]
			n.Children = []string{"third"}
			d.Nodes["children"] = n
			d.Nodes["third"] = PageLayoutNode{Kind: "loop", Children: []string{"childDetail"}, Loop: &PageLoop{Collection: "thirdWindow", ItemVariable: "thirdItem", Limit: 1}}
			d.Variables["thirdWindow"] = PageVariable{Scope: "loop-item", Owner: "children", Type: "object-set", Mode: "resource", Source: &PageResourceSource{Kind: "plan", Query: "third"}}
			d.Variables["thirdItem"] = PageVariable{Scope: "loop-item", Owner: "third", Type: "record", Mode: "resource", Source: &PageResourceSource{Kind: "item", Node: "third"}}
			third := d.Queries["children"]
			third.ItemOwner = "children"
			third.For = &PageValue{Variable: "childItem"}
			third.Limit = 1
			d.Queries["third"] = third
		},
		func(d *PageDocument) {
			q := d.Queries["children"]
			q.For = &PageValue{Literal: Raw("arbitrary")}
			d.Queries["children"] = q
		},
		func(d *PageDocument) { q := d.Queries["children"]; q.ItemOwner = "children"; d.Queries["children"] = q },
		func(d *PageDocument) { n := d.Nodes["parents"]; n.Loop.Limit = 100; d.Nodes["parents"] = n },
		func(d *PageDocument) {
			q := d.Queries["children"]
			q.Limit = 100
			d.Queries["children"] = q
			n := d.Nodes["parents"]
			n.Loop.Limit = 6
			d.Nodes["parents"] = n
		},
	} {
		raw, _ := json.Marshal(p.Document)
		var d PageDocument
		json.Unmarshal(raw, &d)
		change(&d)
		if d.Check(p.Sections) == nil {
			t.Fatal("unsafe nested document accepted")
		}
	}
	delete(p.Document.Queries, "parents")
	shown := p.Document.Visible(p.Sections)
	if _, ok := shown.Queries["children"]; ok {
		t.Fatal("child query survived missing parent source")
	}
}
