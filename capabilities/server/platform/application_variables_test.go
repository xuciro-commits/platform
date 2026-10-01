package platform

import (
	"encoding/json"
	"testing"
)

func sharedApplicationPage() Page {
	return Page{Name: "shared", Object: AssetRef{App: "sample", Kind: AssetObject, Name: "sample.note"}, Layout: "composed", Sections: []Section{{ID: "input", Widget: "input", ConfigVersion: 1}}, Document: &PageDocument{FormatVersion: 2, UIProfile: PageUIProfile(), Root: "root", Nodes: map[string]PageLayoutNode{"root": {Kind: "rows", Children: []string{"input"}}, "input": {Kind: "widget", Section: "input", ValueVariable: "alias"}}, Variables: map[string]PageVariable{"alias": {Scope: "application", Type: "string", Mode: "shared", Writable: true, Source: &PageResourceSource{Kind: "application", Variable: "draft"}}}}}
}

func TestApplicationDeclarationsAndSharedRequirements(t *testing.T) {
	page := sharedApplicationPage()
	app := Application{Name: "desk", Title: "Desk", Pages: []string{page.Name}, UIProfile: PageUIProfile(), Variables: map[string]PageVariable{"draft": {Scope: "application", Type: "string", Mode: "state", Initial: json.RawMessage(`"initial"`)}}}
	if app.CheckVariables() != nil || app.CheckPageVariables(page) != nil || page.Document.Check(page.Sections) != nil {
		t.Fatal("valid application binding rejected")
	}
	for _, change := range []func(*Application){
		func(a *Application) { a.UIProfile = "platform.page.v2.7" },
		func(a *Application) { v := a.Variables["draft"]; v.Scope = "page"; a.Variables["draft"] = v },
		func(a *Application) { v := a.Variables["draft"]; v.Mode = "shared"; a.Variables["draft"] = v },
	} {
		copied := app
		copied.Variables = map[string]PageVariable{"draft": app.Variables["draft"]}
		change(&copied)
		if copied.CheckVariables() == nil {
			t.Fatal("invalid application declaration accepted")
		}
	}
	for _, mode := range []string{"constant", "missing", "wrong-type"} {
		copied := app
		copied.Variables = map[string]PageVariable{"draft": app.Variables["draft"]}
		v := copied.Variables["draft"]
		v.Mode = "constant"
		if mode == "missing" {
			delete(copied.Variables, "draft")
		} else {
			if mode == "wrong-type" {
				v.Type = "boolean"
			}
			copied.Variables["draft"] = v
		}
		if copied.CheckPageVariables(page) == nil {
			t.Fatal("unsatisfied shared requirement accepted")
		}
	}
	pageAsset, err := PageReleaseAsset("sample", "page-1", page)
	if err != nil {
		t.Fatal(err)
	}
	appAsset, err := ApplicationReleaseAsset("sample", "app-1", app)
	if err != nil {
		t.Fatal(err)
	}
	object := ReleaseAsset{Ref: page.Object, SourceVersion: "object-1", ContractVersion: 1, Body: json.RawMessage(`{"type":"sample.note"}`)}
	candidate, err := Candidate([]AssetRef{appAsset.Ref}, []ReleaseAsset{appAsset, pageAsset, object})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ReadCandidate(candidate.ID, candidate.Bytes); err != nil {
		t.Fatal(err)
	}
	// The application candidate closes both the single declaration owner and page requirements.
	app.Variables = map[string]PageVariable{}
	appAsset.Body, _ = json.Marshal(app)
	if _, err = Candidate([]AssetRef{appAsset.Ref}, []ReleaseAsset{appAsset, pageAsset, object}); err == nil {
		t.Fatal("candidate accepted missing shared variable")
	}
}
