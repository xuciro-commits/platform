package platform

import (
	"encoding/json"
	"testing"
)

func TestRecordGallerySharedApplicationPort(t *testing.T) {
	makePage := func() Page {
		p := queryPlanPage()
		p.Document.UIProfile = PageUIProfile()
		p.Document.Events = nil
		p.Document.Variables = map[string]PageVariable{"window": p.Document.Variables["window"], "bucket": p.Document.Variables["bucket"]}
		p.Document.Nodes = map[string]PageLayoutNode{"root": {Kind: "rows", Children: []string{"cards"}}, "cards": {Kind: "widget", Section: "cards"}}
		p.Sections = []Section{{ID: "cards", Widget: "record-list", ConfigVersion: 1, CardLabel: "id", CollectionVariable: "window", RecordList: &PageRecordList{Layout: "grid"}, SelectionVariable: "shared"}}
		p.Document.Variables["shared"] = PageVariable{Scope: "application", Mode: "shared", Type: "record", Writable: true, Source: &PageResourceSource{Kind: "application", Variable: "record", Object: &p.Object}}
		return p
	}
	p := makePage()
	if err := p.Document.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	if err := p.CheckCollectionPorts(); err != nil {
		t.Fatal(err)
	}
	app := Application{Name: "desk", Pages: []string{p.Name}, UIProfile: PageUIProfile(), Variables: map[string]PageVariable{"record": {Scope: "application", Mode: "resource", Type: "record", Source: &PageResourceSource{Kind: "record", Object: &p.Object}}}}
	if err := app.CheckPageVariables(p); err != nil {
		t.Fatal(err)
	}
	asset, err := PageReleaseAsset("sample", "cards-1", p)
	if err != nil {
		t.Fatal(err)
	}
	var body Page
	if err = json.Unmarshal(asset.Body, &body); err != nil {
		t.Fatal(err)
	}
	// The release body owns exactly the original writer declaration.
	if body.Sections[0].SelectionVariable != "shared" {
		t.Fatal("lost shared selection in release")
	}
	for _, change := range []func(*Page){func(p *Page) { p.Document.UIProfile = "platform.page.v2.93" }, func(p *Page) {
		v := p.Document.Variables["shared"]
		v.Writable = false
		p.Document.Variables["shared"] = v
	}, func(p *Page) { p.Sections[0].Selection = "local" }, func(p *Page) { p.Sections[0].RecordList.Layout = "tiles" }, func(p *Page) {
		p.Document.Nodes["root"] = PageLayoutNode{Kind: "rows"}
		p.Document.Overlays = map[string]PageOverlay{"drawer": {Root: "overlay", Kind: "drawer", OpenVariable: "open"}}
		p.Document.Nodes["overlay"] = PageLayoutNode{Kind: "rows", Children: []string{"cards"}}
		p.Document.Variables["open"] = PageVariable{Scope: "page", Type: "boolean", Mode: "state", Initial: Raw(false)}
	}} {
		p := makePage()
		change(&p)
		if p.Document.Check(p.Sections) == nil {
			t.Fatal("invalid gallery writer accepted")
		}
	}
}

func TestRecordListProfileAndOriginalProducer(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	s := Section{ID: "cards", Widget: "record-list", ConfigVersion: 1, CardLabel: "title", Fields: []string{"qty"}, CollectionVariable: "window", RecordList: &PageRecordList{Layout: "grid"}}
	d.Nodes["cards"] = PageLayoutNode{Kind: "widget", Section: "cards"}
	root := d.Nodes[d.Root]
	root.Children = append(root.Children, "cards")
	d.Nodes[d.Root] = root
	p.Sections = append(p.Sections, s)
	d.Variables["cardRecord"] = PageVariable{Scope: "page", Type: "record", Mode: "resource", Source: &PageResourceSource{Kind: "record", Section: "cards"}}
	if err := d.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	info := EntityInfo{Type: "sample.note", Fields: []FieldInfo{{Name: "title", Type: "text"}, {Name: "qty", Type: "integer"}}}
	if err := s.CheckRecordList(info); err != nil {
		t.Fatal(err)
	}
	d.UIProfile = "platform.page.v2.41"
	if d.Check(p.Sections) == nil {
		t.Fatal("old profile accepted cards")
	}
	d.UIProfile = PageUIProfile()
	copy := s
	copy.RecordList = &PageRecordList{Layout: "script"}
	if d.checkRecordList(copy) == nil {
		t.Fatal("unknown layout accepted")
	}
	copy = s
	copy.Fields = []string{"qty", "qty"}
	if copy.CheckRecordList(info) == nil {
		t.Fatal("duplicate summary accepted")
	}
	copy = s
	copy.CardLabel = "hidden"
	if copy.CheckRecordList(info) == nil {
		t.Fatal("hidden label accepted")
	}
	if (*PageDocument)(nil).Check([]Section{s}) == nil {
		t.Fatal("cards without document accepted")
	}
}
