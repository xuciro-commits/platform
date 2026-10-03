package platform

import "testing"

func TestGaugeOriginalNumberAggregateAndProfile(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	q := d.Variables["window"].Source.Query
	d.Variables["average"] = PageVariable{Scope: "page", Type: "number", Mode: "aggregate", Source: &PageResourceSource{Kind: "aggregate", Query: q, Measure: "avg:availability"}}
	s := Section{ID: "gauge", Widget: "gauge", ConfigVersion: 1, GaugeValueVariable: "average", Gauge: &PageGauge{Max: 100, Label: "Availability", Suffix: "%"}}
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	root := d.Nodes[d.Root]
	root.Children = append(root.Children, s.ID)
	d.Nodes[d.Root] = root
	p.Sections = append(p.Sections, s)
	if err := d.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	if err := d.CheckAggregateScalar(d.Variables["average"], EntityInfo{Fields: []FieldInfo{{Name: "availability", Type: "decimal"}}}); err != nil {
		t.Fatal(err)
	}
	if d.CheckAggregateScalar(d.Variables["average"], EntityInfo{Fields: []FieldInfo{{Name: "availability", Type: "money"}}}) == nil {
		t.Fatal("money groups collapsed into a scalar")
	}
	d.UIProfile = "platform.page.v2.48"
	if d.Check(p.Sections) == nil {
		t.Fatal("old profile accepted number gauge")
	}
	d.UIProfile = PageUIProfile()
	v := d.Variables["average"]
	v.Type = "decimal"
	d.Variables["average"] = v
	if d.CheckVariables() == nil {
		t.Fatal("float aggregate relabelled exact")
	}
	v.Type = "number"
	v.Mode = "state"
	v.Source = nil
	v.Initial = Raw(NumberValue{Kind: "number", Value: 1})
	d.Variables["average"] = v
	if d.CheckVariables() == nil {
		t.Fatal("number state accepted")
	}
	s.Gauge.Max = 0
	if d.checkGauge(s) == nil {
		t.Fatal("zero maximum accepted")
	}
	if (*PageDocument)(nil).Check([]Section{s}) == nil {
		t.Fatal("gauge without document accepted")
	}
	if _, ok := NumberLiteral(Raw(NumberValue{Kind: "number", Value: 0.1})); !ok {
		t.Fatal("finite number literal rejected")
	}
	for _, raw := range []string{`{"kind":"number","value":null}`, `{"kind":"number","value":"0.1"}`, `{"kind":"decimal","value":"0.1"}`} {
		if _, ok := NumberLiteral([]byte(raw)); ok {
			t.Fatal("invalid number literal accepted")
		}
	}
}
