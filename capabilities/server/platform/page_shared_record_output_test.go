package platform

import (
	"encoding/json"
	"testing"
)

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
		p.Sections = []Section{{ID: "scatter", Widget: "record-scatter", ConfigVersion: 1, CollectionVariable: "window", SelectionVariable: "selected", Scatter: &PageRecordScatter{XField: "qty", YField: "qty", ColorField: "state", LabelField: "id"}}, {ID: "rank", Widget: "record-leaderboard", ConfigVersion: 1, CollectionVariable: "ranking", SelectionVariable: "selected", Leaderboard: &PageLeaderboard{ValueField: "qty", LabelField: "id", Limit: 8}}}
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
