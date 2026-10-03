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

func TestRecordPickerStableIDStateOwnership(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	d.Variables["personID"] = PageVariable{Scope: "page", Type: "string", Mode: "state", Initial: []byte(`"legacy name"`)}
	s := Section{ID: "pickerID", Widget: "record-picker", ConfigVersion: 1, CollectionVariable: "window", PickerValueVariable: "personID", RecordPicker: &PageRecordPicker{LabelField: "title"}}
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	root := d.Nodes[d.Root]
	root.Children = append(root.Children, s.ID)
	d.Nodes[d.Root] = root
	p.Sections = append(p.Sections, s)
	if err := d.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	d.UIProfile = "platform.page.v2.63"
	if d.Check(p.Sections) == nil {
		t.Fatal("old profile accepted stable ID output")
	}
	d.UIProfile = PageUIProfile()
	for _, v := range []PageVariable{{Scope: "page", Type: "string", Mode: "constant", Initial: []byte(`""`)}, {Scope: "page", Type: "boolean", Mode: "state", Initial: []byte(`false`)}, {Scope: "overlay", Owner: "other", Type: "string", Mode: "state", Initial: []byte(`""`)}} {
		d.Variables["personID"] = v
		if d.Check(p.Sections) == nil {
			t.Fatal("incompatible ID state accepted")
		}
	}
	d.Variables["personID"] = PageVariable{Scope: "page", Type: "string", Mode: "state", Initial: []byte(`""`)}
	p.Sections[len(p.Sections)-1].Widget = "table"
	if d.Check(p.Sections) == nil {
		t.Fatal("ID output on wrong widget accepted")
	}
}
