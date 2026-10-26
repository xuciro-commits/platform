package platform

import (
	"encoding/json"
	"testing"
)

func slotDocument() (*PageDocument, []Section) {
	d, sections := nestedDocument()
	n := d.Nodes["table"]
	n.Children = []string{"toolbar", "footer"}
	d.Nodes["table"] = n
	d.Nodes["toolbar"] = PageLayoutNode{Kind: "toolbar", Slot: "toolbar"}
	d.Nodes["footer"] = PageLayoutNode{Kind: "flow", Slot: "footer", Children: []string{"control"}}
	d.Nodes["control"] = PageLayoutNode{Kind: "widget", Section: "control"}
	return d, append(sections, Section{ID: "control", Widget: "text", ConfigVersion: 1, Text: "Original footer"})
}

func TestPageSlotsUseTheOriginalOwnedTree(t *testing.T) {
	d, sections := slotDocument()
	if err := d.Check(sections); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*PageDocument){
		func(d *PageDocument) { d.UIProfile = "platform.page.v2.103" },
		func(d *PageDocument) { n := d.Nodes["footer"]; n.Slot = "unknown"; d.Nodes["footer"] = n },
		func(d *PageDocument) { n := d.Nodes["footer"]; n.Slot = "toolbar"; d.Nodes["footer"] = n },
		func(d *PageDocument) { n := d.Nodes["footer"]; n.Kind = "tabs"; d.Nodes["footer"] = n },
		func(d *PageDocument) { n := d.Nodes["footer"]; n.Slot = ""; d.Nodes["footer"] = n },
		func(d *PageDocument) {
			n := d.Nodes["root"]
			n.Children = append(n.Children, "footer")
			d.Nodes["root"] = n
		},
		func(d *PageDocument) { n := d.Nodes["control"]; n.Children = []string{"table"}; d.Nodes["control"] = n },
	} {
		next, s := slotDocument()
		change(next)
		if err := next.Check(s); err == nil {
			t.Fatal("invalid slot tree was accepted")
		}
	}
	bytes, _ := json.Marshal(d)
	var copy PageDocument
	if err := json.Unmarshal(bytes, &copy); err != nil {
		t.Fatal(err)
	}
	if copy.Nodes["footer"].Slot != "footer" {
		t.Fatal("slot identity was lost")
	}
	if err := copy.Check(sections); err != nil {
		t.Fatal(err)
	}
}

func TestPageSlotProjectionRetainsEmptySlotsAndRemovesPrivateCompositeChildren(t *testing.T) {
	d, s := slotDocument()
	visible := d.Visible(s)
	if visible.Nodes["toolbar"].Slot != "toolbar" || visible.Nodes["control"].Section != "control" {
		t.Fatal("slot projection dropped owned nodes")
	}
	withoutChild := d.Visible(s[:len(s)-1])
	if _, ok := withoutChild.Nodes["control"]; ok {
		t.Fatal("private slot child remained")
	}
	if withoutChild.Nodes["footer"].Slot != "footer" {
		t.Fatal("empty declared slot was lost")
	}
	var allowed []Section
	for _, section := range s {
		if section.ID != "table" {
			allowed = append(allowed, section)
		}
	}
	hidden := d.Visible(allowed)
	for _, id := range []string{"table", "toolbar", "footer", "control"} {
		if _, ok := hidden.Nodes[id]; ok {
			t.Fatalf("removed composite leaked %s", id)
		}
	}
}
