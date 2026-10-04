package platform

import (
	"encoding/json"
	"testing"
)

func observationPage() Page {
	samples := AssetRef{App: "sample", Kind: AssetObject, Name: "sample.observation"}
	assets := AssetRef{App: "sample", Kind: AssetObject, Name: "sample.asset"}
	signals := []PageObservationSignal{{Field: "value", Unit: "bar"}, {Field: "temperature", Unit: "°C"}, {Field: "availability", Unit: "%"}}
	d := &PageDocument{FormatVersion: 2, UIProfile: PageUIProfile(), Root: "root", Nodes: map[string]PageLayoutNode{"root": {Kind: "rows", Children: []string{"table", "stats", "availability", "series", "detail"}}}, Queries: map[string]PageQuery{"samples": {Object: samples, Limit: 100, Sort: []string{"-at", "id"}}, "assets": {Object: assets, Limit: 100}, "context": {Object: samples, Limit: 100, Sort: []string{"-at", "id"}, Query: &AssetBinding{Ref: AssetRef{App: "sample", Kind: AssetQuery, Name: "history"}, SourceVersion: "original"}, For: &PageValue{Variable: "asset"}}}, Variables: map[string]PageVariable{"samples": {Scope: "page", Type: "object-set", Mode: "resource", Source: &PageResourceSource{Kind: "plan", Query: "samples"}}, "assets": {Scope: "page", Type: "object-set", Mode: "resource", Source: &PageResourceSource{Kind: "plan", Query: "assets"}}, "context": {Scope: "page", Type: "object-set", Mode: "resource", Source: &PageResourceSource{Kind: "plan", Query: "context"}}, "row": {Scope: "page", Type: "record", Mode: "resource", Source: &PageResourceSource{Kind: "record", Section: "table", Port: "row"}}, "asset": {Scope: "page", Type: "record", Mode: "resource", Source: &PageResourceSource{Kind: "record", Section: "table", Port: "asset"}}, "signal": {Scope: "page", Type: "string", Mode: "state", Initial: Raw("value")}, "threshold": {Scope: "page", Type: "string", Mode: "state", Initial: Raw("11.5")}, "rows": {Scope: "page", Type: "string", Mode: "state", Initial: Raw("10000")}, "count": {Scope: "page", Type: "decimal", Mode: "aggregate", Source: &PageResourceSource{Kind: "count", Query: "assets"}}, "mean": {Scope: "page", Type: "number", Mode: "aggregate", Source: &PageResourceSource{Kind: "aggregate", Query: "assets", Measure: "avg:availability"}}}}
	p := Page{Name: "observations", Object: samples, Layout: "composed", Document: d, Sections: []Section{{ID: "table", Widget: "observation", ConfigVersion: 1, CollectionVariable: "samples", Observation: &PageObservation{Kind: "table", TimeField: "at", Signals: signals, Metadata: &PageObservationMetadata{Asset: "asset"}, AssetField: "asset", AssetObject: &assets, RowHeight: 30, RowOutput: "row", AssetOutput: "asset"}}, {ID: "stats", Widget: "observation", ConfigVersion: 1, CollectionVariable: "samples", RecordVariable: "asset", ObservationContextVariable: "context", ObservationSignalVariable: "signal", ObservationThresholdVariable: "threshold", ObservationRowsVariable: "rows", Observation: &PageObservation{Kind: "statistics", TimeField: "at", Signals: signals, AssetField: "asset", AssetObject: &assets}}, {ID: "availability", Widget: "observation", ConfigVersion: 1, Object: assets, CollectionVariable: "assets", ObservationHistoryVariable: "samples", ObservationCountVariable: "count", ObservationMeanVariable: "mean", Observation: &PageObservation{Kind: "availability", TimeField: "at", Signals: signals[2:], AverageField: "availability"}}, {ID: "series", Widget: "observation", ConfigVersion: 1, CollectionVariable: "samples", Observation: &PageObservation{Kind: "series", TimeField: "at", Signals: signals}}, {ID: "detail", Widget: "detail", ConfigVersion: 1, Object: assets, RecordVariable: "asset", Fields: []string{"name"}}}}
	d.Queries["history"] = d.Queries["samples"]
	d.Variables["history"] = PageVariable{Scope: "page", Type: "object-set", Mode: "resource", Source: &PageResourceSource{Kind: "plan", Query: "history"}}
	p.Sections[2].ObservationHistoryVariable = "history"
	p.Sections[3].CollectionVariable = "history"
	for _, s := range p.Sections {
		d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	}
	return p
}
func observationObjects() map[AssetRef]EntityInfo {
	samples := AssetRef{App: "sample", Kind: AssetObject, Name: "sample.observation"}
	assets := AssetRef{App: "sample", Kind: AssetObject, Name: "sample.asset"}
	return map[AssetRef]EntityInfo{samples: {App: "sample", Type: samples.Name, Fields: []FieldInfo{{Name: "at", Type: "datetime"}, {Name: "value", Type: "decimal"}, {Name: "temperature", Type: "integer"}, {Name: "availability", Type: "decimal"}, {Name: "asset", Type: "reference", Ref: assets.Name}}}, assets: {App: "sample", Type: assets.Name, Fields: []FieldInfo{{Name: "name", Type: "text"}, {Name: "availability", Type: "decimal"}}}}
}
func TestObservationFourKindsOriginalObjectsPortsAndFrozenIdentity(t *testing.T) {
	p := observationPage()
	if err := p.Document.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	objects := observationObjects()
	lookup := func(ref AssetRef) (EntityInfo, bool) { info, ok := objects[ref]; return info, ok }
	for _, s := range p.Sections {
		if err := p.CheckObservation(s, lookup); err != nil {
			t.Fatal(s.ID, err)
		}
	}
	if p.RecordResourceObject("row") != p.Object || p.RecordResourceObject("asset").Name != "sample.asset" || p.RecordVariableObject("asset") != "sample.asset" {
		t.Fatal("output identity was replaced by sample type")
	}
	refs := map[AssetRef]ReleaseAsset{}
	for ref, info := range objects {
		refs[ref] = ReleaseAsset{Ref: ref, SourceVersion: "original", ContractVersion: 1, Body: Raw(info)}
	}
	query := AssetRef{App: "sample", Kind: AssetQuery, Name: "history"}
	refs[query] = ReleaseAsset{Ref: query, SourceVersion: "original", ContractVersion: 1, Body: Raw(NamedQuery{Name: "history", Title: "History", Object: p.Object.Name, By: "asset", Sort: []string{"-at", "id"}, Limit: 100})}
	if err := checkFrozenQueries(p, refs); err != nil {
		t.Fatal(err)
	}
	asset, err := PageReleaseAsset("sample", "original", p)
	if err != nil {
		t.Fatal(err)
	}
	p.Sections[0].Observation.Signals[0].Unit = "later"
	v := p.Document.Variables["threshold"]
	v.Initial = Raw("later")
	p.Document.Variables["threshold"] = v
	var saved Page
	if json.Unmarshal(asset.Body, &saved) != nil || saved.Sections[0].Observation.Signals[0].Unit != "bar" || string(saved.Document.Variables["threshold"].Initial) != `"11.5"` {
		t.Fatal("candidate adopted draft")
	}
	info := objects[p.Object]
	info.Fields = info.Fields[1:]
	objects[p.Object] = info
	if saved.CheckObservation(saved.Sections[0], lookup) == nil {
		t.Fatal("hidden business time accepted")
	}
}
func TestObservationRefusesInventedIdentityStateAndModes(t *testing.T) {
	for name, change := range map[string]func(*Page){"old": func(p *Page) { p.Document.UIProfile = "platform.page.v2.81" }, "foreign output": func(p *Page) { p.Document.Variables["asset"].Source.Port = "row" }, "missing ref": func(p *Page) { p.Sections[0].Observation.AssetField = "" }, "same output": func(p *Page) { p.Sections[0].Observation.AssetOutput = "row" }, "duplicate signal": func(p *Page) { p.Sections[3].Observation.Signals[1].Field = "value" }, "borrowed state": func(p *Page) {
		v := p.Document.Variables["signal"]
		v.Scope = "overlay"
		v.Owner = "elsewhere"
		p.Document.Variables["signal"] = v
	}, "wrong mean": func(p *Page) { p.Document.Variables["mean"].Source.Measure = "avg:value" }, "wrong window": func(p *Page) { q := p.Document.Queries["samples"]; q.Limit = 99; p.Document.Queries["samples"] = q }, "fake order": func(p *Page) {
		q := p.Document.Queries["samples"]
		q.Sort = []string{"id"}
		p.Document.Queries["samples"] = q
	}, "self context": func(p *Page) {
		p.Document.Queries["context"].For.Variable = "row"
		p.Sections[1].ObservationContextVariable = "samples"
	}} {
		t.Run(name, func(t *testing.T) {
			p := observationPage()
			change(&p)
			if p.Document.Check(p.Sections) == nil {
				t.Fatal("invalid observation accepted")
			}
		})
	}
}

func TestObservationBindingsRequireDocumentEvenOnOtherWidgets(t *testing.T) {
	var document *PageDocument
	for _, section := range []Section{{Widget: "observation"}, {Widget: "text", ObservationSignalVariable: "signal"}, {Widget: "text", ObservationMeanVariable: "mean"}, {Widget: "text", Observation: &PageObservation{Kind: "table"}}} {
		if document.Check([]Section{section}) == nil {
			t.Fatal("observation configuration escaped its original document")
		}
	}
}
