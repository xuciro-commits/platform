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
