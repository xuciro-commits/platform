package platform

import (
	"strings"
	"testing"
)

func TestBooleanInputOriginalStateProfileAndEmptyLabel(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	d.Variables["flag"] = PageVariable{Scope: "page", Type: "boolean", Mode: "state", Initial: Raw(false)}
	empty := ""
	s := Section{ID: "switch", Widget: "boolean-input", ConfigVersion: 1, BooleanVariable: "flag", BooleanLabel: &empty}
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	root := d.Nodes[d.Root]
	root.Children = append(root.Children, s.ID)
	d.Nodes[d.Root] = root
	p.Sections = append(p.Sections, s)
	if err := d.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(Raw(s)), `"booleanLabel":""`) {
		t.Fatal("intentional empty label lost")
	}
	if (*PageDocument)(nil).Check([]Section{s}) == nil {
		t.Fatal("boolean input without document accepted")
	}
	d.UIProfile = "platform.page.v2.52"
	if d.Check(p.Sections) == nil {
		t.Fatal("old profile accepted switch")
	}
	d.UIProfile = PageUIProfile()
	for _, v := range []PageVariable{{Scope: "page", Type: "boolean", Mode: "constant", Initial: Raw(false)}, {Scope: "page", Type: "string", Mode: "state", Initial: Raw("false")}, {Scope: "application", Type: "boolean", Mode: "shared", Writable: true}} {
		d.Variables["flag"] = v
		if d.checkBooleanInput(s) == nil {
			t.Fatal("coercion or different state owner accepted")
		}
	}
	d.Variables["flag"] = PageVariable{Scope: "page", Type: "boolean", Mode: "state", Initial: Raw(false)}
	long := strings.Repeat("a", 1025)
	s.BooleanLabel = &long
	if d.checkBooleanInput(s) == nil {
		t.Fatal("unbounded label accepted")
	}
	s.BooleanLabel = nil
	s.Widget = "text"
	if d.checkBooleanInput(s) == nil {
		t.Fatal("binding escaped widget")
	}
}

func TestBooleanInputOriginalOverlayAndEnablePort(t *testing.T) {
	d, sections := overlayScopeDocument()
	d.Variables["flag"] = PageVariable{Scope: "overlay", Owner: "panel", Type: "boolean", Mode: "state", Initial: Raw(false)}
	d.Variables["enabled"] = PageVariable{Scope: "overlay", Owner: "panel", Type: "boolean", Mode: "state", Initial: Raw(true)}
	s := Section{ID: "switch", Widget: "boolean-input", ConfigVersion: 1, BooleanVariable: "flag"}
	sections = append(sections, s)
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID, EnabledWhen: "enabled"}
	o := d.Overlays["panel"]
	root := d.Nodes[o.Root]
	root.Children = append(root.Children, s.ID)
	d.Nodes[o.Root] = root
	if err := d.Check(sections); err != nil {
		t.Fatal(err)
	}
	root.Children = root.Children[:len(root.Children)-1]
	d.Nodes[o.Root] = root
	main := d.Nodes[d.Root]
	main.Children = append(main.Children, s.ID)
	d.Nodes[d.Root] = main
	if d.Check(sections) == nil {
		t.Fatal("overlay switch and enabled state escaped into page")
	}
}
