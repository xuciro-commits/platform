package platform

import (
	"strings"
	"testing"
)

func TestNoticeStaticTextProfilesAndBudgets(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	empty := ""
	s := Section{ID: "note", Widget: "notice", ConfigVersion: 1, Notice: &PageNotice{Title: &empty, Message: "<img src=x> {value} **literal**", Tone: "info"}}
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	root := d.Nodes[d.Root]
	root.Children = append(root.Children, s.ID)
	d.Nodes[d.Root] = root
	p.Sections = append(p.Sections, s)
	if err := d.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	if (*PageDocument)(nil).Check([]Section{s}) == nil {
		t.Fatal("notice without document accepted")
	}
	d.UIProfile = "platform.page.v2.60"
	if d.Check(p.Sections) == nil {
		t.Fatal("old profile accepted notice")
	}
	d.UIProfile = PageUIProfile()
	for _, tone := range []string{"info", "success", "warning", "danger"} {
		s.Notice.Tone = tone
		s.Notice.Message = ""
		s.Notice.Title = nil
		if err := d.checkNotice(s); err != nil {
			t.Fatal("valid empty note refused", tone, err)
		}
	}
	for _, change := range []func(*Section){func(s *Section) { s.Notice = nil }, func(s *Section) { s.Notice.Tone = "primary" }, func(s *Section) { v := strings.Repeat("中", 342); s.Notice.Title = &v }, func(s *Section) { s.Notice.Message = strings.Repeat("中", 1366) }, func(s *Section) { s.Widget = "text" }, func(s *Section) { s.CollectionVariable = "window" }, func(s *Section) { s.Fields = []string{"secret"} }, func(s *Section) { s.Actions = []AssetRef{{App: "sample", Kind: AssetAction, Name: "execute"}} }} {
		copy := s
		cfg := *s.Notice
		copy.Notice = &cfg
		change(&copy)
		if d.checkNotice(copy) == nil {
			t.Fatal("invalid note accepted", copy)
		}
	}
}
