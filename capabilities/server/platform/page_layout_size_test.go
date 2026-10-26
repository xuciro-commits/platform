package platform

import (
	"encoding/json"
	"testing"
)

func TestPageLayoutSizing(t *testing.T) {
	ptr := func(v int) *int { return &v }
	makePage := func() (*PageDocument, []Section) {
		d, sections := nestedDocument()
		n := d.Nodes["root"]
		n.Size = &PageLayoutSize{Height: ptr(640)}
		d.Nodes["root"] = n
		n = d.Nodes["columns"]
		n.Size = &PageLayoutSize{Weight: ptr(1)}
		n.Gap = ptr(0)
		d.Nodes["columns"] = n
		n = d.Nodes["table"]
		n.Size = &PageLayoutSize{Weight: ptr(2), MinWidth: ptr(96), MaxWidth: ptr(640), Scroll: "auto"}
		d.Nodes["table"] = n
		n = d.Nodes["detail"]
		n.Size = &PageLayoutSize{Width: ptr(240)}
		d.Nodes["detail"] = n
		return d, sections
	}
	d, sections := makePage()
	if err := d.Check(sections); err != nil {
		t.Fatal(err)
	}
	// Wire bytes and member pruning retain sizing on original stable IDs.
	raw, _ := json.Marshal(d)
	var roundtrip PageDocument
	if err := json.Unmarshal(raw, &roundtrip); err != nil {
		t.Fatal(err)
	}
	visible := roundtrip.Visible(sections[:2])
	if *visible.Nodes["table"].Size.Weight != 2 || len(visible.Nodes["columns"].Children) != 1 || *visible.Nodes["columns"].Gap != 0 {
		t.Fatal("projection lost sizing or retained a hidden track")
	}
	for _, tc := range []struct {
		name  string
		patch func(*PageDocument)
	}{
		{"old profile", func(d *PageDocument) { d.UIProfile = "platform.page.v2.26" }},
		{"gap budget", func(d *PageDocument) { n := d.Nodes["columns"]; n.Gap = ptr(65); d.Nodes["columns"] = n }},
		{"widget gap", func(d *PageDocument) { n := d.Nodes["table"]; n.Gap = ptr(1); d.Nodes["table"] = n }},
		{"size budget", func(d *PageDocument) { d.Nodes["table"].Size.MinWidth = ptr(4097) }},
		{"negative size", func(d *PageDocument) { d.Nodes["table"].Size.MinWidth = ptr(-1) }},
		{"inverted range", func(d *PageDocument) { d.Nodes["table"].Size.MinWidth = ptr(700) }},
		{"fixed outside range", func(d *PageDocument) { d.Nodes["detail"].Size.MaxWidth = ptr(200) }},
		{"fixed and weighted", func(d *PageDocument) { d.Nodes["detail"].Size.Weight = ptr(1) }},
		{"root weight", func(d *PageDocument) { d.Nodes["root"].Size.Weight = ptr(1) }},
		{"excess weight", func(d *PageDocument) { d.Nodes["table"].Size.Weight = ptr(25) }},
		{"natural rows", func(d *PageDocument) { d.Nodes["root"].Size.Height = nil }},
		{"unbounded scroll", func(d *PageDocument) {
			n := d.Nodes["root"]
			n.Size = &PageLayoutSize{Scroll: "auto"}
			d.Nodes["root"] = n
		}},
		{"unsupported scroll", func(d *PageDocument) { d.Nodes["table"].Size.Scroll = "hidden" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, s := makePage()
			tc.patch(d)
			if d.Check(s) == nil {
				t.Fatal("accepted incompatible sizing")
			}
		})
	}
	// A column fills a bounded cross axis, so nested weighted rows have space.
	d, sections = makePage()
	n := d.Nodes["table"]
	n.Kind = "rows"
	n.Section = ""
	n.Children = []string{"row"}
	d.Nodes["table"] = n
	d.Nodes["row"] = PageLayoutNode{Kind: "widget", Section: "table", Size: &PageLayoutSize{Weight: ptr(3), Scroll: "auto"}}
	if err := d.Check(sections); err != nil {
		t.Fatal("lost inherited height", err)
	}
}
