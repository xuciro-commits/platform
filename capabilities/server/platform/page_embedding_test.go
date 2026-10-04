package platform

import (
	"encoding/json"
	"testing"
)

func embeddedTestPage(name string) Page {
	return Page{Name: name, Object: AssetRef{App: "sample", Kind: AssetObject, Name: "sample.note"}, Layout: "composed", Sections: []Section{{ID: "text", Widget: "text", ConfigVersion: 1, Text: name}}, Document: &PageDocument{FormatVersion: 2, UIProfile: PageUIProfile(), Root: "root", Nodes: map[string]PageLayoutNode{"root": {Kind: "rows", Children: []string{"text"}}, "text": {Kind: "widget", Section: "text"}}, Variables: map[string]PageVariable{}}}
}
func TestEmbeddingFixedContentsInterfacesAndExpandedBudget(t *testing.T) {
	child := embeddedTestPage("child")
	child.Document.Variables["input"] = PageVariable{Scope: "page", Type: "string", Mode: "input"}
	child.Document.Interface = &PageInterface{Version: 1, Inputs: map[string]PagePort{"name": {Variable: "input", Type: "string", Required: true}}}
	digest, _ := PageContentVersion(child)
	e := PageEmbedding{Kind: "module", Page: AssetBinding{Ref: AssetRef{App: "sample", Kind: AssetPage, Name: "child"}, SourceVersion: "1"}, ContentVersion: digest, InterfaceVersion: 1, Inputs: map[string]PageValue{"name": {Literal: Raw("original")}}}
	parent := embeddedTestPage("parent")
	parent.Sections = []Section{{ID: "text", Widget: "embedded-page", ConfigVersion: 1, Embedding: &e}}
	if err := parent.Document.Check(parent.Sections); err != nil {
		t.Fatal(err)
	}
	if err := CheckPageEmbedding(parent, child, e); err != nil {
		t.Fatal(err)
	}
	assets := map[AssetRef]ReleaseAsset{}
	for _, p := range []Page{parent, child} {
		a, err := PageReleaseAsset("sample", "1", p)
		if err != nil {
			t.Fatal(err)
		}
		assets[a.Ref] = a
	}
	root := AssetRef{App: "sample", Kind: AssetPage, Name: "parent"}
	if err := CheckPageEmbeddingGraph(root, assets); err != nil {
		t.Fatal(err)
	}
	bad := e
	bad.ContentVersion = "page.sha256." + "0000000000000000000000000000000000000000000000000000000000000000"
	if CheckPageEmbedding(parent, child, bad) == nil {
		t.Fatal("wrong bytes accepted")
	}
	bad = e
	bad.InterfaceVersion = 2
	if CheckPageEmbedding(parent, child, bad) == nil {
		t.Fatal("wrong interface accepted")
	}
	bad = e
	bad.Inputs = map[string]PageValue{"name": {Literal: Raw(true)}}
	if CheckPageEmbedding(parent, child, bad) == nil {
		t.Fatal("wrong input type accepted")
	}
	bad.Inputs = nil
	if CheckPageEmbedding(parent, child, bad) == nil {
		t.Fatal("required input omitted")
	}
	child.Document.Queries = map[string]PageQuery{"large": {Object: child.Object, Limit: 100}}
	digest, _ = PageContentVersion(child)
	e.ContentVersion = digest
	parent.Sections = nil
	parent.Document.Nodes = map[string]PageLayoutNode{"root": {Kind: "rows"}}
	for _, id := range []string{"a", "b", "c", "d", "e", "f"} {
		parent.Sections = append(parent.Sections, Section{ID: id, Widget: "embedded-page", ConfigVersion: 1, Embedding: &e})
		n := parent.Document.Nodes["root"]
		n.Children = append(n.Children, id)
		parent.Document.Nodes["root"] = n
		parent.Document.Nodes[id] = PageLayoutNode{Kind: "widget", Section: id}
	}
	for _, p := range []Page{parent, child} {
		a, err := PageReleaseAsset("sample", "1", p)
		if err != nil {
			t.Fatal(err)
		}
		assets[a.Ref] = a
	}
	if CheckPageEmbeddingGraph(root, assets) == nil {
		t.Fatal("six repeated windows bypassed the 512-record budget")
	}
}

func TestPageContentIdentityUsesCanonicalLiteralBytes(t *testing.T) {
	page := embeddedTestPage("identity")
	page.Document.Variables["value"] = PageVariable{Scope: "page", Type: "string", Mode: "state", Initial: json.RawMessage(`{"b":2,"a":1}`)}
	first, err := PageContentVersion(page)
	if err != nil {
		t.Fatal(err)
	}
	v := page.Document.Variables["value"]
	v.Initial = json.RawMessage(`{ "a": 1, "b": 2 }`)
	page.Document.Variables["value"] = v
	second, err := PageContentVersion(page)
	if err != nil || first != second {
		t.Fatal("canonical candidate changed content identity", first, second, err)
	}
}
