package platform

import (
	"encoding/json"
	"testing"
)

func TestTypedFacetQueryProfile(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	d.Variables["picked"] = PageVariable{Scope: "page", Type: "string-set", Mode: "state", Initial: json.RawMessage(`{"kind":"string-set","values":[]}`)}
	q := d.Queries["read"]
	q.Conditions = []PageQueryCondition{{Field: "bucket", Op: "in", Optional: true, Value: PageValue{Variable: "picked"}}}
	d.Queries["read"] = q
	facet := Section{ID: "facets", Widget: "filter", ConfigVersion: 1, CollectionVariable: "window", Facets: []PageFacet{{Field: "bucket", Variable: "picked", Kind: "histogram"}}}
	p.Sections = append(p.Sections, facet)
	d.Nodes["facets"] = PageLayoutNode{Kind: "widget", Section: "facets"}
	root := d.Nodes[d.Root]
	root.Children = append(root.Children, "facets")
	d.Nodes[d.Root] = root
	info := EntityInfo{Type: "sample.note", Fields: []FieldInfo{{Name: "bucket", Type: "text"}}}
	if err := d.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	if err := p.CheckQuerySchema(q, info, nil); err != nil {
		t.Fatal(err)
	}
	if err := facet.CheckFacetSchema(info); err != nil {
		t.Fatal(err)
	}
	d.UIProfile = "platform.page.v2.29"
	if d.Check(p.Sections) == nil {
		t.Fatal("old profile accepted facets")
	}
	d.UIProfile = PageUIProfile()
	for _, raw := range []string{`{"kind":"string-set","values":["A","A"]}`, `{"kind":"string-set","values":[2]}`, `{"kind":"string-set","values":[],"extra":true}`} {
		v := d.Variables["picked"]
		v.Initial = json.RawMessage(raw)
		d.Variables["picked"] = v
		if d.CheckVariables() == nil {
			t.Fatal("malformed set accepted")
		}
	}
}
