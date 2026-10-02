package platform

import (
	"strings"
	"testing"
)

func TestTitlesRequirePlainTextAndMatchingScopedCompleteCount(t *testing.T) {
	d := &PageDocument{FormatVersion: 2, UIProfile: PageUIProfile(), Root: "root", Nodes: map[string]PageLayoutNode{"root": {Kind: "rows", Children: []string{"heading", "title"}}, "heading": {Kind: "widget", Section: "heading"}, "title": {Kind: "widget", Section: "title"}}, Queries: map[string]PageQuery{"read": {Object: AssetRef{App: "sample", Kind: AssetObject, Name: "sample.item"}, Limit: 1}}, Variables: map[string]PageVariable{"window": {Scope: "page", Type: "object-set", Mode: "resource", Source: &PageResourceSource{Kind: "plan", Query: "read"}}, "count": {Scope: "page", Type: "decimal", Mode: "aggregate", Source: &PageResourceSource{Kind: "count", Query: "read"}}}}
	sections := []Section{{ID: "heading", Widget: "heading", ConfigVersion: 1, HeadingLevel: "h2", Text: "<b>Literal heading</b>"}, {ID: "title", Widget: "collection-title", ConfigVersion: 1, CollectionVariable: "window", CountVariable: "count", Title: "Permitted items"}}
	if err := d.Check(sections); err != nil {
		t.Fatal(err)
	}
	if (*PageDocument)(nil).Check(sections) == nil {
		t.Fatal("titles without document accepted")
	}
	for _, change := range []func([]Section){func(s []Section) { s[0].HeadingLevel = "h4" }, func(s []Section) { s[0].Text = "" }, func(s []Section) { s[0].Text = strings.Repeat("x", 4097) }, func(s []Section) { s[1].CountVariable = "" }, func(s []Section) { s[0].CountVariable = "count" }} {
		copy := append([]Section(nil), sections...)
		change(copy)
		if d.Check(copy) == nil {
			t.Fatal("invalid title configuration accepted")
		}
	}
	d.UIProfile = "platform.page.v2.40"
	if d.Check(sections) == nil {
		t.Fatal("old profile accepted titles")
	}
	d.UIProfile = PageUIProfile()
	count := d.Variables["count"]
	count.Mode = "state"
	count.Source = nil
	count.Initial = Raw(map[string]any{"kind": "decimal", "value": "999"})
	d.Variables["count"] = count
	if d.Check(sections) == nil {
		t.Fatal("writable fake count accepted")
	}
	count = PageVariable{Scope: "overlay", Owner: "other", Type: "decimal", Mode: "aggregate", Source: &PageResourceSource{Kind: "count", Query: "read"}}
	d.Variables["count"] = count
	if d.Check(sections) == nil {
		t.Fatal("count from another scope accepted")
	}
}
