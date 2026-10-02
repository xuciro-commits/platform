package platform

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestRecordLinksProfileSchemaAndFrozenDependencies(t *testing.T) {
	parent := EntityInfo{App: "sample", Type: "sample.parent"}
	child := EntityInfo{App: "sample", Type: "sample.child", Fields: []FieldInfo{{Name: "parent", Type: "reference", Ref: parent.Type, Inverse: "children"}}}
	ref := AssetRef{App: child.App, Kind: AssetObject, Name: child.Type}
	s := Section{ID: "links", Widget: "record-links", ConfigVersion: 1, RecordLinks: []PageRecordLink{{Object: ref, Field: "parent", Title: "Child records"}}}
	d := &PageDocument{FormatVersion: 2, UIProfile: PageUIProfile(), Root: "root", Nodes: map[string]PageLayoutNode{"root": {Kind: "rows", Children: []string{"links"}}, "links": {Kind: "widget", Section: "links"}}}
	lookup := func(typ string) (EntityInfo, bool) { return child, typ == child.Type }
	if (*PageDocument)(nil).Check([]Section{s}) == nil {
		t.Fatal("record links without a document accepted")
	}
	if err := d.Check([]Section{s}); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckRecordLinks(parent, lookup); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Section){func(s *Section) { s.RecordLinks = nil }, func(s *Section) { s.RecordLinks = append(s.RecordLinks, s.RecordLinks[0]) }, func(s *Section) { s.RecordLinks[0].Field = "" }, func(s *Section) { s.Fields = []string{"parent"} }, func(s *Section) { s.Widget = "detail" }} {
		copy := s
		copy.RecordLinks = slices.Clone(s.RecordLinks)
		change(&copy)
		if d.Check([]Section{copy}) == nil {
			t.Fatal("invalid record links accepted")
		}
	}
	d.UIProfile = "platform.page.v2.37"
	if d.Check([]Section{s}) == nil {
		t.Fatal("old profile accepted links")
	}
	d.UIProfile = PageUIProfile()
	p := Page{Name: "parents", Title: "Parents", Object: AssetRef{App: parent.App, Kind: AssetObject, Name: parent.Type}, Document: d, Sections: []Section{s}}
	page, err := PageReleaseAsset("sample", "1", p)
	if err != nil || !slices.Contains(page.Requires, ref) {
		t.Fatalf("dependency missing: %+v %v", page, err)
	}
	body, _ := json.Marshal(parent)
	parentAsset := ReleaseAsset{Ref: p.Object, SourceVersion: "1", ContractVersion: 1, Body: body}
	body, _ = json.Marshal(child)
	childAsset := ReleaseAsset{Ref: ref, SourceVersion: "1", ContractVersion: 1, Body: body}
	if _, err := Candidate([]AssetRef{page.Ref}, []ReleaseAsset{page, parentAsset, childAsset}); err != nil {
		t.Fatal(err)
	}
	child.Fields[0].Inverse = ""
	if s.CheckRecordLinks(parent, lookup) == nil {
		t.Fatal("undeclared inverse accepted")
	}
	body, _ = json.Marshal(child)
	childAsset.Body = body
	if _, err := Candidate([]AssetRef{page.Ref}, []ReleaseAsset{page, parentAsset, childAsset}); err == nil {
		t.Fatal("frozen invalid incoming reference accepted")
	}
	child.Fields[0].Inverse = "children"
	child.Fields[0].Ref = "sample.other"
	if s.CheckRecordLinks(parent, lookup) == nil {
		t.Fatal("wrong parent accepted")
	}
}
