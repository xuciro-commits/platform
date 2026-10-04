package platform

import "testing"

func TestRootSelectUpdatesOnlyWritableApplicationPresentationScalars(t *testing.T) {
	p := collectionBuilderPage()
	p.Sections = p.Sections[1:]
	p.Sections[0].ID = "table"
	p.Document.Nodes = map[string]PageLayoutNode{"root": {Kind: "rows", Children: []string{"table"}}, "table": {Kind: "widget", Section: "table"}}
	p.Document.Queries = map[string]PageQuery{"result": {Object: p.Object, Limit: 3}}
	p.Document.Variables = map[string]PageVariable{"result": {Scope: "page", Mode: "resource", Type: "object-set", Source: &PageResourceSource{Kind: "plan", Query: "result"}}, "detail": {Scope: "application", Mode: "shared", Type: "boolean", Writable: true, Source: &PageResourceSource{Kind: "application", Variable: "detail"}}}
	p.Document.Events = []PageEventBinding{{Source: "table", Event: "select", Target: "detail", Value: Raw(true)}}
	if err := p.Document.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	for _, profile := range []string{"platform.page.v2.88", "platform.page.v2.33"} {
		old := p.Document.UIProfile
		p.Document.UIProfile = profile
		if p.Document.Check(p.Sections) == nil {
			t.Fatal("legacy shared selection event accepted")
		}
		p.Document.UIProfile = old
	}
	v := p.Document.Variables["detail"]
	v.Writable = false
	p.Document.Variables["detail"] = v
	if p.Document.Check(p.Sections) == nil {
		t.Fatal("read-only state accepted")
	}
	v.Writable = true
	p.Document.Variables["detail"] = v
	p.Document.Events[0].Value = Raw("wrong")
	if p.Document.Check(p.Sections) == nil {
		t.Fatal("wrong literal accepted")
	}
	p.Document.Events[0].Value = Raw(true)
	p.Document.Nodes["root"] = PageLayoutNode{Kind: "rows"}
	if p.Document.rootPresentationSource("table") {
		t.Fatal("non-root widget accepted")
	}
	p.Document.Nodes["root"] = PageLayoutNode{Kind: "loop", Children: []string{"table"}}
	if p.Document.rootPresentationSource("table") {
		t.Fatal("loop source accepted")
	}
}
