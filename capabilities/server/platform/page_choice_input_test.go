package platform

import (
	"strings"
	"testing"
)

func TestChoiceInputOriginalStateAndStaticOptions(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	d.Variables["choice"] = PageVariable{Scope: "page", Type: "string", Mode: "state", Initial: Raw("retired")}
	s := Section{ID: "choice", Widget: "choice-input", ConfigVersion: 1, ChoiceVariable: "choice", ChoiceInput: &PageChoiceInput{Variant: "select", Options: []string{"Open", "Closed"}}}
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	root := d.Nodes[d.Root]
	root.Children = append(root.Children, s.ID)
	d.Nodes[d.Root] = root
	p.Sections = append(p.Sections, s)
	if err := d.Check(p.Sections); err != nil {
		t.Fatal("unmatched draft must remain valid state", err)
	}
	if (*PageDocument)(nil).Check([]Section{s}) == nil {
		t.Fatal("choice without document accepted")
	}
	d.UIProfile = "platform.page.v2.54"
	if d.Check(p.Sections) == nil {
		t.Fatal("old profile accepted choice")
	}
	d.UIProfile = PageUIProfile()
	for _, c := range []PageChoiceInput{{Variant: "other", Options: []string{}}, {Variant: "select", Options: nil}, {Variant: "radio", Options: []string{"A", "A"}}, {Variant: "segments", Options: []string{""}}, {Variant: "select", Options: []string{strings.Repeat("a", 257)}}} {
		s.ChoiceInput = &c
		if d.checkChoiceInput(s) == nil {
			t.Fatal("invalid static choices accepted", c)
		}
	}
	s.ChoiceInput = &PageChoiceInput{Variant: "select", Options: []string{}}
	if err := d.checkChoiceInput(s); err != nil {
		t.Fatal("empty source options rejected", err)
	}
	for _, v := range []PageVariable{{Scope: "page", Type: "string", Mode: "constant", Initial: Raw("")}, {Scope: "page", Type: "boolean", Mode: "state", Initial: Raw(false)}, {Scope: "application", Type: "string", Mode: "shared", Writable: true}} {
		d.Variables["choice"] = v
		if d.checkChoiceInput(s) == nil {
			t.Fatal("invalid state owner or coercion accepted")
		}
	}
}

func TestChoiceInputOverlayCannotEscape(t *testing.T) {
	d, sections := overlayScopeDocument()
	s := Section{ID: "choice", Widget: "choice-input", ConfigVersion: 1, ChoiceVariable: "local", ChoiceInput: &PageChoiceInput{Variant: "radio", Options: []string{"A", "B"}}}
	sections = append(sections, s)
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID, EnabledWhen: "open"}
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
		t.Fatal("overlay choice escaped")
	}
}

func TestMultipleChoiceOriginalSetProfileAndPorts(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	d.Variables["pick"] = PageVariable{Scope: "page", Type: "string-set", Mode: "state", Initial: Raw(map[string]any{"kind": "string-set", "values": []string{"retired"}})}
	s := Section{ID: "multi", Widget: "choice-input", ConfigVersion: 1, ChoiceSetVariable: "pick", ChoiceInput: &PageChoiceInput{Variant: "multiple", Options: []string{"A", "B"}}}
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	root := d.Nodes[d.Root]
	root.Children = append(root.Children, s.ID)
	d.Nodes[d.Root] = root
	p.Sections = append(p.Sections, s)
	if err := d.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	d.UIProfile = "platform.page.v2.55"
	if d.Check(p.Sections) == nil {
		t.Fatal("old profile accepted multiple")
	}
	d.UIProfile = PageUIProfile()
	s.ChoiceVariable = "pick"
	if d.checkChoiceInput(s) == nil {
		t.Fatal("dual or coerced ports accepted")
	}
	s.ChoiceVariable = ""
	s.ChoiceInput.Variant = "select"
	if d.checkChoiceInput(s) == nil {
		t.Fatal("single presentation accepted set port")
	}
	s.ChoiceInput.Variant = "multiple"
	v := d.Variables["pick"]
	v.Type = "record-set"
	d.Variables["pick"] = v
	if d.checkChoiceInput(s) == nil {
		t.Fatal("record references coerced to choices")
	}
	v.Type = "string-set"
	v.Mode = "constant"
	d.Variables["pick"] = v
	if d.checkChoiceInput(s) == nil {
		t.Fatal("readonly set accepted")
	}
}
