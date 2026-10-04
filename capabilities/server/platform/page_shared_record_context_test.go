package platform

import "testing"

func sharedContextPage() Page {
	p := explorationPage()
	p.Sections = append(p.Sections[:2], p.Sections[4])
	p.Sections[0] = Section{ID: "resources", Widget: "kanban", ConfigVersion: 1, CollectionVariable: "assets", SelectionVariable: "active", CardLabel: "name"}
	p.Sections = append(p.Sections, Section{ID: "card", Widget: "record-card", ConfigVersion: 1, RecordVariable: "active", RecordCard: &PageRecordCard{LabelField: "name", Tone: "neutral"}}, Section{ID: "trail", Widget: "breadcrumb", ConfigVersion: 1, RecordVariable: "active", Breadcrumb: &PageBreadcrumb{HomeLabel: "Home", PageLabel: "Maintenance", LabelField: "name"}})
	p.Document.Nodes = map[string]PageLayoutNode{"root": {Kind: "rows", Children: []string{"resources", "graph", "sensorCard", "card", "trail"}}}
	for _, s := range p.Sections {
		p.Document.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	}
	p.Document.Variables["active"] = PageVariable{Scope: "application", Type: "record", Mode: "shared", Writable: true, Source: &PageResourceSource{Kind: "application", Variable: "selected", Object: &p.Object}}
	p.Document.Events = []PageEventBinding{{Source: "trail", Control: "home", Event: "click", Navigate: &PageNavigation{Page: AssetRef{App: "sample", Kind: AssetPage, Name: "home"}}}}
	return p
}
func TestSharedMaintenanceContextKeepsTypedInputAndLocalGraphOutputs(t *testing.T) {
	p := sharedContextPage()
	if err := p.Document.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	if err := p.CheckRecordPorts(); err != nil {
		t.Fatal(err)
	}
	if err := p.CheckCollectionPorts(); err != nil {
		t.Fatal(err)
	}
	if p.RecordResourceObject("active") != p.Object {
		t.Fatal("lost full application object identity")
	}
	cases := []struct {
		name   string
		change func(*Page)
	}{
		{"old profile", func(p *Page) { p.Document.UIProfile = "platform.page.v2.97" }},
		{"readonly board", func(p *Page) {
			v := p.Document.Variables["active"]
			v.Writable = false
			p.Document.Variables["active"] = v
		}},
		{"wrong card object", func(p *Page) { p.Sections[3].Object = AssetRef{App: "other", Kind: AssetObject, Name: p.Object.Name} }},
		{"application graph output", func(p *Page) {
			v := p.Document.Variables["sensorOutput"]
			v.Scope = "application"
			p.Document.Variables["sensorOutput"] = v
		}},
		{"overlay producer", func(p *Page) {
			p.Document.Nodes["root"] = PageLayoutNode{Kind: "rows", Children: []string{"graph", "sensorCard", "card", "trail"}}
			p.Document.Variables["open"] = PageVariable{Scope: "page", Type: "boolean", Mode: "state", Initial: Raw(false)}
			p.Document.Overlays = map[string]PageOverlay{"panel": {Title: "Panel", Kind: "drawer", Root: "resources", OpenVariable: "open"}}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := sharedContextPage()
			c.change(&p)
			if p.Document.Check(p.Sections) == nil && p.CheckRecordPorts() == nil && p.CheckCollectionPorts() == nil {
				t.Fatal("accepted invalid shared context")
			}
		})
	}
}

func TestSharedGraphInOverlayKeepsItsOutputsInThatOverlay(t *testing.T) {
	p := sharedContextPage()
	p.Document.Nodes["root"] = PageLayoutNode{Kind: "rows", Children: []string{"resources", "card", "trail"}}
	p.Document.Nodes["panelRoot"] = PageLayoutNode{Kind: "rows", Children: []string{"graph", "sensorCard"}}
	p.Document.Variables["open"] = PageVariable{Scope: "page", Type: "boolean", Mode: "state", Initial: Raw(false)}
	p.Document.Overlays = map[string]PageOverlay{"panel": {Title: "Panel", Kind: "drawer", Root: "panelRoot", OpenVariable: "open"}}
	for _, id := range []string{"assetOutput", "sensorOutput", "alertOutput"} {
		v := p.Document.Variables[id]
		v.Scope = "overlay"
		v.Owner = "panel"
		p.Document.Variables[id] = v
	}
	if err := p.Document.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	if err := p.CheckRecordPorts(); err != nil {
		t.Fatal(err)
	}
	v := p.Document.Variables["sensorOutput"]
	v.Scope = "page"
	v.Owner = ""
	p.Document.Variables["sensorOutput"] = v
	if p.Document.Check(p.Sections) == nil {
		t.Fatal("accepted escaped overlay graph output")
	}
}
