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

func TestInlineActionDefaultsKeepOriginalParameterAndReferenceTypes(t *testing.T) {
	info := EntityInfo{App: "sample", Type: "sample.item", Fields: []FieldInfo{{Name: "state", Type: "choice"}, {Name: "owner", Type: "reference", Ref: "sample.person"}}}
	action := Action{Schema: "sample.item.update", Target: info.Type, Payload: []Field{{Name: "status", Type: "string"}, {Name: "owner", Type: "string", Ref: "sample.person"}}}
	s := Section{Widget: "inline-action", Actions: []AssetRef{{App: "sample", Kind: AssetAction, Name: action.Schema}}, ActionDefaults: []PageActionParameter{{Parameter: "status", Field: "state"}, {Parameter: "owner", Field: "owner"}}}
	if err := s.CheckInlineAction(info, action); err != nil {
		t.Fatal(err)
	}
	private := info
	private.Fields = private.Fields[:1]
	if s.CheckInlineAction(private, action) == nil {
		t.Fatal("withheld default field accepted")
	}
	action.Payload[1].Ref = "sample.other"
	if s.CheckInlineAction(info, action) == nil {
		t.Fatal("reference default changed object identity")
	}
}

func TestActionDefaultDocumentBudgetAndProfile(t *testing.T) {
	d := &PageDocument{FormatVersion: 2, UIProfile: PageUIProfile(), Root: "root", Nodes: map[string]PageLayoutNode{"root": {Kind: "rows", Children: []string{"action"}}, "action": {Kind: "widget", Section: "action"}}}
	s := Section{ID: "action", Widget: "inline-action", ConfigVersion: 1, Actions: []AssetRef{{App: "sample", Kind: AssetAction, Name: "sample.item.update"}}, ActionDefaults: []PageActionParameter{{Parameter: "reason", Field: "note"}}}
	if err := d.Check([]Section{s}); err != nil {
		t.Fatal(err)
	}
	d.UIProfile = "platform.page.v2.91"
	if d.Check([]Section{s}) == nil {
		t.Fatal("old profile accepted defaults")
	}
	d.UIProfile = PageUIProfile()
	s.ActionDefaults = append(s.ActionDefaults, s.ActionDefaults[0])
	if d.Check([]Section{s}) == nil {
		t.Fatal("duplicate defaults accepted")
	}
	s.ActionDefaults = []PageActionParameter{{Parameter: "constructor", Field: "note"}}
	if d.Check([]Section{s}) == nil {
		t.Fatal("reserved parameter accepted")
	}
}

func TestFrozenInlineDefaultsUseNestedOriginalDeclaration(t *testing.T) {
	object := AssetRef{App: "sample", Kind: AssetObject, Name: "sample.item"}
	ref := AssetRef{App: "sample", Kind: AssetAction, Name: "sample.item.update"}
	info := EntityInfo{App: object.App, Type: object.Name, Fields: []FieldInfo{{Name: "note", Type: "text"}}}
	action := Action{Schema: ref.Name, Target: object.Name, Payload: []Field{{Name: "reason", Type: "string"}}}
	page := Page{Object: object, Document: &PageDocument{UIProfile: PageUIProfile()}, Sections: []Section{{Widget: "inline-action", Actions: []AssetRef{ref}, ActionDefaults: []PageActionParameter{{Parameter: "reason", Field: "note"}}}}}
	lookup := map[AssetRef]ReleaseAsset{object: {Body: Raw(info)}, ref: {Body: Raw(map[string]any{"schema": action.Schema, "target": action.Target, "action": action})}}
	if err := checkFrozenQueries(page, lookup); err != nil {
		t.Fatal(err)
	}
	action.Payload[0].Type = "boolean"
	lookup[ref] = ReleaseAsset{Body: Raw(map[string]any{"schema": action.Schema, "target": action.Target, "action": action})}
	if checkFrozenQueries(page, lookup) == nil {
		t.Fatal("nested parameter tampering passed")
	}
	action.Schema = "sample.item.other"
	lookup[ref] = ReleaseAsset{Body: Raw(map[string]any{"schema": ref.Name, "target": object.Name, "action": action})}
	if checkFrozenQueries(page, lookup) == nil {
		t.Fatal("nested identity tampering passed")
	}
}
