package platform

import "testing"

func TestScatterOriginalWindowAndSelectionContract(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	s := Section{ID: "scatter", Widget: "record-scatter", ConfigVersion: 1, CollectionVariable: "window", Scatter: &PageRecordScatter{XField: "x", YField: "y", ColorField: "state", LabelField: "title"}}
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	root := d.Nodes[d.Root]
	root.Children = append(root.Children, s.ID)
	d.Nodes[d.Root] = root
	p.Sections = append(p.Sections, s)
	d.Variables["chosen"] = PageVariable{Scope: "page", Type: "record", Mode: "resource", Source: &PageResourceSource{Kind: "record", Section: s.ID}}
	if err := d.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	if (*PageDocument)(nil).Check([]Section{s}) == nil {
		t.Fatal("documentless scatter accepted")
	}
	d.UIProfile = "platform.page.v2.68"
	if d.Check(p.Sections) == nil {
		t.Fatal("old profile accepted")
	}
	d.UIProfile = PageUIProfile()
	q := d.Queries["read"]
	old := q
	q.Sort = nil
	d.Queries["read"] = q
	if d.Check(p.Sections) == nil {
		t.Fatal("unordered points accepted")
	}
	q = old
	q.Limit = 101
	d.Queries["read"] = q
	if d.Check(p.Sections) == nil {
		t.Fatal("oversized window accepted")
	}
	d.Queries["read"] = old
	other := s
	other.Widget = "record-chart"
	if d.checkScatter(other) == nil {
		t.Fatal("config on another widget accepted")
	}
	info := EntityInfo{Fields: []FieldInfo{{Name: "x", Type: "integer"}, {Name: "y", Type: "decimal"}, {Name: "state", Type: "choice"}, {Name: "title", Type: "text"}}}
	if err := s.CheckScatter(info); err != nil {
		t.Fatal(err)
	}
	for _, field := range info.Fields {
		hidden := info
		hidden.Fields = nil
		for _, f := range info.Fields {
			if f.Name != field.Name {
				hidden.Fields = append(hidden.Fields, f)
			}
		}
		if s.CheckScatter(hidden) == nil {
			t.Fatalf("hidden %s accepted", field.Name)
		}
	}
	info.Fields[0].Type = "text"
	if s.CheckScatter(info) == nil {
		t.Fatal("coerced coordinate accepted")
	}
}
