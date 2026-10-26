package platform

import (
	"encoding/json"
	"testing"
)

func TestApplicationFilterPortsAndFrozenSchema(t *testing.T) {
	object := AssetRef{App: "sample", Kind: AssetObject, Name: "sample.note"}
	source := PageResourceSource{Kind: "filter", Object: &object, Fields: []string{"active"}}
	app := Application{Name: "desk", Title: "Desk", Pages: []string{"notes"}, UIProfile: PageUIProfile(), Variables: map[string]PageVariable{"filter": {Scope: "application", Type: "filter", Mode: "resource", Source: &source}}}
	page := Page{Name: "notes", Object: object, Layout: "composed", Sections: []Section{{ID: "filter", Widget: "filter", ConfigVersion: 1, Fields: []string{"active"}, FilterVariable: "filter"}, {ID: "table", Widget: "table", ConfigVersion: 1, FilterVariable: "filter"}}, Document: &PageDocument{FormatVersion: 2, UIProfile: PageUIProfile(), Root: "root", Nodes: map[string]PageLayoutNode{"root": {Kind: "rows", Children: []string{"filter", "table"}}, "filter": {Kind: "widget", Section: "filter"}, "table": {Kind: "widget", Section: "table"}}, Variables: map[string]PageVariable{"filter": {Scope: "application", Type: "filter", Mode: "shared", Writable: true, Source: &PageResourceSource{Kind: "application", Variable: "filter", Object: &object}}}}}
	if err := app.CheckVariables(); err != nil {
		t.Fatal(err)
	}
	if err := page.Document.Check(page.Sections); err != nil {
		t.Fatal(err)
	}
	if err := page.CheckCollectionPorts(); err != nil {
		t.Fatal(err)
	}
	if err := app.CheckPageVariables(page); err != nil {
		t.Fatal(err)
	}
	pa, err := PageReleaseAsset("sample", "page-1", page)
	if err != nil {
		t.Fatal(err)
	}
	aa, err := ApplicationReleaseAsset("sample", "app-1", app)
	if err != nil {
		t.Fatal(err)
	}
	oa := ReleaseAsset{Ref: object, SourceVersion: "object-1", ContractVersion: 1, Body: json.RawMessage(`{"type":"sample.note","fields":[{"name":"active","type":"boolean"}]}`)}
	candidate, err := Candidate([]AssetRef{aa.Ref}, []ReleaseAsset{aa, pa, oa})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ReadCandidate(candidate.ID, candidate.Bytes); err != nil {
		t.Fatal(err)
	}
	oa.Body = json.RawMessage(`{"type":"sample.note","fields":[{"name":"active","type":"text"}]}`)
	if _, err = Candidate([]AssetRef{aa.Ref}, []ReleaseAsset{aa, pa, oa}); err == nil {
		t.Fatal("unsupported frozen filter field accepted")
	}
	for _, change := range []func(*Page){
		func(p *Page) { p.Document.UIProfile = "platform.page.v2.14" },
		func(p *Page) {
			v := p.Document.Variables["filter"]
			v.Writable = false
			p.Document.Variables["filter"] = v
		},
		func(p *Page) { p.Sections[1].CollectionVariable = "filter" },
		func(p *Page) { p.Sections[1].Widget = "detail" },
	} {
		raw, _ := json.Marshal(page)
		var copy Page
		json.Unmarshal(raw, &copy)
		change(&copy)
		if copy.Document.Check(copy.Sections) == nil {
			t.Fatal("invalid shared filter port accepted")
		}
	}
	page.Sections[0].Fields = []string{"secret"}
	if app.CheckPageVariables(page) == nil {
		t.Fatal("undeclared filter field accepted")
	}
	app.UIProfile = "platform.page.v2.14"
	if app.CheckVariables() == nil {
		t.Fatal("old application profile accepted filter resource")
	}
	legacy := page
	legacy.Document = nil
	if legacy.CheckCollectionPorts() == nil {
		t.Fatal("legacy section accepted filter metadata")
	}
}
