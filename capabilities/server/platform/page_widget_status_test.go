package platform

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// page_alert_test.go

func TestAlertUsesOriginalDecimalPortAndBoundedConfiguration(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	d.Variables["count"] = PageVariable{Scope: "page", Type: "decimal", Mode: "aggregate", Source: &PageResourceSource{Kind: "count", Query: "read"}}
	s := Section{PageWidgetConfig: PageWidgetConfig{AlertValueVariable: "count", AlertBanner: &PageAlertBanner{Threshold: "0", Tone: "danger", Message: "{value} affected records"}}, ID: "alert", Widget: "alert-banner", ConfigVersion: 1}
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
		if copy.checkWidgetConfiguration() == nil && d.checkAlertBanner(copy) == nil {
			t.Fatal("invalid threshold accepted", threshold)
		}
	}
	for _, change := range []func(*Section){func(s *Section) { s.AlertBanner = nil }, func(s *Section) { s.AlertBanner.Tone = "custom" }, func(s *Section) { s.AlertBanner.Message = "" }, func(s *Section) { s.AlertBanner.Message = strings.Repeat("中", 1366) }, func(s *Section) { s.AlertValueVariable = "" }, func(s *Section) { s.Widget = "text" }, func(s *Section) { s.CollectionVariable = "window" }, func(s *Section) { s.Actions = []AssetRef{{App: "sample", Kind: AssetAction, Name: "execute"}} }} {
		copy := s
		cfg := *s.AlertBanner
		copy.AlertBanner = &cfg
		change(&copy)
		if copy.checkWidgetConfiguration() == nil && d.checkAlertBanner(copy) == nil {
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
	s := Section{PageWidgetConfig: PageWidgetConfig{AlertValueVariable: "local", AlertBanner: &PageAlertBanner{Threshold: "0", Tone: "warning", Message: "{value} affected"}}, ID: "alert", Widget: "alert-banner", ConfigVersion: 1}
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	root := d.Nodes[d.Root]
	root.Children = append(root.Children, s.ID)
	d.Nodes[d.Root] = root
	p.Sections = append(p.Sections, s)
	if err := d.Check(p.Sections); err == nil || !strings.Contains(err.Error(), "alertValueVariable escapes its presentation owner") {
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

// page_notice_test.go

func TestNoticeStaticTextProfilesAndBudgets(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	empty := ""
	s := Section{PageWidgetConfig: PageWidgetConfig{Notice: &PageNotice{Title: &empty, Message: "<img src=x> {value} **literal**", Tone: "info"}}, ID: "note", Widget: "notice", ConfigVersion: 1}
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	root := d.Nodes[d.Root]
	root.Children = append(root.Children, s.ID)
	d.Nodes[d.Root] = root
	p.Sections = append(p.Sections, s)
	if err := d.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	if (*PageDocument)(nil).Check([]Section{s}) == nil {
		t.Fatal("notice without document accepted")
	}
	d.UIProfile = "platform.page.v2.60"
	if d.Check(p.Sections) == nil {
		t.Fatal("old profile accepted notice")
	}
	d.UIProfile = PageUIProfile()
	for _, tone := range []string{"info", "success", "warning", "danger"} {
		s.Notice.Tone = tone
		s.Notice.Message = ""
		s.Notice.Title = nil
		if err := d.checkNotice(s); err != nil {
			t.Fatal("valid empty note refused", tone, err)
		}
	}
	for _, change := range []func(*Section){func(s *Section) { s.Notice = nil }, func(s *Section) { s.Notice.Tone = "primary" }, func(s *Section) { v := strings.Repeat("中", 342); s.Notice.Title = &v }, func(s *Section) { s.Notice.Message = strings.Repeat("中", 1366) }, func(s *Section) { s.Widget = "text" }, func(s *Section) { s.CollectionVariable = "window" }, func(s *Section) { s.Fields = []string{"secret"} }, func(s *Section) { s.Actions = []AssetRef{{App: "sample", Kind: AssetAction, Name: "execute"}} }} {
		copy := s
		cfg := *s.Notice
		copy.Notice = &cfg
		change(&copy)
		if copy.checkWidgetConfiguration() == nil && d.checkNotice(copy) == nil {
			t.Fatal("invalid note accepted", copy)
		}
	}
}

// page_progress_test.go

func TestProgressTypedPortsDenominatorAndOldProfile(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	d.Variables["count"] = PageVariable{Scope: "page", Type: "decimal", Mode: "aggregate", Source: &PageResourceSource{Kind: "count", Query: d.Variables["window"].Source.Query}}
	s := Section{PageWidgetConfig: PageWidgetConfig{ProgressValueVariable: "count", ProgressTotal: "400", ProgressLabel: "Open work orders"}, ID: "progress", Widget: "progress", ConfigVersion: 1}
	p.Sections = append(p.Sections, s)
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	root := d.Nodes[d.Root]
	root.Children = append(root.Children, s.ID)
	d.Nodes[d.Root] = root
	if err := d.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	for _, total := range []string{"0", "-1", "1e3", "400.0", "bad"} {
		s.ProgressTotal = total
		if d.checkProgress(s) == nil {
			t.Fatal("invalid fixed denominator accepted")
		}
	}
	s.ProgressTotal = "400"
	s.ProgressTotalVariable = "count"
	if d.checkProgress(s) == nil {
		t.Fatal("two denominators accepted")
	}
	s.ProgressTotal = ""
	if err := d.checkProgress(s); err != nil {
		t.Fatal(err)
	}
	s.ProgressValueVariable = "window"
	if d.checkWidgetPorts(s) == nil {
		t.Fatal("window substituted for scalar")
	}
	s.ProgressValueVariable = "count"
	d.UIProfile = "platform.page.v2.47"
	if d.Check(p.Sections) == nil {
		t.Fatal("old profile accepted progress")
	}
	d.UIProfile = PageUIProfile()
	d.Variables["text"] = PageVariable{Scope: "page", Type: "string", Mode: "state", Initial: Raw("0.1")}
	d.Variables["parsed"] = PageVariable{Scope: "page", Type: "decimal", Mode: "derived", Expression: &PageExpression{Op: "parse-decimal", Args: []PageValue{{Variable: "text"}}}}
	if err := d.CheckVariables(); err != nil {
		t.Fatal(err)
	}
	d.UIProfile = "platform.page.v2.47"
	if d.CheckVariables() == nil {
		t.Fatal("old profile accepted decimal parsing")
	}
	if (*PageDocument)(nil).Check([]Section{s}) == nil {
		t.Fatal("progress without document accepted")
	}
}

// page_status_tracker_test.go

func TestStatusTrackerProfileOriginalSchemaAndFrozenStates(t *testing.T) {
	info := EntityInfo{App: "sample", Type: "sample.item", Fields: []FieldInfo{{Name: "state", Type: "choice"}}, Lifecycle: &LifecycleInfo{Field: "state", States: []State{{Name: "open", Title: "Open"}, {Name: "done", Title: "Done"}}}}
	s := Section{PageWidgetConfig: PageWidgetConfig{StatusTracker: &PageStatusTracker{Field: "state", Stages: []string{"done", "open"}}}, ID: "status", Widget: "status-tracker", ConfigVersion: 1}
	d := &PageDocument{FormatVersion: 2, UIProfile: PageUIProfile(), Root: "root", Nodes: map[string]PageLayoutNode{"root": {Kind: "rows", Children: []string{"status"}}, "status": {Kind: "widget", Section: "status"}}}
	if d.Check([]Section{s}) != nil || s.CheckStatusTracker(info) != nil {
		t.Fatal("valid original lifecycle refused")
	}
	if (*PageDocument)(nil).Check([]Section{s}) == nil {
		t.Fatal("tracker without a document accepted")
	}
	for _, change := range []func(*Section){func(s *Section) { s.StatusTracker = nil }, func(s *Section) { s.StatusTracker.Stages = nil }, func(s *Section) { s.StatusTracker.Stages = []string{"open", "open"} }, func(s *Section) { s.Widget = "detail" }, func(s *Section) {
		s.Actions = []AssetRef{{App: "sample", Kind: AssetAction, Name: "sample.item.close"}}
	}} {
		copy := s
		value := *s.StatusTracker
		value.Stages = slices.Clone(value.Stages)
		copy.StatusTracker = &value
		change(&copy)
		if d.Check([]Section{copy}) == nil {
			t.Fatal("invalid presentation accepted")
		}
	}
	d.UIProfile = "platform.page.v2.38"
	if d.Check([]Section{s}) == nil {
		t.Fatal("old profile accepted tracker")
	}
	d.UIProfile = PageUIProfile()
	hidden := info
	hidden.Fields = nil
	if s.CheckStatusTracker(hidden) == nil {
		t.Fatal("hidden status field accepted")
	}
	ref := AssetRef{App: info.App, Kind: AssetObject, Name: info.Type}
	p := Page{Name: "items", Title: "Items", Object: ref, Document: d, Sections: []Section{s}}
	page, err := PageReleaseAsset("sample", "1", p)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(info)
	object := ReleaseAsset{Ref: ref, SourceVersion: "1", ContractVersion: 1, Body: body}
	if _, err := Candidate([]AssetRef{page.Ref}, []ReleaseAsset{page, object}); err != nil {
		t.Fatal(err)
	}
	info.Lifecycle.States = info.Lifecycle.States[:1]
	body, _ = json.Marshal(info)
	object.Body = body
	if _, err := Candidate([]AssetRef{page.Ref}, []ReleaseAsset{page, object}); err == nil {
		t.Fatal("frozen unknown state accepted")
	}
}
