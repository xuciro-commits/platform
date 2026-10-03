package platform

import (
	"strings"
	"testing"
)

func TestSeparatorStaticLabelsProfilesAndBudgets(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	label := "<img src=x> {value}"
	s := Section{ID: "line", Widget: "separator", ConfigVersion: 1, Separator: &PageSeparator{Label: &label}}
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	root := d.Nodes[d.Root]
	root.Children = append(root.Children, s.ID)
	d.Nodes[d.Root] = root
	p.Sections = append(p.Sections, s)
	if err := d.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	if (*PageDocument)(nil).Check([]Section{s}) == nil {
		t.Fatal("separator without document accepted")
	}
	d.UIProfile = "platform.page.v2.61"
	if d.Check(p.Sections) == nil {
		t.Fatal("old profile accepted separator")
	}
	d.UIProfile = PageUIProfile()
	empty := ""
	for _, value := range []*string{nil, &empty} {
		s.Separator.Label = value
		if err := d.checkSeparator(s); err != nil {
			t.Fatal("absent or empty label refused", err)
		}
	}
	for _, change := range []func(*Section){func(s *Section) { s.Separator = nil }, func(s *Section) { v := strings.Repeat("中", 342); s.Separator.Label = &v }, func(s *Section) { s.Widget = "text" }, func(s *Section) { s.CollectionVariable = "window" }, func(s *Section) { s.Fields = []string{"secret"} }, func(s *Section) { s.Actions = []AssetRef{{App: "sample", Kind: AssetAction, Name: "execute"}} }} {
		copy := s
		c := *s.Separator
		copy.Separator = &c
		change(&copy)
		if d.checkSeparator(copy) == nil {
			t.Fatal("invalid separator accepted", copy)
		}
	}
}
