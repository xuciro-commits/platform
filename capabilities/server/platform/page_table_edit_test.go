package platform

import "testing"

func TestTableEditingProfileAndOriginalAction(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	s := Section{ID: "table", Widget: "table", ConfigVersion: 1, Fields: []string{"name", "locked"}, InlineEdit: &PageInlineEdit{Action: AssetRef{App: "sample", Kind: AssetAction, Name: "sample.note.edit"}, Fields: []string{"name"}}}
	info := EntityInfo{App: "sample", Type: "sample.note", Standard: []string{"sample.note.edit"}, Fields: []FieldInfo{{Name: "name", Type: "text"}, {Name: "locked", Type: "text", ReadOnly: true}}}
	action := Action{Schema: "sample.note.edit", Target: info.Type, Payload: []Field{{Name: "name", Type: "string"}}}
	if err := d.checkTableEdit(s); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckTableEdit(info, action); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Section){func(s *Section) { s.InlineEdit.Fields = []string{"locked"} }, func(s *Section) { s.InlineEdit.Action.Name = "sample.note.archive" }, func(s *Section) { s.InlineEdit.Fields = []string{"id"} }} {
		changed := s
		edit := *s.InlineEdit
		changed.InlineEdit = &edit
		mutate(&changed)
		if changed.CheckTableEdit(info, action) == nil {
			t.Fatal("unsupported edit accepted")
		}
	}
	d.UIProfile = "platform.page.v2.30"
	if d.checkTableEdit(s) == nil {
		t.Fatal("old profile accepted editing")
	}
	action.NeedsApproval = true
	if s.CheckTableEdit(info, action) == nil {
		t.Fatal("approval action disguised as a field patch")
	}
}
