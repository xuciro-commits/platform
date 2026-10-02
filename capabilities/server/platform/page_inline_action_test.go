package platform

import "testing"

func TestInlineActionRequiresOneOriginalRecordAction(t *testing.T) {
	info := EntityInfo{App: "sample", Type: "sample.task"}
	action := Action{Schema: "sample.task.close", Target: info.Type}
	s := Section{Widget: "inline-action", Actions: []AssetRef{{App: info.App, Kind: AssetAction, Name: action.Schema}}}
	if err := s.CheckInlineAction(info, action); err != nil {
		t.Fatal(err)
	}
	doc := PageDocument{FormatVersion: 2, UIProfile: PageUIProfile(), Root: "root", Nodes: map[string]PageLayoutNode{"root": {Kind: "rows", Children: []string{"inline"}}, "inline": {Kind: "widget", Section: "inline"}}}
	s.ID, s.ConfigVersion = "inline", 1
	if err := doc.Check([]Section{s}); err != nil {
		t.Fatal(err)
	}
	doc.UIProfile = "platform.page.v2.28"
	if doc.Check([]Section{s}) == nil {
		t.Fatal("old browser profile accepted inline action")
	}
	doc.UIProfile = PageUIProfile()
	s.ConfigVersion = 2
	if doc.Check([]Section{s}) == nil {
		t.Fatal("unknown inline configuration accepted")
	}
	s.ConfigVersion = 1
	for _, patch := range []func(*Section, *Action){
		func(s *Section, a *Action) { s.Actions = nil },
		func(s *Section, a *Action) { s.Actions = append(s.Actions, s.Actions[0]) },
		func(s *Section, a *Action) { s.Actions[0].App = "other" },
		func(s *Section, a *Action) { s.Actions[0].Kind = AssetObject },
		func(s *Section, a *Action) { a.Schema = "sample.task.edit" },
		func(s *Section, a *Action) { a.Target = "sample.other" },
		func(s *Section, a *Action) { a.New = true },
	} {
		bad, declaration := s, action
		bad.Actions = append([]AssetRef(nil), s.Actions...)
		patch(&bad, &declaration)
		if bad.CheckInlineAction(info, declaration) == nil {
			t.Fatalf("accepted invalid binding: %+v %+v", bad, declaration)
		}
	}
}
