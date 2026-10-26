package platform

import "testing"

func TestOriginalTermCountsProfileAndFields(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	s := Section{ID: "terms", Widget: "term-counts", ConfigVersion: 1, CollectionVariable: "window", Group: "bucket"}
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	r := d.Nodes[d.Root]
	r.Children = append(r.Children, s.ID)
	d.Nodes[d.Root] = r
	p.Sections = append(p.Sections, s)
	if err := d.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckTerms(EntityInfo{Fields: []FieldInfo{{Name: "bucket", Type: "text"}}}); err != nil {
		t.Fatal(err)
	}
	d.UIProfile = "platform.page.v2.66"
	if d.Check(p.Sections) == nil {
		t.Fatal("old profile accepted term counts")
	}
	d.UIProfile = PageUIProfile()
	for _, field := range []FieldInfo{{Name: "bucket", Type: "decimal"}, {Name: "other", Type: "text"}} {
		if s.CheckTerms(EntityInfo{Fields: []FieldInfo{field}}) == nil {
			t.Fatal("invalid or hidden group field accepted")
		}
	}
	s.Group = "count"
	if d.checkTerms(s) == nil {
		t.Fatal("count column collision accepted")
	}
	s.Group = "bucket:day"
	if d.checkTerms(s) == nil {
		t.Fatal("bucket coercion accepted")
	}
	s.Group = "bucket"
	v := d.Variables["window"]
	v.Scope = "application"
	d.Variables["window"] = v
	if d.checkTerms(s) == nil {
		t.Fatal("unimplemented shared terms accepted")
	}
}
