package platform

import (
	"math"
	"testing"
)

func TestSpacerFiniteSizesZeroAndOriginalLayoutBudget(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	size := 16.0
	s := Section{ID: "blank", Widget: "spacer", ConfigVersion: 1, Spacer: &PageSpacer{Size: &size}}
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	root := d.Nodes[d.Root]
	root.Children = append(root.Children, s.ID)
	d.Nodes[d.Root] = root
	p.Sections = append(p.Sections, s)
	for _, value := range []float64{0, 0.5, 16, 16.25, float64(pageWidgets.Layout.MaxSize)} {
		size = value
		if err := d.Check(p.Sections); err != nil {
			t.Fatal("valid spacer refused", value, err)
		}
	}
	if (*PageDocument)(nil).Check([]Section{s}) == nil {
		t.Fatal("spacer without document accepted")
	}
	d.UIProfile = "platform.page.v2.62"
	if d.Check(p.Sections) == nil {
		t.Fatal("old profile accepted spacer")
	}
	d.UIProfile = PageUIProfile()
	for _, value := range []float64{-1, float64(pageWidgets.Layout.MaxSize) + 0.5, math.Inf(1), math.Inf(-1), math.NaN()} {
		size = value
		if d.checkSpacer(s) == nil {
			t.Fatal("invalid spacer size accepted", value)
		}
	}
	size = 16
	for _, change := range []func(*Section){func(s *Section) { s.Spacer = nil }, func(s *Section) { s.Spacer.Size = nil }, func(s *Section) { s.Widget = "text" }, func(s *Section) { s.CollectionVariable = "window" }, func(s *Section) { s.Fields = []string{"secret"} }, func(s *Section) { s.Actions = []AssetRef{{App: "sample", Kind: AssetAction, Name: "execute"}} }} {
		copy := s
		c := *s.Spacer
		copy.Spacer = &c
		change(&copy)
		if d.checkSpacer(copy) == nil {
			t.Fatal("invalid spacer configuration accepted", copy)
		}
	}
}
