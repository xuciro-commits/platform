package platform

import (
	"strings"
	"testing"
)

func indexedChoicePage(variant, mode string) Page {
	p := queryPlanPage()
	d := p.Document
	d.Variables["index"] = PageVariable{Scope: "page", Type: "string", Mode: mode, Initial: Raw("0")}
	s := Section{ID: "indexChoice", Widget: "choice-input", ConfigVersion: 1, ChoiceVariable: "index", ChoiceInput: &PageChoiceInput{Variant: variant, Options: []string{"0", "1", "2"}, OptionLabels: []string{"Same", "Same", ""}}}
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	n := d.Nodes[d.Root]
	n.Children = append(n.Children, s.ID)
	d.Nodes[d.Root] = n
	p.Sections = append(p.Sections, s)
	return p
}
func TestIndexedChoicesKeepOriginalStateAndReadonlyConstant(t *testing.T) {
	for _, variant := range []string{"steps", "tabs"} {
		t.Run(variant, func(t *testing.T) {
			for _, mode := range []string{"state", "constant"} {
				p := indexedChoicePage(variant, mode)
				if err := p.Document.Check(p.Sections); err != nil {
					t.Fatal(mode, err)
				}
				if _, err := PageReleaseAsset("sample", "v1", p); err != nil {
					t.Fatal("cannot freeze original indexed choices", err)
				}
				v := p.Document.Variables["index"]
				v.Initial = Raw("retired")
				p.Document.Variables["index"] = v
				if err := p.Document.Check(p.Sections); err != nil {
					t.Fatal("unmatched original value must remain visible without replacement", err)
				}
				p.Document.UIProfile = "platform.page.v2.74"
				if p.Document.Check(p.Sections) == nil {
					t.Fatal("old profile accepted indexed presentation")
				}
			}
			for _, bad := range []struct {
				name   string
				change func(*Page)
			}{
				{"labels missing", func(p *Page) { p.Sections[len(p.Sections)-1].ChoiceInput.OptionLabels = nil }},
				{"empty indices", func(p *Page) {
					p.Sections[len(p.Sections)-1].ChoiceInput.Options = []string{}
					p.Sections[len(p.Sections)-1].ChoiceInput.OptionLabels = []string{}
				}},
				{"labels mismatched", func(p *Page) { p.Sections[len(p.Sections)-1].ChoiceInput.OptionLabels = []string{"one"} }},
				{"oversized label", func(p *Page) { p.Sections[len(p.Sections)-1].ChoiceInput.OptionLabels[0] = strings.Repeat("界", 86) }},
				{"noncanonical values", func(p *Page) { p.Sections[len(p.Sections)-1].ChoiceInput.Options = []string{"1", "2", "3"} }},
				{"set output", func(p *Page) {
					s := &p.Sections[len(p.Sections)-1]
					s.ChoiceSetVariable = s.ChoiceVariable
					s.ChoiceVariable = ""
				}},
				{"clear", func(p *Page) { p.Sections[len(p.Sections)-1].ChoiceInput.Clearable = true }},
				{"boolean state", func(p *Page) {
					p.Document.Variables["index"] = PageVariable{Scope: "page", Type: "boolean", Mode: "state", Initial: Raw(false)}
				}},
				{"derived value", func(p *Page) {
					p.Document.Variables["index"] = PageVariable{Scope: "page", Type: "string", Mode: "derived", Expression: &PageExpression{Op: "concat", Args: []PageValue{{Literal: Raw("0")}, {Literal: Raw("")}}}}
				}},
			} {
				t.Run(bad.name, func(t *testing.T) {
					p := indexedChoicePage(variant, "state")
					bad.change(&p)
					if p.Document.Check(p.Sections) == nil {
						t.Fatal("invalid indexed choice accepted")
					}
				})
			}
		})
	}
	// Existing frozen variants never acquire display-label or readonly semantics.
	p := indexedChoicePage("select", "state")
	s := &p.Sections[len(p.Sections)-1]
	s.ChoiceInput.OptionLabels = []string{}
	if p.Document.Check(p.Sections) == nil {
		t.Fatal("legacy variant accepted indexed labels")
	}
	s.ChoiceInput.OptionLabels = nil
	if err := p.Document.Check(p.Sections); err != nil {
		t.Fatal("legacy value-only choices changed", err)
	}
	v := p.Document.Variables["index"]
	v.Mode = "constant"
	p.Document.Variables["index"] = v
	if p.Document.Check(p.Sections) == nil {
		t.Fatal("legacy variant gained readonly output")
	}
}
func TestIndexedChoiceExactPresentationOwner(t *testing.T) {
	for _, mode := range []string{"state", "constant"} {
		d, sections := overlayScopeDocument()
		v := PageVariable{Scope: "overlay", Owner: "panel", Type: "string", Mode: mode, Initial: Raw("0")}
		d.Variables["localIndex"] = v
		s := Section{ID: "tabsChoice", Widget: "choice-input", ConfigVersion: 1, ChoiceVariable: "localIndex", ChoiceInput: &PageChoiceInput{Variant: "tabs", Options: []string{"0", "1"}, OptionLabels: []string{"same", "same"}}}
		sections = append(sections, s)
		d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
		overlay := d.Overlays["panel"]
		n := d.Nodes[overlay.Root]
		n.Children = append(n.Children, s.ID)
		d.Nodes[overlay.Root] = n
		if err := d.Check(sections); err != nil {
			t.Fatal(mode, err)
		}
		// Original page state cannot silently replace a local selector's identity.
		v.Scope = "page"
		v.Owner = ""
		d.Variables["localIndex"] = v
		if d.Check(sections) == nil {
			t.Fatal("indexed choice acquired a different owner")
		}
	}
}
