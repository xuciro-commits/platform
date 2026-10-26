package platform

import (
	"encoding/json"
	"testing"
)

func TestButtonGroupControlsAndBindings(t *testing.T) {
	d, sections := overlayDocument()
	d.UIProfile = PageUIProfile()
	sections[len(sections)-2].Widget = "button-group"
	sections[len(sections)-2].Buttons = []PageButton{{ID: "open", Title: "Open", Variant: "primary", Icon: "arrow"}, {ID: "close", Title: "Close", Variant: "danger", Icon: "trash"}}
	d.Events = []PageEventBinding{{Source: "trigger", Control: "open", Event: "click", Effects: []PageEffect{{Kind: "set", Target: "open", Value: json.RawMessage(`true`)}}}, {Source: "trigger", Control: "close", Event: "click", Effects: []PageEffect{{Kind: "set", Target: "open", Value: json.RawMessage(`false`)}}}}
	if err := d.Check(sections); err != nil {
		t.Fatal(err)
	}
	d.UIProfile = "platform.page.v2.36"
	if d.Check(sections) == nil {
		t.Fatal("old profile accepted controls")
	}
	d.UIProfile = PageUIProfile()
	original := append([]PageEventBinding{}, d.Events...)
	d.Events = d.Events[:1]
	if d.Check(sections) == nil {
		t.Fatal("missing control binding accepted")
	}
	d.Events = append(original, original[0])
	if d.Check(sections) == nil {
		t.Fatal("duplicate control binding accepted")
	}
	d.Events = original
	d.Events[0].Control = "unknown"
	if d.Check(sections) == nil {
		t.Fatal("unknown control accepted")
	}
	d.Events[0].Control = "open"
	sections[len(sections)-2].Buttons[0].Icon = "script"
	if d.Check(sections) == nil {
		t.Fatal("unsupported icon accepted")
	}
	sections[len(sections)-2].Buttons[0].Icon = "arrow"
	projected := d.Visible(sections[:len(sections)-1])
	if len(projected.Events) != 0 {
		t.Fatal("hidden overlay retained group events")
	}
	if _, ok := projected.Nodes["trigger"]; ok {
		t.Fatal("group without visible controls retained")
	}
}
