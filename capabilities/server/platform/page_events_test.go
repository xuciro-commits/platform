package platform

import (
	"encoding/json"
	"strings"
	"testing"
)

func overlayDocument() (*PageDocument, []Section) {
	d, sections := nestedDocument()
	d.Variables = map[string]PageVariable{"open": {Scope: "page", Type: "boolean", Mode: "state", Initial: json.RawMessage(`false`)}}
	d.Nodes["root"] = PageLayoutNode{Kind: "rows", Children: []string{"heading", "columns", "trigger"}}
	d.Nodes["trigger"] = PageLayoutNode{Kind: "widget", Section: "trigger"}
	d.Nodes["overlay"] = PageLayoutNode{Kind: "toolbar", Children: []string{"body"}, Align: "end"}
	d.Nodes["body"] = PageLayoutNode{Kind: "widget", Section: "body"}
	d.Overlays = map[string]PageOverlay{"panel": {Root: "overlay", Kind: "drawer", Title: "Record work", OpenVariable: "open"}}
	d.Events = []PageEventBinding{{Source: "trigger", Event: "click", Effects: []PageEffect{{Kind: "set", Target: "open", Value: json.RawMessage(`true`)}}}}
	return d, append(sections, Section{ID: "trigger", Widget: "button", ConfigVersion: 1}, Section{ID: "body", Widget: "detail", ConfigVersion: 1})
}

func TestPageOverlayForestAndTypedEvents(t *testing.T) {
	d, sections := overlayDocument()
	if err := d.Check(sections); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		change func(*PageDocument)
		want   string
	}{
		{"old profile", func(d *PageDocument) { d.UIProfile = "platform.page.v2.3" }, "v2.4"},
		{"shared root", func(d *PageDocument) { o := d.Overlays["panel"]; o.Root = d.Root; d.Overlays["panel"] = o }, "shared"},
		{"shared section", func(d *PageDocument) { d.Nodes["body"] = PageLayoutNode{Kind: "widget", Section: "detail"} }, "unique section"},
		{"missing root", func(d *PageDocument) { o := d.Overlays["panel"]; o.Root = "missing"; d.Overlays["panel"] = o }, "missing node"},
		{"initially open", func(d *PageDocument) {
			v := d.Variables["open"]
			v.Initial = json.RawMessage(`true`)
			d.Variables["open"] = v
		}, "initialized false"},
		{"missing click", func(d *PageDocument) { d.Events = nil }, "click binding"},
		{"duplicate click", func(d *PageDocument) { d.Events = append(d.Events, d.Events[0]) }, "one button"},
		{"wrong type", func(d *PageDocument) { d.Events[0].Effects[0].Value = json.RawMessage(`"true"`) }, "matching state"},
		{"missing target", func(d *PageDocument) { d.Events[0].Effects[0].Target = "missing" }, "matching state"},
		{"action event", func(d *PageDocument) { d.Events[0].Event = "submit" }, "one button"},
		{"business source", func(d *PageDocument) { d.Events[0].Source = "table" }, "one button"},
		{"invalid alignment", func(d *PageDocument) { n := d.Nodes["overlay"]; n.Align = "arbitrary"; d.Nodes["overlay"] = n }, "alignment"},
		{"enable detail", func(d *PageDocument) { n := d.Nodes["body"]; n.EnabledWhen = "open"; d.Nodes["body"] = n }, "enable binding"},
	} {
		t.Run(test.name, func(t *testing.T) {
			d, s := overlayDocument()
			test.change(d)
			if err := d.Check(s); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("want %q, got %v", test.want, err)
			}
		})
	}
}

func TestPageOverlayVisibilityRemovesDanglingTrigger(t *testing.T) {
	d, sections := overlayDocument()
	visible := d.Visible(sections[:len(sections)-1])
	if len(visible.Overlays) != 0 || len(visible.Events) != 0 {
		t.Fatal("hidden content retained overlay or event")
	}
	if _, ok := visible.Nodes["trigger"]; ok {
		t.Fatal("hidden overlay retained its trigger")
	}
	if _, ok := d.Nodes["trigger"]; !ok || len(d.Overlays) != 1 {
		t.Fatal("filter mutated installed document")
	}
	visible = d.Visible(sections)
	if len(visible.Overlays) != 1 || len(visible.Events) != 1 {
		t.Fatal("visible overlay lost its root or event")
	}
}

func TestOldPageProfilesRemainReadable(t *testing.T) {
	for _, profile := range []string{"platform.page.v2.1", "platform.page.v2.2", "platform.page.v2.3"} {
		d, sections := nestedDocument()
		d.UIProfile = profile
		if err := d.Check(sections); err != nil {
			t.Fatal(profile, err)
		}
	}
}

func TestPageClickCannotWriteUnknownTabIdentity(t *testing.T) {
	d, sections := overlayDocument()
	d.Nodes["columns"] = PageLayoutNode{Kind: "tabs", Children: []string{"table", "detail"}, ActiveVariable: "tab"}
	d.Variables["tab"] = PageVariable{Scope: "page", Type: "string", Mode: "state", Initial: json.RawMessage(`"table"`)}
	d.Events[0].Effects[0].Target, d.Events[0].Effects[0].Value = "tab", json.RawMessage(`"missing-child"`)
	if err := d.Check(sections); err == nil || !strings.Contains(err.Error(), "tab value") {
		t.Fatal(err)
	}
	d.Events[0].Effects[0].Value = json.RawMessage(`"detail"`)
	if err := d.Check(sections); err != nil {
		t.Fatal(err)
	}
}
