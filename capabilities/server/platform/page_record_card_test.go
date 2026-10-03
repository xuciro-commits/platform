package platform

import "testing"

func recordCardPage() Page {
	p := queryPlanPage()
	d := p.Document
	d.Variables["chosen"] = PageVariable{Scope: "page", Type: "record", Mode: "resource", Source: &PageResourceSource{Kind: "record", Section: "table"}}
	s := Section{ID: "card", Widget: "record-card", ConfigVersion: 1, RecordVariable: "chosen", RecordCard: &PageRecordCard{LabelField: "name", Tone: "info"}, Fields: []string{"amount"}}
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	root := d.Nodes[d.Root]
	root.Children = append(root.Children, s.ID)
	d.Nodes[d.Root] = root
	p.Sections = append(p.Sections, s)
	return p
}
func TestRecordCardOriginalResourceAndFields(t *testing.T) {
	p := recordCardPage()
	if err := p.Document.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	i := len(p.Sections) - 1
	for _, change := range []func(*Page){func(p *Page) { p.Document.UIProfile = "platform.page.v2.72" }, func(p *Page) { p.Sections[i].RecordVariable = "window" }, func(p *Page) { p.Sections[i].Widget = "detail" }, func(p *Page) { p.Sections[i].RecordCard.Tone = "red" }, func(p *Page) { p.Sections[i].Fields = []string{"a", "b", "c", "d", "e"} }, func(p *Page) {
		v := p.Document.Variables["chosen"]
		v.Mode = "constant"
		v.Source = nil
		v.Initial = Raw("x")
		p.Document.Variables["chosen"] = v
	}} {
		p := recordCardPage()
		change(&p)
		if p.Document.Check(p.Sections) == nil {
			t.Fatal("invalid record card accepted")
		}
	}
	s := p.Sections[i]
	if (*PageDocument)(nil).Check([]Section{s}) == nil {
		t.Fatal("documentless card accepted")
	}
	info := EntityInfo{Fields: []FieldInfo{{Name: "name", Type: "text"}, {Name: "amount", Type: "decimal"}}}
	if err := s.CheckRecordCard(info); err != nil {
		t.Fatal(err)
	}
	info.Fields = info.Fields[1:]
	if s.CheckRecordCard(info) == nil {
		t.Fatal("hidden title accepted")
	}
	s.RecordCard.LabelField = "id"
	if err := s.CheckRecordCard(info); err != nil {
		t.Fatal(err)
	}
	s.Fields = []string{"amount", "amount"}
	if s.CheckRecordCard(info) == nil {
		t.Fatal("duplicate properties accepted")
	}
}
