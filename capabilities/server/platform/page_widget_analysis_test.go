package platform

import (
	"encoding/json"
	"testing"
)

// page_gauge_test.go

func TestGaugeOriginalNumberAggregateAndProfile(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	q := d.Variables["window"].Source.Query
	d.Variables["average"] = PageVariable{Scope: "page", Type: "number", Mode: "aggregate", Source: &PageResourceSource{Kind: "aggregate", Query: q, Measure: "avg:availability"}}
	s := Section{PageWidgetConfig: PageWidgetConfig{GaugeValueVariable: "average", Gauge: &PageGauge{Max: 100, Label: "Availability", Suffix: "%"}}, ID: "gauge", Widget: "gauge", ConfigVersion: 1}
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

// page_sparkline_test.go

func sparklinePage() Page {
	p := queryPlanPage()
	d := p.Document
	d.Variables["count"] = PageVariable{Scope: "page", Type: "decimal", Mode: "aggregate", Source: &PageResourceSource{Kind: "count", Query: "read"}}
	s := Section{PageWidgetConfig: PageWidgetConfig{SparklineDecimalVariable: "count", Sparkline: &PageSparkline{Field: "amount"}}, ID: "spark", Widget: "sparkline-kpi", ConfigVersion: 1, CollectionVariable: "window"}
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

// page_histogram_test.go

func TestHistogramOriginalPlanProfileAndFields(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	s := Section{PageWidgetConfig: PageWidgetConfig{Histogram: &PageHistogram{Field: "value", Bins: 12}}, ID: "hist", Widget: "histogram", ConfigVersion: 1, CollectionVariable: "window"}
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	r := d.Nodes[d.Root]
	r.Children = append(r.Children, s.ID)
	d.Nodes[d.Root] = r
	p.Sections = append(p.Sections, s)
	if err := d.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckHistogram(EntityInfo{Fields: []FieldInfo{{Name: "value", Type: "decimal"}}}); err != nil {
		t.Fatal(err)
	}
	d.UIProfile = "platform.page.v2.67"
	if d.Check(p.Sections) == nil {
		t.Fatal("old profile accepted histogram")
	}
	d.UIProfile = PageUIProfile()
	for _, f := range []FieldInfo{{Name: "value", Type: "money"}, {Name: "value", Type: "text"}, {Name: "hidden", Type: "integer"}} {
		if s.CheckHistogram(EntityInfo{Fields: []FieldInfo{f}}) == nil {
			t.Fatal("invalid or hidden field accepted")
		}
	}
	for _, bins := range []int{0, 65, -1} {
		s.Histogram.Bins = bins
		if d.checkHistogram(s) == nil {
			t.Fatal("unbounded bins accepted")
		}
	}
}

// page_heatmap_test.go

func heatmapPage() Page {
	p := queryPlanPage()
	s := Section{PageWidgetConfig: PageWidgetConfig{RowSetVariable: "row", ColumnValueVariable: "column"}, ID: "heatmap", Widget: "heatmap", ConfigVersion: 1, CollectionVariable: "window", Group: "state", ColumnGroup: "priority", Measure: "count"}
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
	}{{Section{Widget: "heatmap", Group: "state", ColumnGroup: "priority", Measure: "count"}, "priority"}, {Section{PageWidgetConfig: PageWidgetConfig{Scatter: &PageRecordScatter{XField: "x", YField: "y", ColorField: "state", LabelField: "name"}}, Widget: "record-scatter"}, "x"}, {Section{PageWidgetConfig: PageWidgetConfig{Histogram: &PageHistogram{Field: "amount", Bins: 4}}, Widget: "histogram"}, "amount"}, {Section{Widget: "term-counts", Group: "name"}, "name"}, {Section{Widget: "treemap", Group: "name"}, "name"}, {Section{PageWidgetConfig: PageWidgetConfig{RecordCard: &PageRecordCard{LabelField: "name", Tone: "info"}}, Widget: "record-card"}, "name"}, {Section{PageWidgetConfig: PageWidgetConfig{Sparkline: &PageSparkline{Field: "x"}}, Widget: "sparkline-kpi", CollectionVariable: "window"}, "x"}} {
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

func TestFrozenAnalysisWidgetsRejectReservedGroupFields(t *testing.T) {
	ref := AssetRef{App: "sample", Kind: AssetObject, Name: "sample.note"}
	lookup := map[AssetRef]ReleaseAsset{ref: {Ref: ref, Body: Raw(EntityInfo{Type: ref.Name, Fields: []FieldInfo{
		{Name: "state", Type: "text"}, {Name: "count", Type: "text"}, {Name: "bucket:day", Type: "text"},
	}})}}
	for _, section := range []Section{
		{Widget: "heatmap", Group: "state", ColumnGroup: "count", Measure: "count"},
		{Widget: "term-counts", Group: "count"},
		{Widget: "treemap", Group: "bucket:day"},
		{Widget: "tag-counts", Group: "count"},
	} {
		p := Page{Object: ref, Document: &PageDocument{}, Sections: []Section{section}}
		if checkFrozenQueries(p, lookup) == nil {
			t.Fatalf("frozen %s accepted reserved original field %s/%s", section.Widget, section.Group, section.ColumnGroup)
		}
	}
}

// page_treemap_test.go

func treemapPage() Page {
	p := queryPlanPage()
	d := p.Document
	s := Section{PageWidgetConfig: PageWidgetConfig{GroupSetVariable: "chosen"}, ID: "tree", Widget: "treemap", ConfigVersion: 1, CollectionVariable: "window", Group: "state"}
	d.Variables["chosen"] = PageVariable{Scope: "page", Type: "string-set", Mode: "state", Initial: Raw(map[string]any{"kind": "string-set", "values": []string{}})}
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	root := d.Nodes[d.Root]
	root.Children = append(root.Children, s.ID)
	d.Nodes[d.Root] = root
	p.Sections = append(p.Sections, s)
	return p
}
func TestTreemapOriginalCountAndTypedFilterOwner(t *testing.T) {
	p := treemapPage()
	if err := p.Document.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	i := len(p.Sections) - 1
	for _, change := range []func(*Page){func(p *Page) { p.Document.UIProfile = "platform.page.v2.70" }, func(p *Page) { p.Sections[i].GroupValueVariable = "chosen" }, func(p *Page) { p.Sections[i].Widget = "term-counts" }, func(p *Page) { p.Sections[i].Group = "count" }, func(p *Page) {
		v := p.Document.Variables["chosen"]
		v.Mode = "constant"
		p.Document.Variables["chosen"] = v
	}, func(p *Page) {
		v := p.Document.Variables["chosen"]
		v.Scope = "overlay"
		v.Owner = "other"
		p.Document.Variables["chosen"] = v
	}} {
		p := treemapPage()
		change(&p)
		if p.Document.Check(p.Sections) == nil {
			t.Fatal("invalid treemap accepted")
		}
	}
	s := p.Sections[i]
	if (*PageDocument)(nil).Check([]Section{s}) == nil {
		t.Fatal("documentless treemap accepted")
	}
	info := EntityInfo{Fields: []FieldInfo{{Name: "state", Type: "choice"}}}
	if err := s.CheckTerms(info); err != nil {
		t.Fatal(err)
	}
	info.Fields = nil
	if s.CheckTerms(info) == nil {
		t.Fatal("hidden grouping accepted")
	}
}

// page_terms_test.go

func TestOriginalTermCountsProfileAndFields(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	s := Section{ID: "terms", Widget: "term-counts", ConfigVersion: 1, CollectionVariable: "window", Group: "bucket"}
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	r := d.Nodes[d.Root]
	r.Children = append(r.Children, s.ID)
	d.Nodes[d.Root] = r
	p.Sections = append(p.Sections, s)
	if err := d.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckTerms(EntityInfo{Fields: []FieldInfo{{Name: "bucket", Type: "text"}}}); err != nil {
		t.Fatal(err)
	}
	d.UIProfile = "platform.page.v2.66"
	if d.Check(p.Sections) == nil {
		t.Fatal("old profile accepted term counts")
	}
	d.UIProfile = PageUIProfile()
	for _, field := range []FieldInfo{{Name: "bucket", Type: "decimal"}, {Name: "other", Type: "text"}} {
		if s.CheckTerms(EntityInfo{Fields: []FieldInfo{field}}) == nil {
			t.Fatal("invalid or hidden group field accepted")
		}
	}
	s.Group = "count"
	if d.checkTerms(s) == nil {
		t.Fatal("count column collision accepted")
	}
	s.Group = "bucket:day"
	if d.checkTerms(s) == nil {
		t.Fatal("bucket coercion accepted")
	}
	s.Group = "bucket"
	v := d.Variables["window"]
	v.Scope = "application"
	d.Variables["window"] = v
	if d.checkTerms(s) == nil {
		t.Fatal("unimplemented shared terms accepted")
	}
}

// page_summary_test.go

func TestSummaryMatchingFiveMeasureSourceAndOriginalField(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	q := d.Variables["window"].Source.Query
	d.Variables["stats"] = PageVariable{Scope: "page", Type: "statistics", Mode: "aggregate", Source: &PageResourceSource{Kind: "statistics", Query: q, Measure: "qty"}}
	s := Section{PageWidgetConfig: PageWidgetConfig{StatisticsVariable: "stats", SummaryField: "qty"}, ID: "summary", Widget: "summary-stats", ConfigVersion: 1, CollectionVariable: "window"}
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

// page_record_scatter_test.go

func TestScatterOriginalWindowAndSelectionContract(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	s := Section{PageWidgetConfig: PageWidgetConfig{Scatter: &PageRecordScatter{XField: "x", YField: "y", ColorField: "state", LabelField: "title"}}, ID: "scatter", Widget: "record-scatter", ConfigVersion: 1, CollectionVariable: "window"}
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
	if other.checkWidgetConfiguration() == nil {
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

// page_record_chart_test.go

func TestRecordChartKeepsOriginalAxesAndOrderedPlan(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	q := d.Queries[d.Variables["window"].Source.Query]
	q.Sort = []string{"id"}
	d.Queries[d.Variables["window"].Source.Query] = q
	s := Section{PageWidgetConfig: PageWidgetConfig{RecordChart: &PageRecordChart{Mark: "line", XField: "id", YField: "qty"}}, ID: "chart", Widget: "record-chart", ConfigVersion: 1, CollectionVariable: "window"}
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

// page_facets_test.go

func TestTypedFacetQueryProfile(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	d.Variables["picked"] = PageVariable{Scope: "page", Type: "string-set", Mode: "state", Initial: json.RawMessage(`{"kind":"string-set","values":[]}`)}
	q := d.Queries["read"]
	q.Conditions = []PageQueryCondition{{Field: "bucket", Op: "in", Optional: true, Value: PageValue{Variable: "picked"}}}
	d.Queries["read"] = q
	facet := Section{ID: "facets", Widget: "filter", ConfigVersion: 1, CollectionVariable: "window", Facets: []PageFacet{{Field: "bucket", Variable: "picked", Kind: "histogram"}}}
	p.Sections = append(p.Sections, facet)
	d.Nodes["facets"] = PageLayoutNode{Kind: "widget", Section: "facets"}
	root := d.Nodes[d.Root]
	root.Children = append(root.Children, "facets")
	d.Nodes[d.Root] = root
	info := EntityInfo{Type: "sample.note", Fields: []FieldInfo{{Name: "bucket", Type: "text"}}}
	if err := d.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	if err := p.CheckQuerySchema(q, info, nil); err != nil {
		t.Fatal(err)
	}
	if err := facet.CheckFacetSchema(info); err != nil {
		t.Fatal(err)
	}
	d.UIProfile = "platform.page.v2.29"
	if d.Check(p.Sections) == nil {
		t.Fatal("old profile accepted facets")
	}
	d.UIProfile = PageUIProfile()
	for _, raw := range []string{`{"kind":"string-set","values":["A","A"]}`, `{"kind":"string-set","values":[2]}`, `{"kind":"string-set","values":[],"extra":true}`} {
		v := d.Variables["picked"]
		v.Initial = json.RawMessage(raw)
		d.Variables["picked"] = v
		if d.CheckVariables() == nil {
			t.Fatal("malformed set accepted")
		}
	}
}

// page_shared_record_output_test.go

func TestAnalyticsSharedRecordProducersAndFrozenIdentity(t *testing.T) {
	makePage := func() Page {
		p := queryPlanPage()
		d := p.Document
		d.Events = nil
		d.Nodes = map[string]PageLayoutNode{"root": {Kind: "rows", Children: []string{"scatter", "rank"}}, "scatter": {Kind: "widget", Section: "scatter"}, "rank": {Kind: "widget", Section: "rank"}}
		d.Variables = map[string]PageVariable{"window": d.Variables["window"], "bucket": d.Variables["bucket"], "selected": {Scope: "application", Type: "record", Mode: "shared", Writable: true, Source: &PageResourceSource{Kind: "application", Variable: "record", Object: &p.Object}}}
		q := d.Queries["read"]
		q.Sort = []string{"id"}
		d.Queries["read"] = q
		rank := q
		rank.Sort = []string{"-qty", "id"}
		rank.Limit = 8
		d.Queries["rank"] = rank
		d.Variables["ranking"] = PageVariable{Scope: "page", Type: "object-set", Mode: "resource", Source: &PageResourceSource{Kind: "plan", Query: "rank"}}
		p.Sections = []Section{{PageWidgetConfig: PageWidgetConfig{Scatter: &PageRecordScatter{XField: "qty", YField: "qty", ColorField: "state", LabelField: "id"}}, ID: "scatter", Widget: "record-scatter", ConfigVersion: 1, CollectionVariable: "window", SelectionVariable: "selected"}, {PageWidgetConfig: PageWidgetConfig{Leaderboard: &PageLeaderboard{ValueField: "qty", LabelField: "id", Limit: 8}}, ID: "rank", Widget: "record-leaderboard", ConfigVersion: 1, CollectionVariable: "ranking", SelectionVariable: "selected"}}
		return p
	}
	p := makePage()
	if err := p.Document.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	if err := p.CheckCollectionPorts(); err != nil {
		t.Fatal(err)
	}
	app := Application{Name: "desk", Pages: []string{p.Name}, UIProfile: PageUIProfile(), Variables: map[string]PageVariable{"record": {Scope: "application", Type: "record", Mode: "resource", Source: &PageResourceSource{Kind: "record", Object: &p.Object}}}}
	if err := app.CheckPageVariables(p); err != nil {
		t.Fatal(err)
	}
	asset, err := PageReleaseAsset("sample", "analytics-1", p)
	if err != nil {
		t.Fatal(err)
	}
	var frozen Page
	if err = json.Unmarshal(asset.Body, &frozen); err != nil {
		t.Fatal(err)
	}
	for _, s := range frozen.Sections {
		if s.SelectionVariable != "selected" {
			t.Fatal("lost shared output in frozen declaration")
		}
	}
	for _, change := range []func(*Page){func(p *Page) { p.Document.UIProfile = "platform.page.v2.94" }, func(p *Page) {
		v := p.Document.Variables["selected"]
		v.Writable = false
		p.Document.Variables["selected"] = v
	}, func(p *Page) { p.Sections[0].Selection = "local" }, func(p *Page) { p.Sections[1].Selection = "local" }, func(p *Page) {
		p.Document.Nodes["root"] = PageLayoutNode{Kind: "loop", Children: []string{"scatter", "rank"}}
		if p.Document.checkSharedRecordOutput(p.Sections[0]) == nil || p.Document.checkSharedRecordOutput(p.Sections[1]) == nil {
			t.Fatal("Loop writer accepted")
		}
	}, func(p *Page) {
		p.Document.Nodes["root"] = PageLayoutNode{Kind: "rows"}
		p.Document.Nodes["panel"] = PageLayoutNode{Kind: "rows", Children: []string{"scatter", "rank"}}
		p.Document.Overlays = map[string]PageOverlay{"panel": {Root: "panel", Kind: "drawer", OpenVariable: "open"}}
		p.Document.Variables["open"] = PageVariable{Scope: "page", Type: "boolean", Mode: "state", Initial: Raw(false)}
		if p.Document.checkSharedRecordOutput(p.Sections[0]) == nil || p.Document.checkSharedRecordOutput(p.Sections[1]) == nil {
			t.Fatal("Overlay writer accepted")
		}
	}} {
		p := makePage()
		change(&p)
		if p.Document.Check(p.Sections) == nil {
			t.Fatal("invalid shared output accepted")
		}
	}
	p = makePage()
	other := p.Object
	other.Name = "sample.other"
	v := p.Document.Variables["selected"]
	v.Source.Object = &other
	p.Document.Variables["selected"] = v
	if p.CheckCollectionPorts() == nil {
		t.Fatal("wrong object accepted")
	}
	if app.CheckPageVariables(p) == nil {
		t.Fatal("wrong application binding accepted")
	}
}
