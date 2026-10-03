package platform

import "testing"

func TestOriginalSearchInputBindingAndProfile(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	q := d.Queries["read"]
	q.Search = &PageValue{Variable: "bucket"}
	d.Queries["read"] = q
	s := Section{ID: "search", Widget: "input", ConfigVersion: 1, InputKind: "search"}
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID, ValueVariable: "bucket"}
	r := d.Nodes[d.Root]
	r.Children = append(r.Children, s.ID)
	d.Nodes[d.Root] = r
	p.Sections = append(p.Sections, s)
	if err := d.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	d.UIProfile = "platform.page.v2.65"
	if d.Check(p.Sections) == nil {
		t.Fatal("old profile accepted search input")
	}
	d.UIProfile = PageUIProfile()
	q.Search = nil
	d.Queries["read"] = q
	if d.Check(p.Sections) == nil {
		t.Fatal("unbound search input accepted")
	}
	q.Search = &PageValue{Variable: "bucket"}
	q.Owner = "other"
	d.Queries["read"] = q
	if d.checkInputPresentation(s) == nil {
		t.Fatal("foreign-owner query accepted")
	}
	q.Owner = ""
	d.Queries["read"] = q
	v := d.Variables["bucket"]
	v.Type = "decimal"
	d.Variables["bucket"] = v
	if d.checkInputPresentation(s) == nil {
		t.Fatal("decimal search coercion accepted")
	}
	v.Type = "string"
	d.Variables["bucket"] = v
	s.Widget = "text"
	if d.checkInputPresentation(s) == nil {
		t.Fatal("search kind on other widget accepted")
	}
}
