package platform

import (
	"encoding/json"
	"testing"
)

func TestApplicationRecordPortsAndCandidate(t *testing.T) {
	object := AssetRef{App: "sample", Kind: AssetObject, Name: "sample.note"}
	app := Application{Name: "desk", Title: "Desk", Pages: []string{"notes"}, UIProfile: PageUIProfile(), Variables: map[string]PageVariable{"selected": {Scope: "application", Type: "record", Mode: "resource", Source: &PageResourceSource{Kind: "record", Object: &object}}}}
	page := Page{Name: "notes", Object: object, Layout: "composed", Sections: []Section{{ID: "table", Widget: "table", ConfigVersion: 1, SelectionVariable: "selected"}, {ID: "detail", Widget: "detail", ConfigVersion: 1, RecordVariable: "selected"}}, Document: &PageDocument{FormatVersion: 2, UIProfile: PageUIProfile(), Root: "root", Nodes: map[string]PageLayoutNode{"root": {Kind: "rows", Children: []string{"table", "detail"}}, "table": {Kind: "widget", Section: "table"}, "detail": {Kind: "widget", Section: "detail"}}, Variables: map[string]PageVariable{"selected": {Scope: "application", Type: "record", Mode: "shared", Writable: true, Source: &PageResourceSource{Kind: "application", Variable: "selected", Object: &object}}}}}
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
	oa := ReleaseAsset{Ref: object, SourceVersion: "object-1", ContractVersion: 1, Body: json.RawMessage(`{"type":"sample.note"}`)}
	candidate, err := Candidate([]AssetRef{aa.Ref}, []ReleaseAsset{aa, pa, oa})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ReadCandidate(candidate.ID, candidate.Bytes); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Page){
		func(p *Page) { p.Document.UIProfile = "platform.page.v2.13" },
		func(p *Page) {
			v := p.Document.Variables["selected"]
			v.Writable = false
			p.Document.Variables["selected"] = v
		},
		func(p *Page) { p.Sections[0].Selection = "local" },
		func(p *Page) { p.Sections[0].Widget = "detail" },
	} {
		raw, _ := json.Marshal(page)
		var copy Page
		json.Unmarshal(raw, &copy)
		change(&copy)
		if copy.Document.Check(copy.Sections) == nil {
			t.Fatal("invalid selection output accepted")
		}
	}
	app.Queries = map[string]PageQuery{"bySelection": {Object: object, Limit: 10, For: &PageValue{Variable: "selected"}}}
	if app.CheckVariables() == nil {
		t.Fatal("application record escaped into unsupported query input")
	}
	app.Queries = nil
	legacy := page
	legacy.Document = nil
	if legacy.CheckCollectionPorts() == nil {
		t.Fatal("legacy section accepted shared selection metadata")
	}
	other := object
	other.Name = "sample.other"
	v := page.Document.Variables["selected"]
	v.Source.Object = &other
	page.Document.Variables["selected"] = v
	if app.CheckPageVariables(page) == nil || page.CheckCollectionPorts() == nil {
		t.Fatal("object mismatch accepted")
	}
	app.UIProfile = "platform.page.v2.13"
	if app.CheckVariables() == nil {
		t.Fatal("old application profile accepted record resource")
	}
}
