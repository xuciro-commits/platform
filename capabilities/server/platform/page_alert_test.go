package platform

import (
	"strings"
	"testing"
)

func TestAlertUsesOriginalDecimalPortAndBoundedConfiguration(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	d.Variables["count"] = PageVariable{Scope: "page", Type: "decimal", Mode: "aggregate", Source: &PageResourceSource{Kind: "count", Query: "read"}}
	s := Section{ID: "alert", Widget: "alert-banner", ConfigVersion: 1, AlertValueVariable: "count", AlertBanner: &PageAlertBanner{Threshold: "0", Tone: "danger", Message: "{value} affected records"}}
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	root := d.Nodes[d.Root]
	root.Children = append(root.Children, s.ID)
	d.Nodes[d.Root] = root
	p.Sections = append(p.Sections, s)
	if err := d.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	if (*PageDocument)(nil).Check([]Section{s}) == nil {
		t.Fatal("alert without document accepted")
	}
	d.UIProfile = "platform.page.v2.59"
	if d.Check(p.Sections) == nil {
		t.Fatal("old profile accepted alert")
	}
	d.UIProfile = PageUIProfile()
	for _, threshold := range []string{"bad", "1e3", "00", "0.0", "-0", strings.Repeat("9", 129)} {
		copy := s
		cfg := *s.AlertBanner
		copy.AlertBanner = &cfg
		cfg.Threshold = threshold
		if d.checkAlertBanner(copy) == nil {
			t.Fatal("invalid threshold accepted", threshold)
		}
	}
	for _, change := range []func(*Section){func(s *Section) { s.AlertBanner = nil }, func(s *Section) { s.AlertBanner.Tone = "custom" }, func(s *Section) { s.AlertBanner.Message = "" }, func(s *Section) { s.AlertBanner.Message = strings.Repeat("中", 1366) }, func(s *Section) { s.AlertValueVariable = "" }, func(s *Section) { s.Widget = "text" }, func(s *Section) { s.CollectionVariable = "window" }, func(s *Section) { s.Actions = []AssetRef{{App: "sample", Kind: AssetAction, Name: "execute"}} }} {
		copy := s
		cfg := *s.AlertBanner
		copy.AlertBanner = &cfg
		change(&copy)
		if d.checkAlertBanner(copy) == nil {
			t.Fatal("invalid alert accepted", copy)
		}
	}
	v := d.Variables["count"]
	v.Type = "number"
	d.Variables["count"] = v
	if d.checkAlertBanner(s) == nil || d.checkWidgetPorts(s) == nil {
		t.Fatal("floating value substituted for decimal")
	}
	v.Type = "decimal"
	v.Scope = "application"
	d.Variables["count"] = v
	if d.checkAlertBanner(s) == nil {
		t.Fatal("shared application scope accepted")
	}
}

func TestAlertCannotConsumeAnotherOverlayValue(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	d.Variables["open"] = PageVariable{Scope: "page", Type: "boolean", Mode: "state", Initial: Raw(false)}
	d.Variables["local"] = PageVariable{Scope: "overlay", Owner: "panel", Type: "decimal", Mode: "constant", Initial: Raw(map[string]any{"kind": "decimal", "value": "1"})}
	d.Nodes["panelRoot"] = PageLayoutNode{Kind: "rows", Children: []string{"panelText"}}
	d.Nodes["panelText"] = PageLayoutNode{Kind: "widget", Section: "panelText"}
	p.Sections = append(p.Sections, Section{ID: "panelText", Widget: "text", ConfigVersion: 1, Text: "Panel"})
	d.Overlays = map[string]PageOverlay{"panel": {Root: "panelRoot", Title: "Alert panel", Kind: "drawer", OpenVariable: "open"}}
	s := Section{ID: "alert", Widget: "alert-banner", ConfigVersion: 1, AlertValueVariable: "local", AlertBanner: &PageAlertBanner{Threshold: "0", Tone: "warning", Message: "{value} affected"}}
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	root := d.Nodes[d.Root]
	root.Children = append(root.Children, s.ID)
	d.Nodes[d.Root] = root
	p.Sections = append(p.Sections, s)
	if err := d.Check(p.Sections); err == nil || !strings.Contains(err.Error(), "alert cannot escape") {
		t.Fatal("cross-overlay alert did not fail at its original scope boundary", err)
	}
	root.Children = root.Children[:len(root.Children)-1]
	d.Nodes[d.Root] = root
	panel := d.Nodes["panelRoot"]
	panel.Children = append(panel.Children, s.ID)
	d.Nodes["panelRoot"] = panel
	if err := d.Check(p.Sections); err != nil {
		t.Fatal("owned overlay alert refused", err)
	}
}
