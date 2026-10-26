package platform

import (
	"slices"
	"testing"
)

func TestSharedRecordSetDeclarationsAndOriginalBindings(t *testing.T) {
	p := recordComparisonPage()
	object := p.Object
	p.Document.Variables["picked"] = PageVariable{Scope: "application", Mode: "shared", Type: "record-set", Writable: true, Source: &PageResourceSource{Kind: "application", Variable: "records", Object: &object}}
	app := Application{Name: "desk", Pages: []string{p.Name}, UIProfile: PageUIProfile(), Variables: map[string]PageVariable{"records": {Scope: "application", Type: "record-set", Mode: "resource", Source: &PageResourceSource{Kind: "record-set", Object: &object}}}}
	for _, err := range []error{app.CheckVariables(), p.Document.Check(p.Sections), app.CheckPageVariables(p), p.CheckCollectionPorts(), p.CheckRecordPorts()} {
		if err != nil {
			t.Fatal(err)
		}
	}
	if !slices.Contains(app.QueryPage().QueryReferences(), object) || p.RecordSetVariableObject("picked") != object {
		t.Fatal("lost original object requirement")
	}
	// A consumer needs no local table or additional query plan.
	consumer := p
	consumer.Sections = p.Sections[1:]
	copy := *p.Document
	consumer.Document = &copy
	consumer.Document.Nodes = map[string]PageLayoutNode{"root": {Kind: "rows", Children: []string{"comparison"}}, "comparison": {Kind: "widget", Section: "comparison"}}
	v := consumer.Document.Variables["picked"]
	v.Writable = false
	consumer.Document.Variables = map[string]PageVariable{"picked": v}
	if err := consumer.Document.Check(consumer.Sections); err != nil {
		t.Fatal(err)
	}
	if err := consumer.CheckRecordPorts(); err != nil {
		t.Fatal(err)
	}
	app.Variables["has"] = PageVariable{Scope: "application", Type: "boolean", Mode: "derived", Expression: &PageExpression{Op: "present", Args: []PageValue{{Variable: "records"}}}}
	if app.CheckVariables() == nil {
		t.Fatal("unsupported set scalar expression accepted")
	}
	delete(app.Variables, "has")
	old := app.UIProfile
	app.UIProfile = "platform.page.v2.95"
	if app.CheckVariables() == nil {
		t.Fatal("old application profile accepted")
	}
	app.UIProfile = old
	appv := app.Variables["records"]
	appv.Source.Section = "table"
	app.Variables["records"] = appv
	if app.CheckVariables() == nil {
		t.Fatal("application adopted page producer")
	}
	appv.Source.Section = ""
	app.Variables["records"] = appv
	v = p.Document.Variables["picked"]
	v.Writable = false
	p.Document.Variables["picked"] = v
	if p.Document.Check(p.Sections) == nil {
		t.Fatal("read-only table output accepted")
	}
	v.Writable = true
	p.Document.Variables["picked"] = v
	p.Document.UIProfile = "platform.page.v2.95"
	if p.Document.Check(p.Sections) == nil {
		t.Fatal("old shared set profile accepted")
	}
	p.Document.UIProfile = PageUIProfile()
	p.Document.Nodes["root"] = PageLayoutNode{Kind: "loop", Children: []string{"table", "comparison"}}
	if p.Document.checkSharedRecordSetOutput(p.Sections[0]) == nil {
		t.Fatal("Loop set producer accepted")
	}
	p.Document.Nodes["root"] = PageLayoutNode{Kind: "rows"}
	p.Document.Nodes["panel"] = PageLayoutNode{Kind: "rows", Children: []string{"table"}}
	p.Document.Overlays = map[string]PageOverlay{"panel": {Root: "panel", Kind: "drawer", OpenVariable: "open"}}
	if p.Document.checkSharedRecordSetOutput(p.Sections[0]) == nil {
		t.Fatal("Overlay set producer accepted")
	}
	other := object
	other.App = "other"
	v = p.Document.Variables["picked"]
	v.Source.Object = &other
	p.Document.Variables["picked"] = v
	if p.CheckCollectionPorts() == nil || app.CheckPageVariables(p) == nil || p.CheckRecordPorts() == nil {
		t.Fatal("foreign object identity accepted")
	}
}
