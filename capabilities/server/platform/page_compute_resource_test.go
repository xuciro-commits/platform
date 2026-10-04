package platform

import "testing"

func computeResourcePage() (Page, EntityInfo, Operation) {
	object := AssetRef{App: "sample", Kind: AssetObject, Name: "sample.note"}
	c := &PageComputeResource{Operation: AssetBinding{Ref: AssetRef{App: "sample", Kind: AssetCompute, Name: "score"}, SourceVersion: "1"}, RecordVariable: "selected", Inputs: map[string]Binding{"qty": {Source: "subject", Path: []string{"qty"}}}}
	p := Page{Name: "compute", Object: object, Layout: "composed", Sections: []Section{{ID: "table", Widget: "table", ConfigVersion: 1, Fields: []string{"qty"}}, {ID: "gauge", Widget: "gauge", ConfigVersion: 1, GaugeValueVariable: "number", Gauge: &PageGauge{Max: 100}}, {ID: "mini", Widget: "sparkline-kpi", ConfigVersion: 1, SparklineNumberVariable: "number", Sparkline: &PageSparkline{}}, {ID: "notice", Widget: "alert-banner", ConfigVersion: 1, AlertValueVariable: "exact", AlertBanner: &PageAlertBanner{Threshold: "60", Tone: "warning", Message: "{value}"}}}, Document: &PageDocument{FormatVersion: 2, UIProfile: PageUIProfile(), Root: "root", Nodes: map[string]PageLayoutNode{"root": {Kind: "rows", Children: []string{"table", "gauge", "mini", "notice"}}}, Variables: map[string]PageVariable{"selected": {Scope: "page", Type: "record", Mode: "resource", Source: &PageResourceSource{Kind: "record", Section: "table"}}, "number": {Scope: "page", Type: "number", Mode: "resource", Source: &PageResourceSource{Kind: "compute", Compute: c}}, "exact": {Scope: "page", Type: "decimal", Mode: "resource", Source: &PageResourceSource{Kind: "compute", Compute: c}}}}}
	for _, s := range p.Sections {
		p.Document.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	}
	info := EntityInfo{App: "sample", Type: object.Name, Fields: []FieldInfo{{Name: "qty", Type: "integer"}}}
	op := Operation{Name: "score", Title: "Original score", Input: ValueSchema{Type: "object", Properties: map[string]ValueSchema{"qty": {Type: "integer"}}, Required: []string{"qty"}}, Output: ValueSchema{Type: "integer"}, Roles: []string{"reader"}, Binding: OperationBinding{Kind: "native"}, Limits: OperationLimits{TimeoutMillis: 1000, MemoryPages: 1, MaxInputBytes: 4096, MaxOutputBytes: 4096}}
	return p, info, op
}
func TestComputeResourceThreeConsumersRetainTypedInputAndFrozenOperation(t *testing.T) {
	p, info, op := computeResourcePage()
	if err := p.Document.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	for id, c := range p.ComputeResources() {
		if err := p.CheckComputeResource(c, info, op, p.Document.Variables[id].Type); err != nil {
			t.Fatal(err)
		}
	}
	asset, err := PageReleaseAsset("sample", "1", p)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range asset.Requires {
		found = found || r.Kind == AssetCompute && r.Name == "score"
	}
	if !found {
		t.Fatal("operation was not retained as a candidate dependency")
	}
	lookup := map[AssetRef]ReleaseAsset{p.Object: {Ref: p.Object, SourceVersion: "1", Body: Raw(info)}, p.Document.Variables["number"].Source.Compute.Operation.Ref: {Ref: p.Document.Variables["number"].Source.Compute.Operation.Ref, SourceVersion: "1", Body: Raw(op)}}
	if err := checkFrozenQueries(p, lookup); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Page){func(p *Page) { p.Document.UIProfile = "platform.page.v2.99" }, func(p *Page) {
		v := p.Document.Variables["number"]
		v.Scope = "application"
		p.Document.Variables["number"] = v
	}, func(p *Page) {
		p.Document.Variables["number"].Source.Compute.Inputs["qty"] = Binding{Source: "input", Path: []string{"qty"}}
	}, func(p *Page) {
		p.Document.Variables["number"].Source.Compute.Inputs["qty"] = Binding{Source: "subject", Path: []string{"qty", "nested"}}
	}, func(p *Page) {
		v := p.Document.Variables["number"]
		v.Source.Section = "table"
		p.Document.Variables["number"] = v
	}} {
		p, _, _ := computeResourcePage()
		change(&p)
		if p.Document.Check(p.Sections) == nil {
			t.Fatal("accepted invalid computation owner")
		}
	}
	hidden := info
	hidden.Fields = nil
	if p.CheckComputeResource(*p.Document.Variables["number"].Source.Compute, hidden, op, "number") == nil {
		t.Fatal("hidden fields admitted a calculation")
	}
	op.Output = ValueSchema{Type: "number"}
	if p.CheckComputeResource(*p.Document.Variables["exact"].Source.Compute, info, op, "decimal") == nil {
		t.Fatal("binary fractional output acquired exact decimal semantics")
	}
}

func TestComputeVisibilityRetainsConsumersAndWithdrawsLostRecordInput(t *testing.T) {
	p, _, _ := computeResourcePage()
	visible := p.Document.Visible(p.Sections)
	if visible.Variables["number"].Source == nil || visible.Variables["exact"].Source == nil || visible.Nodes["gauge"].Section != "gauge" || visible.Nodes["mini"].Section != "mini" || visible.Nodes["notice"].Section != "notice" {
		t.Fatal("visible calculation and its three consumers were pruned")
	}
	if err := visible.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	retired := p.Document.Visible(p.Sections[1:])
	if _, ok := retired.Variables["number"]; ok {
		t.Fatal("calculation survived a missing original record")
	}
	if _, ok := retired.Variables["exact"]; ok {
		t.Fatal("exact calculation survived a missing original record")
	}
	for _, id := range []string{"gauge", "mini", "notice"} {
		if _, ok := retired.Nodes[id]; ok {
			t.Fatal("consumer survived its unavailable calculation")
		}
	}
}
