package platform

import (
	"encoding/json"
	"testing"
)

func analysisPage() Page {
	object := AssetRef{App: "sample", Kind: AssetObject, Name: "sample.note"}
	d := &PageDocument{FormatVersion: 2, UIProfile: PageUIProfile(), Root: "root", Nodes: map[string]PageLayoutNode{"root": {Kind: "rows"}}, Queries: map[string]PageQuery{"read": {Object: object, Limit: 100, Sort: []string{"id"}}, "axes": {Object: object, Limit: 80, Sort: []string{"id"}}}, Variables: map[string]PageVariable{
		"window": {Scope: "page", Type: "object-set", Mode: "resource", Source: &PageResourceSource{Kind: "plan", Query: "read"}}, "axes": {Scope: "page", Type: "object-set", Mode: "resource", Source: &PageResourceSource{Kind: "plan", Query: "axes"}}, "count": {Scope: "page", Type: "decimal", Mode: "aggregate", Source: &PageResourceSource{Kind: "count", Query: "read"}}, "mean": {Scope: "page", Type: "number", Mode: "aggregate", Source: &PageResourceSource{Kind: "aggregate", Query: "read", Measure: "avg:pressure"}}, "x": {Scope: "page", Type: "string", Mode: "state", Initial: Raw("pressure")}, "y": {Scope: "page", Type: "string", Mode: "state", Initial: Raw("temperature")}}}
	p := Page{Name: "analysis", Object: object, Layout: "composed", Document: d, Sections: []Section{{ID: "bars", Widget: "collection-analysis", ConfigVersion: 1, CollectionVariable: "window", Analysis: &PageCollectionAnalysis{Kind: "status-bars", GroupField: "status"}}, {ID: "signed", Widget: "collection-analysis", ConfigVersion: 1, CollectionVariable: "window", Analysis: &PageCollectionAnalysis{Kind: "signed-counts", GroupField: "status", Steps: []PageAnalysisStep{{Value: "ready", Label: "Active", Positive: true}, {Value: "warning", Label: "Warning"}, {Value: "maintenance", Label: "Maint."}, {Value: "offline", Label: "Offline"}}}}, {ID: "mean", Widget: "collection-analysis", ConfigVersion: 1, CollectionVariable: "window", AnalysisCountVariable: "count", AnalysisMeanVariable: "mean", Analysis: &PageCollectionAnalysis{Kind: "derived-mean", Field: "pressure", Unit: "bar"}}, {ID: "axes", Widget: "collection-analysis", ConfigVersion: 1, CollectionVariable: "axes", AnalysisXVariable: "x", AnalysisYVariable: "y", Analysis: &PageCollectionAnalysis{Kind: "record-axes", Fields: []string{"pressure", "temperature", "availability", "exposure"}}}}}
	for _, s := range p.Sections {
		d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
		root := d.Nodes[d.Root]
		root.Children = append(root.Children, s.ID)
		d.Nodes[d.Root] = root
	}
	return p
}
func analysisInfo() EntityInfo {
	return EntityInfo{App: "sample", Type: "sample.note", Fields: []FieldInfo{{Name: "status", Type: "choice", Choices: []string{"ready", "warning", "maintenance", "offline"}}, {Name: "pressure", Type: "decimal"}, {Name: "temperature", Type: "integer"}, {Name: "availability", Type: "decimal"}, {Name: "exposure", Type: "decimal"}}}
}
func TestCollectionAnalysisOriginalOwnersFieldsAndFrozenSchema(t *testing.T) {
	p := analysisPage()
	if err := p.Document.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	if err := p.CheckCollectionPorts(); err != nil {
		t.Fatal(err)
	}
	for _, s := range p.Sections {
		if err := s.CheckCollectionAnalysis(analysisInfo()); err != nil {
			t.Fatal(err)
		}
	}
	lookup := map[AssetRef]ReleaseAsset{p.Object: {Ref: p.Object, SourceVersion: "1", ContractVersion: 1, Body: Raw(analysisInfo())}}
	if err := checkFrozenQueries(p, lookup); err != nil {
		t.Fatal(err)
	}
	asset, err := PageReleaseAsset("sample", "1", p)
	if err != nil {
		t.Fatal(err)
	}
	original := append([]byte(nil), asset.Body...)
	p.Sections[1].Analysis.Steps[0].Value = "later"
	var saved Page
	if json.Unmarshal(asset.Body, &saved) != nil || saved.Sections[1].Analysis.Steps[0].Value != "ready" {
		t.Fatal("frozen analysis adopted mutable draft")
	}
	if string(original) != string(asset.Body) {
		t.Fatal("candidate bytes changed")
	}
	bad := analysisInfo()
	bad.Fields = bad.Fields[:4]
	lookup[p.Object] = ReleaseAsset{Ref: p.Object, Body: Raw(bad)}
	if checkFrozenQueries(saved, lookup) == nil {
		t.Fatal("frozen axes accepted a missing numeric field")
	}
}
func TestCollectionAnalysisRejectsUnownedPortsWrongTypesAndInventedShapes(t *testing.T) {
	for name, change := range map[string]func(*Page){"old profile": func(p *Page) { p.Document.UIProfile = "platform.page.v2.79" }, "axis outside fields": func(p *Page) {
		v := p.Document.Variables["x"]
		v.Initial = Raw("unknown")
		p.Document.Variables["x"] = v
	}, "axis duplicate port": func(p *Page) { p.Sections[3].AnalysisYVariable = "x" }, "wrong mean": func(p *Page) { p.Document.Variables["mean"].Source.Measure = "sum:pressure" }, "wrong query": func(p *Page) { p.Document.Variables["count"].Source.Query = "axes" }, "unbounded axes": func(p *Page) { q := p.Document.Queries["axes"]; q.Limit = 100; p.Document.Queries["axes"] = q }, "wrong sort": func(p *Page) {
		q := p.Document.Queries["axes"]
		q.Sort = []string{"-pressure"}
		p.Document.Queries["axes"] = q
	}, "duplicate status": func(p *Page) { p.Sections[1].Analysis.Steps[1].Value = "ready" }, "changed sign": func(p *Page) { p.Sections[1].Analysis.Steps[1].Positive = true }, "selection output": func(p *Page) { p.Sections[3].Selection = "made-up" }, "foreign overlay": func(p *Page) {
		v := p.Document.Variables["x"]
		v.Scope = "overlay"
		v.Owner = "elsewhere"
		p.Document.Variables["x"] = v
	}, "config on wrong widget": func(p *Page) { p.Sections[0].Widget = "text" }} {
		t.Run(name, func(t *testing.T) {
			p := analysisPage()
			change(&p)
			if p.Document.Check(p.Sections) == nil {
				t.Fatal("invalid original analysis accepted")
			}
		})
	}
	p := analysisPage()
	if (*PageDocument)(nil).Check(p.Sections) == nil {
		t.Fatal("analysis accepted without document")
	}
	info := analysisInfo()
	info.Fields[0].Choices = []string{"ready"}
	if p.Sections[1].CheckCollectionAnalysis(info) == nil {
		t.Fatal("foreign business status mapped")
	}
	info = analysisInfo()
	info.Fields[1].Type = "money"
	if p.Sections[2].CheckCollectionAnalysis(info) == nil {
		t.Fatal("money coerced into numeric mean")
	}
	for _, q := range []NamedQuery{{Sort: []string{"-pressure"}}, {Limit: 20}} {
		if p.CheckAnalysisQuery("axes", &Definition{Query: &q}) == nil {
			t.Fatal("retained query lost bounds or original order")
		}
	}
}
