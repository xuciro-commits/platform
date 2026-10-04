package platform

import "testing"

func TestTablePresentationBoundsAndFieldFormats(t *testing.T) {
	d := &PageDocument{UIProfile: PageUIProfile()}
	search := false
	s := Section{Widget: "table", Fields: []string{"amount", "status", "date"}, ShowSearch: &search, TableColumns: []PageTableColumn{{Field: "id", Title: "Record", Width: 90}, {Field: "amount", Formatter: "numeric"}, {Field: "status", Formatter: "badge"}, {Field: "date", Formatter: "date"}}}
	info := EntityInfo{Fields: []FieldInfo{{Name: "amount", Type: "decimal"}, {Name: "status", Type: "choice"}, {Name: "date", Type: "date"}}}
	if err := d.checkTablePresentation(s); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckTablePresentation(info); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Section){func(s *Section) { s.Widget = "detail" }, func(s *Section) { s.TableColumns[0].Width = 39 }, func(s *Section) { s.TableColumns[0].Width = 1201 }, func(s *Section) { s.TableColumns[0].Formatter = "numeric" }, func(s *Section) { s.TableColumns[1].Field = "hidden" }, func(s *Section) { s.TableColumns = append(s.TableColumns, s.TableColumns[0]) }, func(s *Section) { s.TableColumns[0].Formatter = "script" }} {
		copy := s
		copy.TableColumns = append([]PageTableColumn{}, s.TableColumns...)
		change(&copy)
		if d.checkTablePresentation(copy) == nil {
			t.Fatal("invalid column accepted", copy)
		}
	}
	d.UIProfile = "platform.page.v2.33"
	if d.checkTablePresentation(s) == nil {
		t.Fatal("old profile accepted new presentation")
	}
	d.UIProfile = PageUIProfile()
	bad := s
	bad.TableColumns = []PageTableColumn{{Field: "status", Formatter: "numeric"}}
	if bad.CheckTablePresentation(info) == nil {
		t.Fatal("numeric formatter accepted choice")
	}
	bad.TableColumns = []PageTableColumn{{Field: "unknown"}}
	if bad.CheckTablePresentation(info) == nil {
		t.Fatal("unknown field accepted")
	}
}

func TestTableControlsFreezeBoundedOriginalCountTitle(t *testing.T) {
	p := collectionBuilderPage()
	p.Sections = p.Sections[1:]
	p.Document.Nodes = map[string]PageLayoutNode{"root": {Kind: "rows", Children: []string{"table"}}, "table": {Kind: "widget", Section: "table"}}
	p.Document.Queries = map[string]PageQuery{"result": {Object: p.Object, Limit: 2}}
	delete(p.Document.Variables, "output")
	delete(p.Document.Variables, "base")
	title := "Assets ({count})"
	p.Sections[0].TablePresentation = &PageTablePresentation{Density: "compact", ShowToolbar: true, TitleTemplate: &title}
	if err := p.Document.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Page){func(p *Page) { p.Document.UIProfile = "platform.page.v2.89" }, func(p *Page) { p.Sections[0].TablePresentation.Density = "large" }, func(p *Page) { s := "{count} {count}"; p.Sections[0].TablePresentation.TitleTemplate = &s }, func(p *Page) { s := "{execute()}"; p.Sections[0].TablePresentation.TitleTemplate = &s }, func(p *Page) { p.Sections[0].CollectionVariable = "" }, func(p *Page) { p.Sections[0].Widget = "detail" }} {
		copy := p
		section := p.Sections[0]
		style := *section.TablePresentation
		section.TablePresentation = &style
		copy.Sections = []Section{section}
		doc := *p.Document
		copy.Document = &doc
		change(&copy)
		if copy.Document.Check(copy.Sections) == nil {
			t.Fatal("invalid table controls accepted")
		}
	}
	empty := ""
	p.Sections[0].TablePresentation.TitleTemplate = &empty
	p.Sections[0].TablePresentation.ShowToolbar = false
	if err := p.Document.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
}
