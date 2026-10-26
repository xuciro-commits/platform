package platform

import (
	"encoding/json"
	"testing"
)

func TestRecordPropertyGraphAndSchema(t *testing.T) {
	object := AssetRef{App: "sample", Kind: AssetObject, Name: "sample.note"}
	page := Page{Object: object, Sections: []Section{{ID: "table", Widget: "table", ConfigVersion: 1}}, Document: &PageDocument{FormatVersion: 2, UIProfile: PageUIProfile(), Root: "root", Nodes: map[string]PageLayoutNode{"root": {Kind: "rows", Children: []string{"table"}}, "table": {Kind: "widget", Section: "table"}}, Variables: map[string]PageVariable{"selected": {Scope: "page", Type: "record", Mode: "resource", Source: &PageResourceSource{Kind: "record", Section: "table"}}, "active": {Scope: "page", Type: "boolean", Mode: "property", Source: &PageResourceSource{Kind: "property", Variable: "selected", Object: &object, Field: "active"}}}}}
	if err := page.Document.Check(page.Sections); err != nil {
		t.Fatal(err)
	}
	info := EntityInfo{App: "sample", Type: object.Name, Fields: []FieldInfo{{Name: "active", Type: "boolean"}}}
	if err := page.CheckPropertySchema(page.Document.Variables["active"], info); err != nil {
		t.Fatal(err)
	}
	info.Fields[0].Type = "text"
	if page.CheckPropertySchema(page.Document.Variables["active"], info) == nil {
		t.Fatal("field type drift accepted")
	}
	for _, change := range []func(*PageDocument){func(d *PageDocument) { d.UIProfile = "platform.page.v2.16" }, func(d *PageDocument) {
		v := d.Variables["active"]
		v.Source.Variable = "active"
		d.Variables["active"] = v
	}, func(d *PageDocument) {
		v := d.Variables["selected"]
		v.Scope = "overlay"
		v.Owner = "other"
		d.Variables["selected"] = v
	}} {
		raw, _ := json.Marshal(page.Document)
		var copy PageDocument
		json.Unmarshal(raw, &copy)
		change(&copy)
		if copy.Check(page.Sections) == nil {
			t.Fatal("invalid property graph accepted")
		}
	}
	visible := page.Document.Visible(nil)
	if _, ok := visible.Variables["active"]; ok {
		t.Fatal("property survived missing record source")
	}
}
