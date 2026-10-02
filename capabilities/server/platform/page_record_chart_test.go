package platform

import "testing"

func TestRecordChartKeepsOriginalAxesAndOrderedPlan(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	q := d.Queries[d.Variables["window"].Source.Query]
	q.Sort = []string{"id"}
	d.Queries[d.Variables["window"].Source.Query] = q
	s := Section{ID: "chart", Widget: "record-chart", ConfigVersion: 1, CollectionVariable: "window", RecordChart: &PageRecordChart{Mark: "line", XField: "id", YField: "qty"}}
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	root := d.Nodes[d.Root]
	root.Children = append(root.Children, s.ID)
	d.Nodes[d.Root] = root
	p.Sections = append(p.Sections, s)
	if err := d.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	info := EntityInfo{Fields: []FieldInfo{{Name: "qty", Type: "decimal"}, {Name: "name", Type: "text"}}}
	if err := s.CheckRecordChart(info); err != nil {
		t.Fatal(err)
	}
	d.UIProfile = "platform.page.v2.42"
	if d.Check(p.Sections) == nil {
		t.Fatal("old profile accepted a record chart")
	}
	d.UIProfile = PageUIProfile()
	q.Sort = nil
	d.Queries[d.Variables["window"].Source.Query] = q
	if d.Check(p.Sections) == nil {
		t.Fatal("unordered record chart accepted")
	}
	for _, config := range []*PageRecordChart{nil, {Mark: "line", XField: "private", YField: "qty"}, {Mark: "line", XField: "id", YField: "name"}} {
		s.RecordChart = config
		if s.CheckRecordChart(info) == nil {
			t.Fatal("missing or hidden axes accepted")
		}
	}
	if (*PageDocument)(nil).Check([]Section{s}) == nil {
		t.Fatal("record chart without a document accepted")
	}
}
