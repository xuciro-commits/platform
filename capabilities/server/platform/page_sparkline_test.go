package platform

import "testing"

func sparklinePage() Page {
	p := queryPlanPage()
	d := p.Document
	d.Variables["count"] = PageVariable{Scope: "page", Type: "decimal", Mode: "aggregate", Source: &PageResourceSource{Kind: "count", Query: "read"}}
	s := Section{ID: "spark", Widget: "sparkline-kpi", ConfigVersion: 1, CollectionVariable: "window", SparklineDecimalVariable: "count", Sparkline: &PageSparkline{Field: "amount"}}
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	root := d.Nodes[d.Root]
	root.Children = append(root.Children, s.ID)
	d.Nodes[d.Root] = root
	p.Sections = append(p.Sections, s)
	return p
}
func TestSparklineOriginalScalarAndRecordWindow(t *testing.T) {
	p := sparklinePage()
	if err := p.Document.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	i := len(p.Sections) - 1
	for _, change := range []func(*Page){func(p *Page) { p.Document.UIProfile = "platform.page.v2.71" }, func(p *Page) { p.Sections[i].SparklineNumberVariable = "count" }, func(p *Page) { p.Sections[i].SparklineDecimalVariable = "" }, func(p *Page) { p.Sections[i].Widget = "record-chart" }, func(p *Page) {
		v := p.Document.Variables["count"]
		v.Scope = "overlay"
		v.Owner = "other"
		p.Document.Variables["count"] = v
	}, func(p *Page) { q := p.Document.Queries["read"]; q.Limit = 31; p.Document.Queries["read"] = q }, func(p *Page) { q := p.Document.Queries["read"]; q.Sort = nil; p.Document.Queries["read"] = q }} {
		p := sparklinePage()
		change(&p)
		if p.Document.Check(p.Sections) == nil {
			t.Fatal("invalid sparkline accepted")
		}
	}
	s := p.Sections[i]
	if (*PageDocument)(nil).Check([]Section{s}) == nil {
		t.Fatal("documentless sparkline accepted")
	}
	if s.CheckSparkline(EntityInfo{Fields: []FieldInfo{{Name: "amount", Type: "decimal"}}}) != nil {
		t.Fatal("original numeric series refused")
	}
	if s.CheckSparkline(EntityInfo{}) == nil {
		t.Fatal("hidden numeric series accepted")
	}
	p.Sections[i].CollectionVariable = ""
	p.Sections[i].Sparkline.Field = ""
	if err := p.Document.Check(p.Sections); err != nil {
		t.Fatal("scalar-only source refused", err)
	}
}
