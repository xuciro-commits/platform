package platform

import "testing"

func TestSummaryMatchingFiveMeasureSourceAndOriginalField(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	q := d.Variables["window"].Source.Query
	d.Variables["stats"] = PageVariable{Scope: "page", Type: "statistics", Mode: "aggregate", Source: &PageResourceSource{Kind: "statistics", Query: q, Measure: "qty"}}
	s := Section{ID: "summary", Widget: "summary-stats", ConfigVersion: 1, CollectionVariable: "window", StatisticsVariable: "stats", SummaryField: "qty"}
	p.Sections = append(p.Sections, s)
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	root := d.Nodes[d.Root]
	root.Children = append(root.Children, s.ID)
	d.Nodes[d.Root] = root
	if err := d.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	info := EntityInfo{Fields: []FieldInfo{{Name: "qty", Type: "decimal"}}}
	if err := s.CheckSummary(info); err != nil {
		t.Fatal(err)
	}
	if err := d.CheckAggregateScalar(d.Variables["stats"], info); err != nil {
		t.Fatal(err)
	}
	d.UIProfile = "platform.page.v2.49"
	if d.Check(p.Sections) == nil {
		t.Fatal("old profile accepted statistics")
	}
	d.UIProfile = PageUIProfile()
	s.SummaryField = "other"
	if d.checkSummary(s) == nil {
		t.Fatal("mismatched field accepted")
	}
	s.SummaryField = "qty"
	v := d.Variables["stats"]
	v.Scope = "application"
	d.Variables["stats"] = v
	if d.CheckVariables() == nil {
		t.Fatal("unowned application statistics accepted")
	}
	v.Scope = "page"
	v.Mode = "constant"
	v.Source = nil
	d.Variables["stats"] = v
	if d.CheckVariables() == nil {
		t.Fatal("constant statistics accepted")
	}
	if s.CheckSummary(EntityInfo{Fields: []FieldInfo{{Name: "qty", Type: "money"}}}) == nil {
		t.Fatal("money groups collapsed")
	}
	if s.CheckSummary(EntityInfo{}) == nil {
		t.Fatal("hidden field accepted")
	}
	if (*PageDocument)(nil).Check([]Section{s}) == nil {
		t.Fatal("summary without document accepted")
	}
}
