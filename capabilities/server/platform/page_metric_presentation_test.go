package platform

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMetricPresentationAndFrozenMeasure(t *testing.T) {
	info := EntityInfo{App: "sample", Type: "sample.item", Fields: []FieldInfo{{Name: "qty", Type: "decimal"}, {Name: "amount", Type: "money"}}}
	s := Section{ID: "metric", Widget: "metric", ConfigVersion: 1, Measure: "avg:qty", MetricPresentation: &PageMetricPresentation{Suffix: "%", Formatter: "number", Variant: "card", Tone: "success"}}
	d := &PageDocument{FormatVersion: 2, UIProfile: PageUIProfile(), Root: "root", Nodes: map[string]PageLayoutNode{"root": {Kind: "rows", Children: []string{"metric"}}, "metric": {Kind: "widget", Section: "metric"}}}
	if d.Check([]Section{s}) != nil || s.CheckMetricPresentation(info) != nil {
		t.Fatal("valid metric presentation refused")
	}
	for _, change := range []func(*Section){func(s *Section) { s.MetricPresentation.Prefix = strings.Repeat("x", 65) }, func(s *Section) { s.MetricPresentation.Formatter = "script" }, func(s *Section) { s.Widget = "text" }} {
		copy := s
		value := *s.MetricPresentation
		copy.MetricPresentation = &value
		change(&copy)
		if d.Check([]Section{copy}) == nil {
			t.Fatal("invalid metric presentation accepted")
		}
	}
	d.UIProfile = "platform.page.v2.39"
	if d.Check([]Section{s}) == nil {
		t.Fatal("old profile accepted display")
	}
	d.UIProfile = PageUIProfile()
	if (*PageDocument)(nil).Check([]Section{s}) == nil {
		t.Fatal("display without document accepted")
	}
	ref := AssetRef{App: info.App, Kind: AssetObject, Name: info.Type}
	page, err := PageReleaseAsset("sample", "1", Page{Name: "metrics", Title: "Metrics", Object: ref, Document: d, Sections: []Section{s}})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(info)
	object := ReleaseAsset{Ref: ref, SourceVersion: "1", ContractVersion: 1, Body: body}
	if _, err := Candidate([]AssetRef{page.Ref}, []ReleaseAsset{page, object}); err != nil {
		t.Fatal(err)
	}
	info.Fields = info.Fields[1:]
	body, _ = json.Marshal(info)
	object.Body = body
	if _, err := Candidate([]AssetRef{page.Ref}, []ReleaseAsset{page, object}); err == nil {
		t.Fatal("frozen missing measure field accepted")
	}
	s.Measure = "sum:amount"
	s.MetricPresentation.Formatter = "short"
	if s.CheckMetricPresentation(info) == nil {
		t.Fatal("money lost currency semantics")
	}
}
