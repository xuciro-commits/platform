package platform

import "testing"

func TestHistogramOriginalPlanProfileAndFields(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	s := Section{ID: "hist", Widget: "histogram", ConfigVersion: 1, CollectionVariable: "window", Histogram: &PageHistogram{Field: "value", Bins: 12}}
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	r := d.Nodes[d.Root]
	r.Children = append(r.Children, s.ID)
	d.Nodes[d.Root] = r
	p.Sections = append(p.Sections, s)
	if err := d.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckHistogram(EntityInfo{Fields: []FieldInfo{{Name: "value", Type: "decimal"}}}); err != nil {
		t.Fatal(err)
	}
	d.UIProfile = "platform.page.v2.67"
	if d.Check(p.Sections) == nil {
		t.Fatal("old profile accepted histogram")
	}
	d.UIProfile = PageUIProfile()
	for _, f := range []FieldInfo{{Name: "value", Type: "money"}, {Name: "value", Type: "text"}, {Name: "hidden", Type: "integer"}} {
		if s.CheckHistogram(EntityInfo{Fields: []FieldInfo{f}}) == nil {
			t.Fatal("invalid or hidden field accepted")
		}
	}
	for _, bins := range []int{0, 65, -1} {
		s.Histogram.Bins = bins
		if d.checkHistogram(s) == nil {
			t.Fatal("unbounded bins accepted")
		}
	}
}
