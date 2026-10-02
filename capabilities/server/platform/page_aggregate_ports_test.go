package platform

import "testing"

func TestAggregateCollectionPortsUseTheOriginalObjectSetAndScope(t *testing.T) {
	p := queryPlanPage()
	p.Sections = append(p.Sections, Section{ID: "metric", Widget: "metric", ConfigVersion: 1, Measure: "count", CollectionVariable: "window"})
	p.Document.Nodes["metric"] = PageLayoutNode{Kind: "widget", Section: "metric"}
	root := p.Document.Nodes[p.Document.Root]
	root.Children = append(root.Children, "metric")
	p.Document.Nodes[p.Document.Root] = root
	if err := p.Document.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	if err := p.CheckCollectionPorts(); err != nil {
		t.Fatal(err)
	}
	p.Document.UIProfile = "platform.page.v2.18"
	if p.Document.Check(p.Sections) == nil {
		t.Fatal("old profile accepted a new aggregate port")
	}
	p.Document.UIProfile = PageUIProfile()
	p.Sections[len(p.Sections)-1].FilterVariable = "extra"
	if p.Document.Check(p.Sections) == nil {
		t.Fatal("aggregate plan accepted an extra filter")
	}
	p.Sections[len(p.Sections)-1].FilterVariable = ""
	p.Sections[len(p.Sections)-1].Object = AssetRef{App: "sample", Kind: AssetObject, Name: "sample.other"}
	if p.CheckCollectionPorts() == nil {
		t.Fatal("aggregate port accepted another object")
	}
}
