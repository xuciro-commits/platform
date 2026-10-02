package platform

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestStatusTrackerProfileOriginalSchemaAndFrozenStates(t *testing.T) {
	info := EntityInfo{App: "sample", Type: "sample.item", Fields: []FieldInfo{{Name: "state", Type: "choice"}}, Lifecycle: &LifecycleInfo{Field: "state", States: []State{{Name: "open", Title: "Open"}, {Name: "done", Title: "Done"}}}}
	s := Section{ID: "status", Widget: "status-tracker", ConfigVersion: 1, StatusTracker: &PageStatusTracker{Field: "state", Stages: []string{"done", "open"}}}
	d := &PageDocument{FormatVersion: 2, UIProfile: PageUIProfile(), Root: "root", Nodes: map[string]PageLayoutNode{"root": {Kind: "rows", Children: []string{"status"}}, "status": {Kind: "widget", Section: "status"}}}
	if d.Check([]Section{s}) != nil || s.CheckStatusTracker(info) != nil {
		t.Fatal("valid original lifecycle refused")
	}
	if (*PageDocument)(nil).Check([]Section{s}) == nil {
		t.Fatal("tracker without a document accepted")
	}
	for _, change := range []func(*Section){func(s *Section) { s.StatusTracker = nil }, func(s *Section) { s.StatusTracker.Stages = nil }, func(s *Section) { s.StatusTracker.Stages = []string{"open", "open"} }, func(s *Section) { s.Widget = "detail" }, func(s *Section) {
		s.Actions = []AssetRef{{App: "sample", Kind: AssetAction, Name: "sample.item.close"}}
	}} {
		copy := s
		value := *s.StatusTracker
		value.Stages = slices.Clone(value.Stages)
		copy.StatusTracker = &value
		change(&copy)
		if d.Check([]Section{copy}) == nil {
			t.Fatal("invalid presentation accepted")
		}
	}
	d.UIProfile = "platform.page.v2.38"
	if d.Check([]Section{s}) == nil {
		t.Fatal("old profile accepted tracker")
	}
	d.UIProfile = PageUIProfile()
	hidden := info
	hidden.Fields = nil
	if s.CheckStatusTracker(hidden) == nil {
		t.Fatal("hidden status field accepted")
	}
	ref := AssetRef{App: info.App, Kind: AssetObject, Name: info.Type}
	p := Page{Name: "items", Title: "Items", Object: ref, Document: d, Sections: []Section{s}}
	page, err := PageReleaseAsset("sample", "1", p)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(info)
	object := ReleaseAsset{Ref: ref, SourceVersion: "1", ContractVersion: 1, Body: body}
	if _, err := Candidate([]AssetRef{page.Ref}, []ReleaseAsset{page, object}); err != nil {
		t.Fatal(err)
	}
	info.Lifecycle.States = info.Lifecycle.States[:1]
	body, _ = json.Marshal(info)
	object.Body = body
	if _, err := Candidate([]AssetRef{page.Ref}, []ReleaseAsset{page, object}); err == nil {
		t.Fatal("frozen unknown state accepted")
	}
}
