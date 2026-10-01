package platform

import (
	"encoding/json"
	"testing"
)

func applicationQueryFixture() (Application, Page) {
	object := AssetRef{App: "build", Kind: AssetObject, Name: "build.note"}
	app := Application{Name: "notes", Title: "Notes", UIProfile: PageUIProfile(), Pages: []string{"desk"}, Variables: map[string]PageVariable{
		"bucket": {Scope: "application", Type: "string", Mode: "state", Initial: json.RawMessage(`"A"`)}, "window": {Scope: "application", Type: "object-set", Mode: "resource", Source: &PageResourceSource{Kind: "plan", Query: "read"}}}, Queries: map[string]PageQuery{"read": {Object: object, Limit: 10, Conditions: []PageQueryCondition{{Field: "bucket", Op: "=", Value: PageValue{Variable: "bucket"}}}}}}
	page := Page{Name: "desk", Title: "Desk", Layout: "composed", Object: object, Sections: []Section{{ID: "table", Widget: "table", ConfigVersion: 1, CollectionVariable: "shared"}, {ID: "detail", Widget: "detail", ConfigVersion: 1, RecordVariable: "item"}}, Document: &PageDocument{FormatVersion: 2, UIProfile: PageUIProfile(), Root: "root", Nodes: map[string]PageLayoutNode{"root": {Kind: "rows", Children: []string{"table", "cards"}}, "table": {Kind: "widget", Section: "table"}, "cards": {Kind: "loop", Children: []string{"detail"}, Loop: &PageLoop{Collection: "shared", ItemVariable: "item", Limit: 10}}, "detail": {Kind: "widget", Section: "detail"}}, Variables: map[string]PageVariable{"shared": {Scope: "application", Type: "object-set", Mode: "shared", Source: &PageResourceSource{Kind: "application", Variable: "window", Object: &object}}, "item": {Scope: "loop-item", Owner: "cards", Type: "record", Mode: "resource", Source: &PageResourceSource{Kind: "item", Node: "cards"}}}}}
	return app, page
}
func TestApplicationQueryContractAndSharedObjectRequirements(t *testing.T) {
	app, page := applicationQueryFixture()
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
	if page.RecordVariableObject("item") != "build.note" {
		t.Fatal("shared loop object was lost")
	}
	for _, tc := range []struct {
		name   string
		change func(*Application, *Page)
	}{
		{"old profile", func(a *Application, _ *Page) { a.UIProfile = "platform.page.v2.11" }},
		{"query has overlay owner", func(a *Application, _ *Page) { q := a.Queries["read"]; q.Owner = "picker"; a.Queries["read"] = q }},
		{"query reads page state", func(a *Application, _ *Page) { v := a.Variables["bucket"]; v.Scope = "page"; a.Variables["bucket"] = v }},
		{"query depends on result", func(a *Application, _ *Page) {
			a.Variables["flag"] = PageVariable{Scope: "application", Type: "boolean", Mode: "derived", Expression: &PageExpression{Op: "present", Args: []PageValue{{Variable: "window"}}}}
			q := a.Queries["read"]
			q.Conditions[0].Value = PageValue{Variable: "flag"}
			a.Queries["read"] = q
		}},
		{"shared window writable", func(_ *Application, p *Page) {
			v := p.Document.Variables["shared"]
			v.Writable = true
			p.Document.Variables["shared"] = v
		}},
		{"shared object missing", func(_ *Application, p *Page) {
			v := p.Document.Variables["shared"]
			v.Source.Object = nil
			p.Document.Variables["shared"] = v
		}},
		{"shared object wrong", func(_ *Application, p *Page) {
			v := p.Document.Variables["shared"]
			v.Source.Object = &AssetRef{App: "build", Kind: AssetObject, Name: "build.other"}
			p.Document.Variables["shared"] = v
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, p := applicationQueryFixture()
			tc.change(&a, &p)
			if a.CheckVariables() == nil && p.Document.Check(p.Sections) == nil && p.CheckCollectionPorts() == nil && a.CheckPageVariables(p) == nil {
				t.Fatal("invalid shared resource accepted")
			}
		})
	}
	deps := app.Dependencies("build")
	found := false
	for _, d := range deps {
		if d == app.Queries["read"].Object {
			found = true
		}
	}
	if !found {
		t.Fatal("application query object not in closure")
	}
	// A candidate cannot substitute a different retained query version.
	named := NamedQuery{Name: "fixed", Title: "Fixed", Object: "build.note", Limit: 10}
	queryRef := AssetRef{App: "build", Kind: AssetQuery, Name: named.Name}
	q := app.Queries["read"]
	q.Query = &AssetBinding{Ref: queryRef, SourceVersion: "q1"}
	app.Queries["read"] = q
	appAsset, err := ApplicationReleaseAsset("build", "1", app)
	if err != nil {
		t.Fatal(err)
	}
	pageAsset, err := PageReleaseAsset("build", "1", page)
	if err != nil {
		t.Fatal(err)
	}
	objectBody, _ := json.Marshal(EntityInfo{Type: "build.note", Fields: []FieldInfo{{Name: "bucket", Type: "text"}}})
	queryBody, _ := json.Marshal(named)
	assets := []ReleaseAsset{appAsset, pageAsset, {Ref: app.Queries["read"].Object, ContractVersion: 1, SourceVersion: "1", Body: objectBody}, {Ref: queryRef, ContractVersion: 1, SourceVersion: "q2", Requires: []AssetRef{app.Queries["read"].Object}, Body: queryBody}}
	if _, err := Candidate([]AssetRef{appAsset.Ref}, assets); err == nil {
		t.Fatal("application pinned query version ignored")
	}
	assets[len(assets)-1].SourceVersion = "q1"
	if _, err := Candidate([]AssetRef{appAsset.Ref}, assets); err != nil {
		t.Fatal("valid application query candidate failed", err)
	}
}
