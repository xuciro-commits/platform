package platform

import (
	"fmt"
	"slices"
	"testing"
)

func recordComparisonPage() Page {
	object := AssetRef{App: "sample", Kind: AssetObject, Name: "sample.note"}
	return Page{Name: "compare", Object: object, Layout: "composed", Sections: []Section{
		{ID: "table", Widget: "table", ConfigVersion: 1, Fields: []string{"name"}, SelectionSetVariable: "picked"},
		{ID: "comparison", Widget: "record-comparison", ConfigVersion: 1, RecordSetVariable: "picked", RecordComparison: &PageRecordComparison{LabelField: "name"}, Fields: []string{"amount", "active"}},
	}, Document: &PageDocument{FormatVersion: 2, UIProfile: PageUIProfile(), Root: "root", Nodes: map[string]PageLayoutNode{
		"root": {Kind: "rows", Children: []string{"table", "comparison"}}, "table": {Kind: "widget", Section: "table"}, "comparison": {Kind: "widget", Section: "comparison"},
	}, Variables: map[string]PageVariable{"picked": {Scope: "page", Type: "record-set", Mode: "resource", Source: &PageResourceSource{Kind: "records", Section: "table"}}}}}
}
func TestRecordComparisonOriginalTableResource(t *testing.T) {
	p := recordComparisonPage()
	if err := p.Document.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	if err := p.CheckRecordPorts(); err != nil {
		t.Fatal(err)
	}
	if p.RecordSetVariableObject("picked") != p.Object {
		t.Fatal("record set lost original object identity")
	}
	cases := []struct {
		name   string
		change func(*Page)
	}{
		{"old profile", func(p *Page) { p.Document.UIProfile = "platform.page.v2.73" }},
		{"wrong widget", func(p *Page) { p.Sections[1].Widget = "detail" }},
		{"missing configuration", func(p *Page) { p.Sections[1].RecordComparison = nil }},
		{"empty title", func(p *Page) { p.Sections[1].RecordComparison.LabelField = "" }},
		{"empty fields", func(p *Page) { p.Sections[1].Fields = nil }},
		{"field budget", func(p *Page) {
			p.Sections[1].Fields = make([]string, 65)
			for i := range p.Sections[1].Fields {
				p.Sections[1].Fields[i] = fmt.Sprint("field", i)
			}
		}},
		{"duplicate field", func(p *Page) { p.Sections[1].Fields = []string{"amount", "amount"} }},
		{"missing input", func(p *Page) { p.Sections[1].RecordSetVariable = "missing" }},
		{"single record input", func(p *Page) {
			v := p.Document.Variables["picked"]
			v.Type = "record"
			v.Source.Kind = "record"
			p.Document.Variables["picked"] = v
		}},
		{"constant input", func(p *Page) {
			v := p.Document.Variables["picked"]
			v.Mode = "constant"
			v.Source = nil
			v.Initial = Raw([]string{})
			p.Document.Variables["picked"] = v
		}},
		{"missing table output", func(p *Page) { p.Sections[0].SelectionSetVariable = "" }},
		{"wrong table producer", func(p *Page) {
			v := p.Document.Variables["picked"]
			v.Source.Section = "comparison"
			p.Document.Variables["picked"] = v
		}},
		{"writes single selection", func(p *Page) { p.Sections[1].SelectionVariable = "picked" }},
		{"writes set selection", func(p *Page) { p.Sections[1].SelectionSetVariable = "picked" }},
		{"single record binding", func(p *Page) { p.Sections[1].RecordVariable = "picked" }},
		{"business action", func(p *Page) { p.Sections[1].Actions = []AssetRef{{App: "sample", Kind: AssetAction, Name: "save"}} }},
		{"collection input", func(p *Page) { p.Sections[1].CollectionVariable = "picked" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := recordComparisonPage()
			tc.change(&p)
			if p.Document.Check(p.Sections) == nil {
				t.Fatal("invalid record comparison accepted")
			}
		})
	}
	if (*PageDocument)(nil).Check(p.Sections) == nil {
		t.Fatal("documentless comparison accepted")
	}
	// Complete AssetRef identity, including owner, is retained by the input.
	for _, object := range []AssetRef{{App: "other", Kind: AssetObject, Name: "sample.note"}, {App: "sample", Kind: AssetObject, Name: "sample.other"}} {
		p := recordComparisonPage()
		p.Sections[1].Object = object
		if p.CheckRecordPorts() == nil {
			t.Fatal("foreign original object accepted")
		}
		if _, err := PageReleaseAsset("sample", "v1", p); err == nil {
			t.Fatal("frozen foreign object accepted")
		}
	}
}
func TestRecordComparisonOwnerAndVisibleProducer(t *testing.T) {
	p := recordComparisonPage()
	d := p.Document
	d.Nodes["root"] = PageLayoutNode{Kind: "rows", Children: []string{"trigger"}}
	d.Nodes["trigger"] = PageLayoutNode{Kind: "widget", Section: "trigger"}
	d.Nodes["panelRoot"] = PageLayoutNode{Kind: "rows", Children: []string{"table", "comparison"}}
	d.Variables["open"] = PageVariable{Scope: "page", Type: "boolean", Mode: "state", Initial: Raw(false)}
	d.Events = []PageEventBinding{{Source: "trigger", Event: "click", Effects: []PageEffect{{Kind: "set", Target: "open", Value: Raw(true)}}}}
	d.Overlays = map[string]PageOverlay{"panel": {Root: "panelRoot", Kind: "modal", Title: "Compare", OpenVariable: "open"}}
	p.Sections = append(p.Sections, Section{ID: "trigger", Widget: "button", ConfigVersion: 1})
	v := d.Variables["picked"]
	v.Scope = "overlay"
	v.Owner = "panel"
	d.Variables["picked"] = v
	if err := d.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	// A root consumer cannot use an overlay's authorized selection.
	n := d.Nodes["panelRoot"]
	n.Children = []string{"table"}
	d.Nodes["panelRoot"] = n
	n = d.Nodes["root"]
	n.Children = append(n.Children, "comparison")
	d.Nodes["root"] = n
	if d.Check(p.Sections) == nil {
		t.Fatal("record-set escaped its overlay owner")
	}
	// A page selection does not become an independent overlay selection.
	p = recordComparisonPage()
	d = p.Document
	d.Nodes["root"] = PageLayoutNode{Kind: "rows", Children: []string{"table"}}
	d.Nodes["panelRoot"] = PageLayoutNode{Kind: "rows", Children: []string{"comparison"}}
	d.Variables["open"] = PageVariable{Scope: "page", Type: "boolean", Mode: "state", Initial: Raw(false)}
	d.Overlays = map[string]PageOverlay{"panel": {Root: "panelRoot", Kind: "modal", Title: "Compare", OpenVariable: "open"}}
	if d.Check(p.Sections) == nil {
		t.Fatal("page selection acquired a different overlay owner")
	}
	p = recordComparisonPage()
	d = p.Document
	visible := d.Visible(p.Sections[1:])
	if _, ok := visible.Variables["picked"]; ok {
		t.Fatal("hidden table retained selected records resource")
	}
	for _, node := range visible.Nodes {
		if node.Section == "comparison" {
			t.Fatal("hidden table retained comparison consumer")
		}
	}
	// Even a page-owned original selection cannot enter a loop template.
	d.Queries = map[string]PageQuery{"read": {Object: p.Object, Limit: 2}}
	d.Variables["window"] = PageVariable{Scope: "page", Type: "object-set", Mode: "resource", Source: &PageResourceSource{Kind: "plan", Query: "read"}}
	d.Variables["item"] = PageVariable{Scope: "loop-item", Owner: "loop", Type: "record", Mode: "resource", Source: &PageResourceSource{Kind: "item", Node: "loop"}}
	d.Nodes["loop"] = PageLayoutNode{Kind: "loop", Children: []string{"comparison"}, Loop: &PageLoop{Collection: "window", ItemVariable: "item", Limit: 2}}
	d.Nodes["root"] = PageLayoutNode{Kind: "rows", Children: []string{"table", "loop"}}
	if d.Check(p.Sections) == nil {
		t.Fatal("comparison entered loop")
	}
}
func TestRecordComparisonVisibleOriginalFieldSchema(t *testing.T) {
	s := recordComparisonPage().Sections[1]
	info := EntityInfo{Fields: []FieldInfo{{Name: "name", Type: "text"}, {Name: "amount", Type: "decimal"}, {Name: "active", Type: "boolean"}}}
	if err := s.CheckRecordComparison(info); err != nil {
		t.Fatal(err)
	}
	info.Fields = info.Fields[1:]
	if s.CheckRecordComparison(info) == nil {
		t.Fatal("private title accepted")
	}
	s.RecordComparison.LabelField = "id"
	if err := s.CheckRecordComparison(info); err != nil {
		t.Fatal(err)
	}
	for _, typ := range []string{"text", "longtext", "choice", "reference", "integer", "decimal", "money", "date", "datetime", "boolean"} {
		s.Fields = []string{"value"}
		info.Fields = []FieldInfo{{Name: "value", Type: typ}}
		if err := s.CheckRecordComparison(info); err != nil {
			t.Fatalf("original %s: %v", typ, err)
		}
	}
	s.Fields = []string{"value", "value"}
	if s.CheckRecordComparison(info) == nil {
		t.Fatal("duplicate field accepted")
	}
	s.Fields = []string{"value"}
	info.Fields = []FieldInfo{{Name: "value", Type: "collection"}}
	if s.CheckRecordComparison(info) == nil {
		t.Fatal("unsupported type accepted")
	}
	s.Fields = nil
	if s.CheckRecordComparison(info) == nil {
		t.Fatal("fully cropped properties accepted")
	}
}
func TestFrozenRecordComparisonOriginalSchema(t *testing.T) {
	p := recordComparisonPage()
	fields := []FieldInfo{{Name: "name", Type: "text"}, {Name: "amount", Type: "decimal"}, {Name: "active", Type: "boolean"}}
	lookup := map[AssetRef]ReleaseAsset{p.Object: {Ref: p.Object, Body: Raw(EntityInfo{Type: p.Object.Name, Fields: fields})}}
	if err := checkFrozenQueries(p, lookup); err != nil {
		t.Fatal(err)
	}
	for _, missing := range []string{"name", "amount", "active"} {
		cropped := slices.DeleteFunc(slices.Clone(fields), func(f FieldInfo) bool { return f.Name == missing })
		lookup[p.Object] = ReleaseAsset{Ref: p.Object, Body: Raw(EntityInfo{Type: p.Object.Name, Fields: cropped})}
		if checkFrozenQueries(p, lookup) == nil {
			t.Fatalf("frozen comparison ignored %s", missing)
		}
	}
	lookup[p.Object] = ReleaseAsset{Ref: p.Object, Body: Raw(EntityInfo{Type: p.Object.Name, Fields: fields})}
	p.Sections[0].Object = AssetRef{App: "other", Kind: AssetObject, Name: p.Object.Name}
	if checkFrozenQueries(p, lookup) == nil {
		t.Fatal("frozen original owner ignored")
	}
}
