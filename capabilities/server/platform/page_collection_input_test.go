package platform

import "testing"

func collectionInterfacePages() (Page, Page, PageEmbedding) {
	parent := embeddedTestPage("parent")
	parent.Document.Queries = map[string]PageQuery{"source": {Object: parent.Object, Limit: 20}}
	parent.Document.Variables["set"] = PageVariable{Scope: "page", Type: "object-set", Mode: "resource", Source: &PageResourceSource{Kind: "plan", Query: "source"}}
	child := embeddedTestPage("child")
	child.Document.Variables["objects"] = PageVariable{Scope: "page", Type: "object-set", Mode: "input"}
	child.Document.Interface = &PageInterface{Version: 1, Inputs: map[string]PagePort{"inputObjectSet": {Variable: "objects", Type: "object-set", Object: &parent.Object, Required: true}}}
	child.Document.Queries = map[string]PageQuery{"read": {Object: parent.Object, Input: "objects", Limit: 30}}
	child.Document.Variables["window"] = PageVariable{Scope: "page", Type: "object-set", Mode: "resource", Source: &PageResourceSource{Kind: "plan", Query: "read"}}
	digest, _ := PageContentVersion(child)
	embedding := PageEmbedding{Kind: "custom", Page: AssetBinding{Ref: AssetRef{App: "sample", Kind: AssetPage, Name: "child"}, SourceVersion: "1"}, ContentVersion: digest, InterfaceVersion: 1, Inputs: map[string]PageValue{"inputObjectSet": {Variable: "set"}}}
	return parent, child, embedding
}

func TestCollectionInterfacePreservesOriginalTypedQueryOwnership(t *testing.T) {
	parent, child, e := collectionInterfacePages()
	for _, p := range []Page{parent, child} {
		if err := p.Document.Check(p.Sections); err != nil {
			t.Fatal(err)
		}
	}
	if err := CheckPageEmbedding(parent, child, e); err != nil {
		t.Fatal(err)
	}
	parent.Document.UIProfile = "platform.page.v2.84"
	if CheckPageNavigation(parent, child, e.Navigation()) == nil {
		t.Fatal("old sending renderer accepted collection transport")
	}
	parent.Document.UIProfile = PageUIProfile()
	q := parent.Document.Queries["source"]
	q.Object.App = "foreign"
	parent.Document.Queries["source"] = q
	if CheckPageNavigation(parent, child, e.Navigation()) == nil {
		t.Fatal("wrong original object owner accepted")
	}
	child.Document.UIProfile = "platform.page.v2.84"
	if child.Document.Check(child.Sections) == nil {
		t.Fatal("old receiving profile accepted collection input")
	}
	child.Document.UIProfile = PageUIProfile()
	q = child.Document.Queries["read"]
	q.Input = "missing"
	child.Document.Queries["read"] = q
	if child.Document.Check(child.Sections) == nil {
		t.Fatal("missing collection broadened to an object read")
	}
}
