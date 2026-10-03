package platform

import "testing"

func treemapPage() Page {
	p := queryPlanPage()
	d := p.Document
	s := Section{ID: "tree", Widget: "treemap", ConfigVersion: 1, CollectionVariable: "window", Group: "state", GroupSetVariable: "chosen"}
	d.Variables["chosen"] = PageVariable{Scope: "page", Type: "string-set", Mode: "state", Initial: Raw(map[string]any{"kind": "string-set", "values": []string{}})}
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	root := d.Nodes[d.Root]
	root.Children = append(root.Children, s.ID)
	d.Nodes[d.Root] = root
	p.Sections = append(p.Sections, s)
	return p
}
func TestTreemapOriginalCountAndTypedFilterOwner(t *testing.T) {
	p := treemapPage()
	if err := p.Document.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	i := len(p.Sections) - 1
	for _, change := range []func(*Page){func(p *Page) { p.Document.UIProfile = "platform.page.v2.70" }, func(p *Page) { p.Sections[i].GroupValueVariable = "chosen" }, func(p *Page) { p.Sections[i].Widget = "term-counts" }, func(p *Page) { p.Sections[i].Group = "count" }, func(p *Page) {
		v := p.Document.Variables["chosen"]
		v.Mode = "constant"
		p.Document.Variables["chosen"] = v
	}, func(p *Page) {
		v := p.Document.Variables["chosen"]
		v.Scope = "overlay"
		v.Owner = "other"
		p.Document.Variables["chosen"] = v
	}} {
		p := treemapPage()
		change(&p)
		if p.Document.Check(p.Sections) == nil {
			t.Fatal("invalid treemap accepted")
		}
	}
	s := p.Sections[i]
	if (*PageDocument)(nil).Check([]Section{s}) == nil {
		t.Fatal("documentless treemap accepted")
	}
	info := EntityInfo{Fields: []FieldInfo{{Name: "state", Type: "choice"}}}
	if err := s.CheckTerms(info); err != nil {
		t.Fatal(err)
	}
	info.Fields = nil
	if s.CheckTerms(info) == nil {
		t.Fatal("hidden grouping accepted")
	}
}
