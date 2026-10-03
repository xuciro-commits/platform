package platform

import "testing"

func TestRecordPickerOriginalCandidatesAndProducer(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	s := Section{ID: "picker", Widget: "record-picker", ConfigVersion: 1, CollectionVariable: "window", RecordPicker: &PageRecordPicker{LabelField: "title"}, Selection: "picker"}
	p.Selections = []SelectionVariable{{Name: "picker", Object: p.Object}}
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	root := d.Nodes[d.Root]
	root.Children = append(root.Children, s.ID)
	d.Nodes[d.Root] = root
	p.Sections = append(p.Sections, s)
	d.Variables["picked"] = PageVariable{Scope: "page", Type: "record", Mode: "resource", Source: &PageResourceSource{Kind: "record", Section: s.ID}}
	if err := d.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	if (*PageDocument)(nil).Check([]Section{s}) == nil {
		t.Fatal("picker without doc accepted")
	}
	d.UIProfile = "platform.page.v2.57"
	if d.Check(p.Sections) == nil {
		t.Fatal("old profile accepted picker")
	}
	d.UIProfile = PageUIProfile()
	q := d.Queries["read"]
	q.Limit = 21
	d.Queries["read"] = q
	if d.Check(p.Sections) == nil {
		t.Fatal("large candidate window accepted")
	}
	q.Limit = 20
	q.Offset = 1
	d.Queries["read"] = q
	if d.Check(p.Sections) == nil {
		t.Fatal("candidate paging accepted")
	}
	q.Offset = 0
	q.Sort = []string{"title"}
	d.Queries["read"] = q
	if d.Check(p.Sections) == nil {
		t.Fatal("non-ID order accepted")
	}
	info := EntityInfo{Fields: []FieldInfo{{Name: "title", Type: "text"}}}
	if err := s.CheckRecordPicker(info); err != nil {
		t.Fatal(err)
	}
	info.Fields[0].Type = "decimal"
	if s.CheckRecordPicker(info) == nil {
		t.Fatal("non-text label accepted")
	}
	named := &Definition{Query: &NamedQuery{Sort: []string{"title"}}}
	if p.CheckRecordPickerQuery("read", named) == nil {
		t.Fatal("named ordering discarded")
	}
}
