package platform

import "testing"

func TestLinkPlanRequiresTypedStartVersionAndDirection(t *testing.T) {
	p := nestedPage()
	q := p.Document.Queries["children"]
	q.Query = &AssetBinding{Ref: AssetRef{App: "sample", Kind: AssetLinkType, Name: "children"}, SourceVersion: "l1"}
	q.Direction = "forward"
	p.Document.Queries["children"] = q
	if err := p.Document.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	l := LinkType{Parent: p.Object, Child: q.Object}
	named := Definition{Ref: q.Query.Ref, Version: "l1", LinkType: &l}
	if err := p.CheckQuerySchema(q, EntityInfo{Type: q.Object.Name}, &named); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*PageDocument){func(d *PageDocument) { d.UIProfile = "platform.page.v2.21" }, func(d *PageDocument) { q := d.Queries["children"]; q.Direction = ""; d.Queries["children"] = q }, func(d *PageDocument) {
		q := d.Queries["children"]
		q.For = &PageValue{Literal: Raw("A")}
		d.Queries["children"] = q
	}, func(d *PageDocument) {
		q := d.Queries["children"]
		q.Set = &PageQuerySet{Op: "union", Inputs: []string{"parents", "parents"}}
		d.Queries["children"] = q
	}} {
		bad := nestedPage()
		bad.Document.Queries["children"] = q
		change(bad.Document)
		if bad.Document.Check(bad.Sections) == nil {
			t.Fatal("unsafe relation plan accepted")
		}
	}
	q.Direction = "reverse"
	if p.CheckQuerySchema(q, EntityInfo{Type: q.Object.Name}, &named) == nil {
		t.Fatal("reverse direction accepted forward object and record")
	}
}
