package platform

import "testing"

func TestInventoryPlansKeepOriginalActiveAndDeclaredBudgets(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	d.Events = nil
	d.Queries = map[string]PageQuery{"read": {Object: p.Object, Limit: 100}, "inventory": {Object: p.Object, Limit: 100}}
	d.Nodes = map[string]PageLayoutNode{"root": {Kind: "rows", Children: []string{"table"}}, "table": {Kind: "widget", Section: "table"}, "unused": {Kind: "widget", Section: "unused"}}
	d.UnusedWidgets = []PageUnusedWidget{{Node: "unused", Parent: "root"}}
	d.Variables = map[string]PageVariable{"set": {Scope: "page", Type: "object-set", Mode: "resource", Source: &PageResourceSource{Kind: "plan", Query: "read"}}, "spare": {Scope: "page", Type: "object-set", Mode: "resource", Source: &PageResourceSource{Kind: "plan", Query: "inventory"}}}
	p.Sections = []Section{{ID: "table", Widget: "table", ConfigVersion: 1, CollectionVariable: "set"}, {ID: "unused", Widget: "table", ConfigVersion: 1, CollectionVariable: "spare"}}
	if active := d.ActiveQueryPlans(p.Sections); len(active) != 1 || !active["read"] {
		t.Fatalf("wrong inventory owners: %v", active)
	}
	if err := d.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	d.UIProfile = "platform.page.v2.96"
	if !d.ActiveQueryPlans(p.Sections)["inventory"] {
		t.Fatal("legacy plan deferred")
	}
	d.UIProfile = PageUIProfile()
	d.Nodes["root"] = PageLayoutNode{Kind: "rows", Children: []string{"table", "unused"}}
	if !d.ActiveQueryPlans(p.Sections)["inventory"] {
		t.Fatal("restored plan omitted")
	}
	d.Nodes["root"] = PageLayoutNode{Kind: "rows", Children: []string{"table"}, VisibleWhen: "spare"}
	if !d.ActiveQueryPlans(p.Sections)["inventory"] {
		t.Fatal("referenced plan omitted")
	}
	d.Nodes["root"] = PageLayoutNode{Kind: "rows", Children: []string{"table"}}
	d.Queries["orphan"] = PageQuery{Object: p.Object, Limit: 100}
	if !d.ActiveQueryPlans(p.Sections)["orphan"] {
		t.Fatal("unowned declaration hidden")
	}
	for i := 0; i < 5; i++ {
		id := string(rune('a' + i))
		d.Queries[id] = PageQuery{Object: p.Object, Limit: 100}
	}
	if d.CheckQueries(p.Sections) == nil {
		t.Fatal("original active row budget bypassed")
	}
}
