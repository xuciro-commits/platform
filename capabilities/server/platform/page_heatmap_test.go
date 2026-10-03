package platform

import "testing"

func heatmapPage() Page {
	p := queryPlanPage()
	s := Section{ID: "heatmap", Widget: "heatmap", ConfigVersion: 1, CollectionVariable: "window", Group: "state", ColumnGroup: "priority", Measure: "count", RowSetVariable: "row", ColumnValueVariable: "column"}
	d := p.Document
	d.Variables["row"] = PageVariable{Scope: "page", Type: "string-set", Mode: "state", Initial: Raw(map[string]any{"kind": "string-set", "values": []string{}})}
	d.Variables["column"] = PageVariable{Scope: "page", Type: "string", Mode: "state", Initial: Raw("")}
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	root := d.Nodes[d.Root]
	root.Children = append(root.Children, s.ID)
	d.Nodes[d.Root] = root
	p.Sections = append(p.Sections, s)
	return p
}
func TestHeatmapTypedPortsAndOriginalOwner(t *testing.T) {
	p := heatmapPage()
	if err := p.Document.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	index := len(p.Sections) - 1
	for _, change := range []func(*Page){func(p *Page) { p.Document.UIProfile = "platform.page.v2.69" }, func(p *Page) { p.Sections[index].ColumnGroup = "state" }, func(p *Page) { p.Sections[index].Measure = "sum:qty" }, func(p *Page) { p.Sections[index].RowValueVariable = "column" }, func(p *Page) { p.Sections[index].RowSetVariable = "column" }, func(p *Page) { p.Sections[index].ColumnSetVariable = "row"; p.Sections[index].ColumnValueVariable = "" }, func(p *Page) { v := p.Document.Variables["row"]; v.Mode = "constant"; p.Document.Variables["row"] = v }, func(p *Page) {
		v := p.Document.Variables["row"]
		v.Scope = "overlay"
		v.Owner = "other"
		p.Document.Variables["row"] = v
	}, func(p *Page) { p.Sections[index].Widget = "pivot" }} {
		p := heatmapPage()
		change(&p)
		if p.Document.Check(p.Sections) == nil {
			t.Fatal("invalid heatmap accepted")
		}
	}
	if (*PageDocument)(nil).Check([]Section{p.Sections[index]}) == nil {
		t.Fatal("documentless heatmap accepted")
	}
	s := p.Sections[index]
	info := EntityInfo{Fields: []FieldInfo{{Name: "state", Type: "choice"}, {Name: "priority", Type: "text"}}}
	if err := s.CheckHeatmap(info); err != nil {
		t.Fatal(err)
	}
	info.Fields = info.Fields[:1]
	if s.CheckHeatmap(info) == nil {
		t.Fatal("hidden column accepted")
	}
	info.Fields = append(info.Fields, FieldInfo{Name: "priority", Type: "integer"})
	if s.CheckHeatmap(info) == nil {
		t.Fatal("numeric axis coerced to text")
	}
}

func TestFrozenAnalysisWidgetsCheckOriginalFieldSchemas(t *testing.T) {
	ref := AssetRef{App: "sample", Kind: AssetObject, Name: "sample.note"}
	fields := []FieldInfo{{Name: "state", Type: "choice"}, {Name: "priority", Type: "text"}, {Name: "x", Type: "integer"}, {Name: "y", Type: "decimal"}, {Name: "name", Type: "text"}, {Name: "amount", Type: "decimal"}}
	for _, tc := range []struct {
		section Section
		missing string
	}{{Section{Widget: "heatmap", Group: "state", ColumnGroup: "priority", Measure: "count"}, "priority"}, {Section{Widget: "record-scatter", Scatter: &PageRecordScatter{XField: "x", YField: "y", ColorField: "state", LabelField: "name"}}, "x"}, {Section{Widget: "histogram", Histogram: &PageHistogram{Field: "amount", Bins: 4}}, "amount"}, {Section{Widget: "term-counts", Group: "name"}, "name"}, {Section{Widget: "treemap", Group: "name"}, "name"}} {
		t.Run(tc.section.Widget, func(t *testing.T) {
			p := Page{Object: ref, Document: &PageDocument{}, Sections: []Section{tc.section}}
			lookup := map[AssetRef]ReleaseAsset{ref: {Ref: ref, Body: Raw(EntityInfo{Type: "sample.note", Fields: fields})}}
			if err := checkFrozenQueries(p, lookup); err != nil {
				t.Fatal(err)
			}
			hidden := []FieldInfo{}
			for _, field := range fields {
				if field.Name != tc.missing {
					hidden = append(hidden, field)
				}
			}
			lookup[ref] = ReleaseAsset{Ref: ref, Body: Raw(EntityInfo{Type: "sample.note", Fields: hidden})}
			if checkFrozenQueries(p, lookup) == nil {
				t.Fatal("frozen widget escaped its original field check")
			}
		})
	}
}
