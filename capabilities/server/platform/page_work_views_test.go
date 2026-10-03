package platform

import (
	"slices"
	"testing"
)

func workViewsPage() Page {
	object := AssetRef{App: "sample", Kind: AssetObject, Name: "sample.note"}
	p := Page{Name: "work-views", Layout: "composed", Object: object, Document: &PageDocument{FormatVersion: 2, UIProfile: PageUIProfile(), Root: "root", Nodes: map[string]PageLayoutNode{"root": {Kind: "rows", Children: []string{"table"}}, "table": {Kind: "widget", Section: "table"}}, Variables: map[string]PageVariable{
		"active": {Scope: "page", Type: "record", Mode: "resource", Source: &PageResourceSource{Kind: "record", Section: "table"}}, "enabled": {Scope: "page", Type: "boolean", Mode: "state", Initial: Raw(false)},
	}}, Sections: []Section{{ID: "table", Widget: "table", ConfigVersion: 1, Fields: []string{"name"}}}}
	for _, s := range []Section{{ID: "approvals", Widget: "approval-inbox", ConfigVersion: 1}, {ID: "notifications", Widget: "notification-feed", ConfigVersion: 1}, {ID: "history", Widget: "timeline", ConfigVersion: 1, RecordVariable: "active", HistoryLimit: 18}} {
		p.Sections = append(p.Sections, s)
		node := PageLayoutNode{Kind: "widget", Section: s.ID}
		if s.Widget != "timeline" {
			node.EnabledWhen = "enabled"
		}
		p.Document.Nodes[s.ID] = node
		n := p.Document.Nodes[p.Document.Root]
		n.Children = append(n.Children, s.ID)
		p.Document.Nodes[p.Document.Root] = n
	}
	return p
}
func TestWorkViewsFixedCallerProfileAndBindings(t *testing.T) {
	p := workViewsPage()
	if err := p.Document.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	if err := p.CheckRecordPorts(); err != nil {
		t.Fatal(err)
	}
	if (*PageDocument)(nil).Check(p.Sections) == nil {
		t.Fatal("documentless feeds accepted")
	}
	for _, tc := range []struct {
		name   string
		change func(*Page)
	}{
		{"old profile", func(p *Page) { p.Document.UIProfile = "platform.page.v2.76" }},
		{"feed object override", func(p *Page) { p.Sections[1].Object = p.Object }},
		{"feed record input", func(p *Page) { p.Sections[1].RecordVariable = "active" }},
		{"feed original collection", func(p *Page) { p.Sections[1].CollectionVariable = "active" }},
		{"ordinary approve action", func(p *Page) {
			p.Sections[1].Actions = []AssetRef{{App: "sample", Kind: AssetAction, Name: "sample.note.finish"}}
		}},
		{"feed fields", func(p *Page) { p.Sections[2].Fields = []string{"name"} }},
		{"history old profile", func(p *Page) {
			p.Sections = p.Sections[:1]
			delete(p.Document.Nodes, "approvals")
			delete(p.Document.Nodes, "notifications")
			p.Sections = append(p.Sections, Section{ID: "history", Widget: "timeline", ConfigVersion: 1, RecordVariable: "active", HistoryLimit: 18})
			p.Document.Nodes["root"] = PageLayoutNode{Kind: "rows", Children: []string{"table", "history"}}
			p.Document.UIProfile = "platform.page.v2.76"
		}},
		{"history budget", func(p *Page) { p.Sections[3].HistoryLimit = 101 }},
		{"history negative", func(p *Page) { p.Sections[3].HistoryLimit = -1 }},
		{"history range on another widget", func(p *Page) { p.Sections[3].Widget = "detail" }},
		{"history independent fields", func(p *Page) { p.Sections[3].Fields = []string{"name"} }},
		{"history constant ID", func(p *Page) {
			p.Document.Variables["active"] = PageVariable{Scope: "page", Type: "string", Mode: "constant", Initial: Raw("N1")}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := workViewsPage()
			tc.change(&p)
			if p.Document.Check(p.Sections) == nil {
				t.Fatal("invalid fixed work view accepted")
			}
		})
	}
	p = workViewsPage()
	p.Sections[3].Object = AssetRef{App: "other", Kind: AssetObject, Name: p.Object.Name}
	if p.CheckRecordPorts() == nil {
		t.Fatal("bounded history changed original object owner")
	}
	// Legacy timeline without a declared range keeps its original profile and shape.
	p = workViewsPage()
	p.Sections = []Section{p.Sections[0], p.Sections[3]}
	p.Sections[1].HistoryLimit = 0
	p.Document.UIProfile = "platform.page.v2.11"
	delete(p.Document.Nodes, "approvals")
	delete(p.Document.Nodes, "notifications")
	p.Document.Nodes["root"] = PageLayoutNode{Kind: "rows", Children: []string{"table", "history"}}
	if err := p.Document.Check(p.Sections); err != nil {
		t.Fatal("legacy timeline changed", err)
	}
}
func TestWorkViewOwnerAndHistoryProducerRetirement(t *testing.T) {
	p := workViewsPage()
	d := p.Document
	d.Nodes["panelRoot"] = PageLayoutNode{Kind: "rows", Children: []string{"table", "history", "approvals", "notifications"}}
	d.Nodes[d.Root] = PageLayoutNode{Kind: "rows", Children: []string{"main"}}
	d.Nodes["main"] = PageLayoutNode{Kind: "widget", Section: "main"}
	p.Sections = append(p.Sections, Section{ID: "main", Widget: "text", ConfigVersion: 1})
	d.Variables["open"] = PageVariable{Scope: "page", Type: "boolean", Mode: "state", Initial: Raw(false)}
	d.Overlays = map[string]PageOverlay{"panel": {Kind: "modal", Title: "Work", Root: "panelRoot", OpenVariable: "open"}}
	v := d.Variables["active"]
	v.Scope = "overlay"
	v.Owner = "panel"
	d.Variables["active"] = v
	if err := d.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	n := d.Nodes["panelRoot"]
	n.Children = slices.DeleteFunc(n.Children, func(id string) bool { return id == "history" })
	d.Nodes["panelRoot"] = n
	d.Nodes[d.Root] = PageLayoutNode{Kind: "rows", Children: []string{"main", "history"}}
	if d.Check(p.Sections) == nil {
		t.Fatal("history escaped original overlay producer")
	}
	p = workViewsPage()
	visible := p.Document.Visible(p.Sections[1:])
	for _, node := range visible.Nodes {
		if node.Section == "history" {
			t.Fatal("hidden original producer retained history")
		}
	}
	p = workViewsPage()
	d = p.Document
	d.Queries = map[string]PageQuery{"read": {Object: p.Object, Limit: 2}}
	d.Variables["window"] = PageVariable{Scope: "page", Type: "object-set", Mode: "resource", Source: &PageResourceSource{Kind: "plan", Query: "read"}}
	d.Variables["item"] = PageVariable{Scope: "loop-item", Owner: "loop", Type: "record", Mode: "resource", Source: &PageResourceSource{Kind: "item", Node: "loop"}}
	d.Nodes["loop"] = PageLayoutNode{Kind: "loop", Children: []string{"approvals"}, Loop: &PageLoop{Collection: "window", ItemVariable: "item", Limit: 2}}
	d.Nodes[d.Root] = PageLayoutNode{Kind: "rows", Children: []string{"table", "notifications", "history", "loop"}}
	if d.Check(p.Sections) == nil {
		t.Fatal("caller feed entered loop")
	}
}
func TestFrozenWorkViewsServiceClosureIncludesVirtualNotificationAction(t *testing.T) {
	p := workViewsPage()
	page, err := PageReleaseAsset("sample", "v1", p)
	if err != nil {
		t.Fatal(err)
	}
	services := []ReleaseAsset{{Ref: p.Object, SourceVersion: "1", ContractVersion: 1, Body: Raw(EntityInfo{App: p.Object.App, Type: p.Object.Name, Fields: []FieldInfo{{Name: "name", Type: "text"}}})}}
	refs := []AssetRef{}
	for _, s := range p.Sections {
		for _, ref := range s.WorkViewDependencies() {
			refs = append(refs, ref)
			var body any
			if ref.Kind == AssetObject {
				body = map[string]any{"type": ref.Name, "entity": EntityInfo{App: ref.App, Type: ref.Name}}
			} else {
				target := "work.approval"
				if ref.App == "platform" {
					target = "platform.notification"
				}
				body = Action{Schema: ref.Name, Target: target}
			}
			services = append(services, ReleaseAsset{Ref: ref, SourceVersion: "1", ContractVersion: 1, Body: Raw(body)})
		}
	}
	assets := append([]ReleaseAsset{page}, services...)
	if _, err := Candidate([]AssetRef{page.Ref}, assets); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(page.Requires, AssetRef{App: "platform", Kind: AssetObject, Name: "platform.notification"}) {
		t.Fatal("virtual notification class became a fake entity dependency")
	}
	for _, ref := range refs {
		if !slices.Contains(page.Requires, ref) {
			t.Fatal("fixed work dependency omitted", ref)
		}
		missing := page
		missing.Requires = slices.DeleteFunc(slices.Clone(page.Requires), func(r AssetRef) bool { return r == ref })
		if _, err := Candidate([]AssetRef{page.Ref}, append([]ReleaseAsset{missing}, services...)); err == nil {
			t.Fatal("page silently omitted fixed service", ref)
		}
	}
	bad := slices.Clone(assets)
	for i := range bad {
		if bad[i].Ref.Name == "platform.notification.read" {
			bad[i].Body = Raw(Action{Schema: "platform.notification.read", Target: p.Object.Name})
		}
	}
	if _, err := Candidate([]AssetRef{page.Ref}, bad); err == nil {
		t.Fatal("ordinary action target replaced the notification read decision")
	}
	bad = slices.Clone(assets)
	for i := range bad {
		if bad[i].Ref == p.Object {
			bad[i].Body = Raw(map[string]any{"type": p.Object.Name, "entity": EntityInfo{App: "other", Type: p.Object.Name}})
		}
	}
	if _, err := Candidate([]AssetRef{page.Ref}, bad); err == nil {
		t.Fatal("frozen history accepted a false actual object owner")
	}
}

func TestFrozenHistoryKeepsActualAliasedCodeOwner(t *testing.T) {
	p := workViewsPage()
	p.Object = AssetRef{App: "relations", Kind: AssetObject, Name: "platform.comment"}
	p.Sections = []Section{p.Sections[0], p.Sections[3]}
	p.Sections[0].Fields = []string{"text"}
	delete(p.Document.Nodes, "approvals")
	delete(p.Document.Nodes, "notifications")
	p.Document.Nodes[p.Document.Root] = PageLayoutNode{Kind: "rows", Children: []string{"table", "history"}}
	object := ReleaseAsset{Ref: p.Object, SourceVersion: "1", ContractVersion: 1, Body: Raw(map[string]any{"type": p.Object.Name, "entity": EntityInfo{App: "relations", Type: p.Object.Name, Fields: []FieldInfo{{Name: "text", Type: "longtext"}}}})}
	page, err := PageReleaseAsset("sample", "v1", p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Candidate([]AssetRef{page.Ref}, []ReleaseAsset{page, object}); err != nil {
		t.Fatal("actual code owner was inferred from its different type prefix", err)
	}
	// Producer and consumer both agree on a false prefix-derived owner. The
	// frozen source still names the actual app and must reject that declaration.
	p.Object.App = "platform"
	object.Ref = p.Object
	page, err = PageReleaseAsset("sample", "v1", p)
	if err != nil {
		t.Fatal("fixture must reach frozen actual-owner validation", err)
	}
	if _, err := Candidate([]AssetRef{page.Ref}, []ReleaseAsset{page, object}); err == nil {
		t.Fatal("matching false producer and consumer owners passed frozen history")
	}
}
