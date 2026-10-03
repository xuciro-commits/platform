package platform

import (
	"strings"
	"testing"
)

func TestRangeOriginalDraftsProfileAndOwnership(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	d.Variables["lower"] = PageVariable{Scope: "page", Type: "string", Mode: "state", Initial: Raw("")}
	d.Variables["upper"] = PageVariable{Scope: "page", Type: "string", Mode: "state", Initial: Raw("")}
	s := Section{ID: "range", Widget: "range-input", ConfigVersion: 1, RangeMinVariable: "lower", RangeMaxVariable: "upper", RangeInput: &PageRangeInput{Min: "0", Max: "45", Step: "1", Label: "Pressure", Unit: "bar"}}
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	root := d.Nodes[d.Root]
	root.Children = append(root.Children, s.ID)
	d.Nodes[d.Root] = root
	p.Sections = append(p.Sections, s)
	if err := d.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	if (*PageDocument)(nil).Check([]Section{s}) == nil {
		t.Fatal("range without document accepted")
	}
	d.UIProfile = "platform.page.v2.51"
	if d.Check(p.Sections) == nil {
		t.Fatal("old profile accepted")
	}
	d.UIProfile = PageUIProfile()
	for _, r := range []PageRangeInput{{Min: "0", Max: "1", Step: "0.3"}, {Min: "0", Max: "10001", Step: "1"}, {Min: "0", Max: "1", Step: "0"}, {Min: "-0", Max: "1", Step: "1"}, {Min: "1", Max: "0", Step: "1"}, {Min: "0", Max: "1e3", Step: "1"}} {
		s.RangeInput = &r
		if d.checkRangeInput(s) == nil {
			t.Fatalf("invalid grid accepted: %+v", r)
		}
	}
	s.RangeInput = &PageRangeInput{Min: "-0.3", Max: "0.3", Step: "0.1"}
	if err := d.checkRangeInput(s); err != nil {
		t.Fatal(err)
	}
	s.RangeInput = &PageRangeInput{Min: "1" + strings.Repeat("0", 126), Max: "1" + strings.Repeat("0", 125) + "1", Step: "0.01"}
	if d.checkRangeInput(s) == nil {
		t.Fatal("generated draft exceeds decimal budget")
	}
	s.RangeInput = &PageRangeInput{Min: "-0.3", Max: "0.3", Step: "0.1"}
	s.RangeMinVariable = "upper"
	if d.checkRangeInput(s) == nil {
		t.Fatal("same state pair accepted")
	}
	s.RangeMinVariable = "lower"
	for _, v := range []PageVariable{{Scope: "page", Type: "string", Mode: "constant", Initial: Raw("")}, {Scope: "page", Type: "decimal", Mode: "state", Initial: Raw(DecimalValue{Kind: "decimal", Value: "0"})}, {Scope: "overlay", Owner: "other", Type: "string", Mode: "state", Initial: Raw("")}} {
		d.Variables["upper"] = v
		if d.checkRangeInput(s) == nil {
			t.Fatal("incompatible state owner accepted")
		}
	}
}

func TestRangeOverlayOwnershipCannotEscape(t *testing.T) {
	d, sections := overlayScopeDocument()
	d.Variables["upper"] = PageVariable{Scope: "overlay", Owner: "panel", Type: "string", Mode: "state", Initial: Raw("")}
	s := Section{ID: "range", Widget: "range-input", ConfigVersion: 1, RangeMinVariable: "local", RangeMaxVariable: "upper", RangeInput: &PageRangeInput{Min: "0", Max: "45", Step: "1"}}
	sections = append(sections, s)
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	overlay := d.Overlays["panel"]
	root := d.Nodes[overlay.Root]
	root.Children = append(root.Children, s.ID)
	d.Nodes[overlay.Root] = root
	if err := d.Check(sections); err != nil {
		t.Fatal(err)
	}
	root.Children = root.Children[:len(root.Children)-1]
	d.Nodes[overlay.Root] = root
	main := d.Nodes[d.Root]
	main.Children = append(main.Children, s.ID)
	d.Nodes[d.Root] = main
	if d.Check(sections) == nil {
		t.Fatal("overlay range escaped into page")
	}
}
