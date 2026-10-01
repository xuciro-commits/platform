package platform

import (
	"encoding/json"
	"testing"
)

func overlayScopeDocument() (*PageDocument, []Section) {
	d, s := overlayDocument()
	d.UIProfile = PageUIProfile()
	d.Variables["local"] = PageVariable{Scope: "overlay", Owner: "panel", Type: "string", Mode: "state", Initial: json.RawMessage(`"initial"`)}
	d.Nodes["input"] = PageLayoutNode{Kind: "widget", Section: "input", ValueVariable: "local"}
	for _, overlay := range d.Overlays {
		n := d.Nodes[overlay.Root]
		n.Children = append(n.Children, "input")
		d.Nodes[overlay.Root] = n
	}
	return d, append(s, Section{ID: "input", Widget: "input", ConfigVersion: 1})
}

func TestOverlayScopeBindings(t *testing.T) {
	d, s := overlayScopeDocument()
	if err := d.Check(s); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		change func(*PageDocument)
	}{
		{"old profile", func(d *PageDocument) { d.UIProfile = "platform.page.v2.6" }},
		{"missing owner", func(d *PageDocument) { v := d.Variables["local"]; v.Owner = "missing"; d.Variables["local"] = v }},
		{"outside root", func(d *PageDocument) {
			for _, o := range d.Overlays {
				n := d.Nodes[o.Root]
				n.Children = n.Children[:len(n.Children)-1]
				d.Nodes[o.Root] = n
			}
			n := d.Nodes[d.Root]
			n.Children = append(n.Children, "input")
			d.Nodes[d.Root] = n
		}},
		{"constant input", func(d *PageDocument) { v := d.Variables["local"]; v.Mode = "constant"; d.Variables["local"] = v }},
		{"missing binding", func(d *PageDocument) { n := d.Nodes["input"]; n.ValueVariable = ""; d.Nodes["input"] = n }},
		{"page event writes overlay local", func(d *PageDocument) {
			d.Events[0].Target, d.Events[0].Value = "local", json.RawMessage(`"leak"`)
		}},
		{"page navigation returns to overlay local", func(d *PageDocument) {
			d.Events[0] = PageEventBinding{Source: "trigger", Event: "click", Navigate: &PageNavigation{
				Page: AssetRef{App: "build", Kind: AssetPage, Name: "handler"}, InterfaceVersion: 1, Results: map[string]string{"result": "local"},
			}}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			d, s := overlayScopeDocument()
			test.change(d)
			if d.Check(s) == nil {
				t.Fatal("invalid scope accepted")
			}
		})
	}
	visible := d.Visible(s[:len(s)-2]) // Hide every Overlay body section.
	for _, v := range visible.Variables {
		if v.Scope == "overlay" {
			t.Fatal("hidden overlay left local declarations")
		}
	}
}
