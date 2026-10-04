package platform

import "testing"

func collectionBuilderPage() Page {
	p := embeddedTestPage("builder")
	p.Sections = []Section{{ID: "builder", Widget: "collection-builder", ConfigVersion: 1, CollectionVariable: "base", CollectionOutputVariable: "output", CollectionBuilder: &PageCollectionBuilder{Fields: []string{"name", "qty"}}}, {ID: "table", Widget: "table", ConfigVersion: 1, CollectionVariable: "result"}}
	p.Document.Nodes = map[string]PageLayoutNode{"root": {Kind: "rows", Children: []string{"builder", "table"}}, "builder": {Kind: "widget", Section: "builder"}, "table": {Kind: "widget", Section: "table"}}
	p.Document.Queries = map[string]PageQuery{"base": {Object: p.Object, Sort: []string{"id"}, Limit: 2}, "result": {Object: p.Object, Input: "output", Limit: 3}}
	p.Document.Variables = map[string]PageVariable{"base": {Scope: "page", Mode: "resource", Type: "object-set", Source: &PageResourceSource{Kind: "plan", Query: "base"}}, "output": {Scope: "page", Mode: "resource", Type: "object-set", Source: &PageResourceSource{Kind: "query", Section: "builder"}}, "result": {Scope: "page", Mode: "resource", Type: "object-set", Source: &PageResourceSource{Kind: "plan", Query: "result"}}}
	return p
}
func TestCollectionBuilderOriginalOutputRejectsFeedbackAndWrongOwner(t *testing.T) {
	p := collectionBuilderPage()
	if err := p.Document.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	if p.CollectionInputObject("output") != p.Object {
		t.Fatal("builder output lost original object")
	}
	info := EntityInfo{Type: p.Object.Name, Fields: []FieldInfo{{Name: "name", Type: "text"}, {Name: "qty", Type: "decimal"}}}
	if err := p.Sections[0].CheckCollectionBuilderFields(info); err != nil {
		t.Fatal(err)
	}
	info.Fields = info.Fields[:1]
	if p.Sections[0].CheckCollectionBuilderFields(info) == nil {
		t.Fatal("hidden original field accepted")
	}
	for _, change := range []func(*Page){
		func(p *Page) {
			v := p.Document.Variables["output"]
			v.Source.Query = "hidden"
			p.Document.Variables["output"] = v
		},
		func(p *Page) { p.Document.UIProfile = "platform.page.v2.87" },
		func(p *Page) { p.Sections[0].CollectionBuilder.Fields = []string{"qty", "qty"} },
		func(p *Page) {
			v := p.Document.Variables["output"]
			v.Owner = "other"
			p.Document.Variables["output"] = v
		},
		func(p *Page) { q := p.Document.Queries["base"]; q.Input = "output"; p.Document.Queries["base"] = q },
		func(p *Page) { p.Sections[1].CollectionVariable = "output" },
		func(p *Page) { p.Sections[0].CollectionOutputVariable = "base" },
		func(p *Page) {
			q := p.Document.Queries["result"]
			q.Object.Name = "sample.other"
			p.Document.Queries["result"] = q
		},
		func(p *Page) { p.Document.Variables["alias"] = p.Document.Variables["output"] },
	} {
		p := collectionBuilderPage()
		change(&p)
		if p.Document.Check(p.Sections) == nil {
			t.Fatal("invalid collection output accepted")
		}
	}
}
