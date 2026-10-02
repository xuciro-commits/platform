package platform

import (
	"encoding/json"
	"slices"
	"testing"
)

func pauseLeaf(d *PageDocument, leaf, parent string) {
	n := d.Nodes[parent]
	n.Children = slices.DeleteFunc(slices.Clone(n.Children), func(id string) bool { return id == leaf })
	d.Nodes[parent] = n
	d.UnusedWidgets = append(d.UnusedWidgets, PageUnusedWidget{Node: leaf, Parent: parent})
}

func TestUnusedWidgetOwnershipAndProjection(t *testing.T) {
	makePage := func() (*PageDocument, []Section) {
		d, s := nestedDocument()
		pauseLeaf(d, "detail", "columns")
		return d, s
	}
	d, s := makePage()
	if err := d.Check(s); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*PageDocument)
	}{
		{"old profile", func(d *PageDocument) { d.UIProfile = "platform.page.v2.27" }},
		{"duplicate", func(d *PageDocument) { d.UnusedWidgets = append(d.UnusedWidgets, d.UnusedWidgets[0]) }},
		{"placed and unused", func(d *PageDocument) {
			n := d.Nodes["columns"]
			n.Children = append(n.Children, "detail")
			d.Nodes["columns"] = n
		}},
		{"missing leaf", func(d *PageDocument) { d.UnusedWidgets[0].Node = "missing" }},
		{"container leaf", func(d *PageDocument) { d.UnusedWidgets[0].Node = "columns" }},
		{"missing parent", func(d *PageDocument) { d.UnusedWidgets[0].Parent = "missing" }},
		{"widget parent", func(d *PageDocument) { d.UnusedWidgets[0].Parent = "table" }},
		{"inventory budget", func(d *PageDocument) {
			for range 128 {
				d.UnusedWidgets = append(d.UnusedWidgets, d.UnusedWidgets[0])
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, s := makePage()
			tc.change(d)
			if d.Check(s) == nil {
				t.Fatal("accepted invalid dormant ownership")
			}
		})
	}
	visible := d.Visible(s[:2])
	if len(visible.UnusedWidgets) != 0 {
		t.Fatal("hidden widget inventory leaked")
	}
	if _, ok := visible.Nodes["detail"]; ok {
		t.Fatal("hidden unused node leaked")
	}
	visible = d.Visible(s[2:])
	if len(visible.UnusedWidgets) != 1 || len(visible.Nodes["columns"].Children) != 0 {
		t.Fatal("authorized dormant ownership lost")
	}
	raw, _ := json.Marshal(d)
	var restored PageDocument
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	if err := restored.Check(s); err != nil {
		t.Fatal("wire roundtrip", err)
	}
	// All widgets may be staged while the existing page remains structurally valid.
	d, s = nestedDocument()
	pauseLeaf(d, "heading", "root")
	pauseLeaf(d, "table", "columns")
	pauseLeaf(d, "detail", "columns")
	if err := d.Check(s); err != nil {
		t.Fatal("all-unused page", err)
	}
}

func TestUnusedWidgetRetainsLocalScope(t *testing.T) {
	d, s := overlayScopeDocument()
	parent := d.Overlays["panel"].Root
	pauseLeaf(d, "input", parent)
	if err := d.Check(s); err != nil {
		t.Fatal(err)
	}
	if d.overlayOwners()["input"] != "panel" {
		t.Fatal("dormant input widened its scope")
	}
	d.UnusedWidgets[0].Parent = d.Root
	if d.Check(s) == nil {
		t.Fatal("unused input bypassed overlay-local authority")
	}
	d, s = loopDocument()
	pauseLeaf(d, "toggle", "loop")
	if err := d.Check(s); err != nil {
		t.Fatal(err)
	}
	if d.loopOwners()["toggle"] != "loop" {
		t.Fatal("dormant event lost item scope")
	}
	d.UnusedWidgets[0].Parent = d.Root
	if d.Check(s) == nil {
		t.Fatal("unused event bypassed item-local authority")
	}
}
