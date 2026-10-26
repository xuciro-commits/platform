package platform

import (
	"fmt"
	"testing"
)

func TestPageCountsAreScopedReadOnlyAndCannotFeedQueryInputs(t *testing.T) {
	p := nestedPage()
	p.Document.Variables["count"] = PageVariable{Scope: "loop-item", Owner: "parents", Type: "decimal", Mode: "aggregate", Source: &PageResourceSource{Kind: "count", Query: "children"}}
	if err := p.Document.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*PageDocument){
		func(d *PageDocument) { d.UIProfile = "platform.page.v2.20" },
		func(d *PageDocument) { v := d.Variables["count"]; v.Owner = "children"; d.Variables["count"] = v },
		func(d *PageDocument) { v := d.Variables["count"]; v.Type = "string"; d.Variables["count"] = v },
		func(d *PageDocument) { v := d.Variables["count"]; v.Writable = true; d.Variables["count"] = v },
		func(d *PageDocument) {
			q := d.Queries["children"]
			q.Conditions = []PageQueryCondition{{Field: "score", Op: ">", Value: PageValue{Variable: "count"}}}
			d.Queries["children"] = q
		},
		func(d *PageDocument) { n := d.Nodes["parents"]; n.Loop.Limit = 33; d.Nodes["parents"] = n },
		func(d *PageDocument) {
			for i := 0; i < 8; i++ {
				d.Variables[fmt.Sprintf("count%d", i)] = d.Variables["count"]
			}
		},
	} {
		original := p.Document
		p2 := nestedPage()
		p2.Document.Variables["count"] = original.Variables["count"]
		change(p2.Document)
		if p2.Document.Check(p2.Sections) == nil {
			t.Fatal("invalid count declaration accepted")
		}
	}
	delete(p.Document.Queries, "children")
	if _, ok := p.Document.Visible(p.Sections).Variables["count"]; ok {
		t.Fatal("count survived missing source")
	}
}

func TestApplicationCountDeclarationsAndSharedBindings(t *testing.T) {
	a := Application{Name: "desk", UIProfile: PageUIProfile(), Variables: map[string]PageVariable{"count": {Scope: "application", Type: "decimal", Mode: "aggregate", Source: &PageResourceSource{Kind: "count", Query: "read"}}}, Queries: map[string]PageQuery{"read": {Object: AssetRef{App: "sample", Kind: AssetObject, Name: "sample.note"}, Limit: 1}}}
	if err := a.CheckVariables(); err != nil {
		t.Fatal(err)
	}
	p := Page{Document: &PageDocument{Variables: map[string]PageVariable{"count": {Scope: "application", Type: "decimal", Mode: "shared", Source: &PageResourceSource{Kind: "application", Variable: "count"}}}}}
	if err := a.CheckPageVariables(p); err != nil {
		t.Fatal(err)
	}
	v := p.Document.Variables["count"]
	v.Writable = true
	p.Document.Variables["count"] = v
	if a.CheckPageVariables(p) == nil {
		t.Fatal("count became writable")
	}
}
