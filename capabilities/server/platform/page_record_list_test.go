package platform

import "testing"

func TestRecordListProfileAndOriginalProducer(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	s := Section{ID: "cards", Widget: "record-list", ConfigVersion: 1, CardLabel: "title", Fields: []string{"qty"}, CollectionVariable: "window", RecordList: &PageRecordList{Layout: "grid"}}
	d.Nodes["cards"] = PageLayoutNode{Kind: "widget", Section: "cards"}
	root := d.Nodes[d.Root]
	root.Children = append(root.Children, "cards")
	d.Nodes[d.Root] = root
	p.Sections = append(p.Sections, s)
	d.Variables["cardRecord"] = PageVariable{Scope: "page", Type: "record", Mode: "resource", Source: &PageResourceSource{Kind: "record", Section: "cards"}}
	if err := d.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	info := EntityInfo{Type: "sample.note", Fields: []FieldInfo{{Name: "title", Type: "text"}, {Name: "qty", Type: "integer"}}}
	if err := s.CheckRecordList(info); err != nil {
		t.Fatal(err)
	}
	d.UIProfile = "platform.page.v2.41"
	if d.Check(p.Sections) == nil {
		t.Fatal("old profile accepted cards")
	}
	d.UIProfile = PageUIProfile()
	copy := s
	copy.RecordList = &PageRecordList{Layout: "script"}
	if d.checkRecordList(copy) == nil {
		t.Fatal("unknown layout accepted")
	}
	copy = s
	copy.Fields = []string{"qty", "qty"}
	if copy.CheckRecordList(info) == nil {
		t.Fatal("duplicate summary accepted")
	}
	copy = s
	copy.CardLabel = "hidden"
	if copy.CheckRecordList(info) == nil {
		t.Fatal("hidden label accepted")
	}
	if (*PageDocument)(nil).Check([]Section{s}) == nil {
		t.Fatal("cards without document accepted")
	}
}
