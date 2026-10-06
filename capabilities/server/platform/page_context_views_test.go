package platform

import (
	"math"
	"slices"
	"strings"
	"testing"
)

func contextViewsPage() Page {
	object := AssetRef{App: "sample", Kind: AssetObject, Name: "sample.asset"}
	people := AssetRef{App: "sample", Kind: AssetObject, Name: "sample.person"}
	all := AssetBinding{Ref: AssetRef{App: "sample", Kind: AssetQuery, Name: "all-people"}, SourceVersion: "v1"}
	related := AssetBinding{Ref: AssetRef{App: "sample", Kind: AssetQuery, Name: "asset-people"}, SourceVersion: "v1"}
	height := 0.5
	caption := ""
	p := Page{Name: "context", Object: object, Layout: "composed", Sections: []Section{
		{ID: "table", Widget: "table", ConfigVersion: 1, Fields: []string{"name"}, CollectionVariable: "assets"},
		{ID: "crumb", Widget: "breadcrumb", ConfigVersion: 1, RecordVariable: "active", Breadcrumb: &PageBreadcrumb{HomeLabel: "Home", PageLabel: "Assets", LabelField: "name"}},
		{ID: "avatars", Widget: "avatar-stack", ConfigVersion: 1, Object: people, CollectionVariable: "all", Avatar: &PageAvatarStack{LabelField: "name", DetailFields: []string{"shift", "secret"}, ContextVariable: "active", ContextCollectionVariable: "related"}},
		{ID: "image", Widget: "static-image", ConfigVersion: 1, Image: &PageStaticImage{URL: "/assets/example.png", Caption: &caption, Height: &height}},
	}, Document: &PageDocument{FormatVersion: 2, UIProfile: PageUIProfile(), Root: "root", Nodes: map[string]PageLayoutNode{"root": {Kind: "rows", Children: []string{"table", "crumb", "avatars", "image"}}}, Variables: map[string]PageVariable{
		"assets":  {Scope: "page", Type: "object-set", Mode: "resource", Source: &PageResourceSource{Kind: "plan", Query: "assets"}},
		"active":  {Scope: "page", Type: "record", Mode: "resource", Source: &PageResourceSource{Kind: "record", Section: "table"}},
		"all":     {Scope: "page", Type: "object-set", Mode: "resource", Source: &PageResourceSource{Kind: "plan", Query: "all"}},
		"related": {Scope: "page", Type: "object-set", Mode: "resource", Source: &PageResourceSource{Kind: "plan", Query: "related"}},
	}, Queries: map[string]PageQuery{
		"assets":  {Object: object, Limit: 6, Sort: []string{"id"}},
		"all":     {Object: people, Query: &all, Limit: 6, Sort: []string{"id"}},
		"related": {Object: people, Query: &related, For: &PageValue{Variable: "active"}, Limit: 6, Sort: []string{"id"}},
	}, Events: []PageEventBinding{{Source: "crumb", Event: "click", Control: "home", Effects: []PageEffect{{Kind: "navigate", Navigate: &PageNavigation{Page: AssetRef{App: "sample", Kind: AssetPage, Name: "home"}}}}}}}}
	for _, s := range p.Sections {
		p.Document.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	}
	return p
}
func contextViewsLookup(p Page) map[AssetRef]ReleaseAsset {
	people := p.Sections[2].Object
	all := p.Document.Queries["all"].Query.Ref
	related := p.Document.Queries["related"].Query.Ref
	return map[AssetRef]ReleaseAsset{
		p.Object: {Ref: p.Object, SourceVersion: "v1", ContractVersion: 1, Body: Raw(EntityInfo{App: p.Object.App, Type: p.Object.Name, Fields: []FieldInfo{{Name: "name", Type: "text"}}})},
		people:   {Ref: people, SourceVersion: "v1", ContractVersion: 1, Body: Raw(EntityInfo{App: people.App, Type: people.Name, Fields: []FieldInfo{{Name: "name", Type: "text"}, {Name: "asset", Type: "reference", Ref: p.Object.Name}, {Name: "shift", Type: "choice"}, {Name: "secret", Type: "text"}}})},
		all:      {Ref: all, SourceVersion: "v1", ContractVersion: 1, Body: Raw(NamedQuery{Object: people.Name})},
		related:  {Ref: related, SourceVersion: "v1", ContractVersion: 1, Body: Raw(NamedQuery{Object: people.Name, By: "asset"})},
	}
}
func TestContextViewsNativeOriginalBindings(t *testing.T) {
	p := contextViewsPage()
	if err := p.Document.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	if err := p.CheckRecordPorts(); err != nil {
		t.Fatal(err)
	}
	if err := p.CheckCollectionPorts(); err != nil {
		t.Fatal(err)
	}
	if err := checkFrozenQueries(p, contextViewsLookup(p)); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		change func(*Page)
	}{
		{"old profile", func(p *Page) { p.Document.UIProfile = "platform.page.v2.77" }},
		{"wrong widget", func(p *Page) { p.Sections[1].Widget = "text" }},
		{"missing breadcrumb", func(p *Page) { p.Sections[1].Breadcrumb = nil }},
		{"missing home", func(p *Page) { p.Document.Events = nil }},
		{"wrong home control", func(p *Page) { p.Document.Events[0].Control = "current" }},
		{"home scalar write", func(p *Page) { p.Document.Events[0].Effects[0] = PageEffect{Kind: "set", Target: "active", Value: Raw(true)} }},
		{"home arguments", func(p *Page) {
			p.Document.Events[0].Effects[0].Navigate.Inputs = map[string]PageValue{"arg": {Variable: "active"}}
		}},
		{"title without record", func(p *Page) { p.Sections[1].RecordVariable = "" }},
		{"breadcrumb shared record", func(p *Page) {
			v := p.Document.Variables["active"]
			v.Mode = "shared"
			p.Document.Variables["active"] = v
		}},
		{"avatar detail budget", func(p *Page) { p.Sections[2].Avatar.DetailFields = []string{"one", "two", "three"} }},
		{"duplicate details", func(p *Page) { p.Sections[2].Avatar.DetailFields = []string{"shift", "shift"} }},
		{"title reused as detail", func(p *Page) { p.Sections[2].Avatar.DetailFields = []string{"name"} }},
		{"missing avatar config", func(p *Page) { p.Sections[2].Avatar = nil }},
		{"missing all window", func(p *Page) { p.Sections[2].CollectionVariable = "" }},
		{"ignored context", func(p *Page) { q := p.Document.Queries["related"]; q.For = nil; p.Document.Queries["related"] = q }},
		{"literal context", func(p *Page) {
			q := p.Document.Queries["related"]
			q.For = &PageValue{Literal: Raw("A")}
			p.Document.Queries["related"] = q
		}},
		{"missing context window", func(p *Page) { p.Sections[2].Avatar.ContextCollectionVariable = "" }},
		{"all context", func(p *Page) {
			q := p.Document.Queries["all"]
			q.For = &PageValue{Variable: "active"}
			p.Document.Queries["all"] = q
		}},
		{"wrong context object", func(p *Page) {
			q := p.Document.Queries["related"]
			q.Object = p.Object
			p.Document.Queries["related"] = q
		}},
		{"inline all query", func(p *Page) { q := p.Document.Queries["all"]; q.Query = nil; p.Document.Queries["all"] = q }},
		{"image object override", func(p *Page) { p.Sections[3].Object = p.Object }},
		{"image record binding", func(p *Page) { p.Sections[3].RecordVariable = "active" }},
		{"negative height", func(p *Page) { h := -0.1; p.Sections[3].Image.Height = &h }},
		{"height budget", func(p *Page) { h := 4096.1; p.Sections[3].Image.Height = &h }},
		{"nan height", func(p *Page) { h := math.NaN(); p.Sections[3].Image.Height = &h }},
		{"caption bytes", func(p *Page) { caption := strings.Repeat("中", 1366); p.Sections[3].Image.Caption = &caption }},
	}
	for _, binding := range []string{"all", "related"} {
		for _, field := range []string{"limit", "offset", "sort"} {
			binding, field := binding, field
			cases = append(cases, struct {
				name   string
				change func(*Page)
			}{binding + " " + field, func(p *Page) {
				q := p.Document.Queries[binding]
				switch field {
				case "limit":
					q.Limit = 7
				case "offset":
					q.Offset = 1
				case "sort":
					q.Sort = []string{"name", "id"}
				}
				p.Document.Queries[binding] = q
			}})
		}
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := contextViewsPage()
			tc.change(&p)
			if p.Document.Check(p.Sections) == nil {
				t.Fatal("invalid context view accepted")
			}
		})
	}
	if (*PageDocument)(nil).Check(p.Sections) == nil {
		t.Fatal("documentless views accepted")
	}
	for _, i := range []int{1, 2} {
		p := contextViewsPage()
		p.Sections[i].Object = AssetRef{App: "wrong", Kind: AssetObject, Name: p.Sections[i].Object.Name}
		if i == 1 {
			p.Sections[i].Object.Name = p.Object.Name
		}
		if p.CheckRecordPorts() == nil {
			t.Fatal("foreign complete object accepted")
		}
	}
	// A presentation-only breadcrumb can omit the record, and an avatar can omit context.
	p = contextViewsPage()
	p.Sections[1].RecordVariable = ""
	p.Sections[1].Breadcrumb.LabelField = ""
	p.Sections[2].Avatar.ContextVariable = ""
	p.Sections[2].Avatar.ContextCollectionVariable = ""
	delete(p.Document.Queries, "related")
	delete(p.Document.Variables, "related")
	if err := p.Document.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
}
func TestPageStaticImageLiteralURLPolicy(t *testing.T) {
	for _, raw := range []string{"", "/image.png", "/images/图.png?fit=cover#preview", "https://example.com/image.png", "HTTP://localhost:5433/img", "https://[::1]:8443/img"} {
		if !ValidPageImageURL(raw) {
			t.Errorf("valid original URL rejected %q", raw)
		}
	}
	for _, raw := range []string{"//example.com/image", "/%2fexample.com/image", "https://user:pass@example.com/a", "https://@example.com/a", "https:example.com/a", "data:image/png;base64,abc", "blob:https://example.com/id", "file:///image", "javascript:alert(1)", "https://example.com\\image", "https://example.com/%5cimage", "https://example.com/%20image", "https://example.com/\u0085image", "https://example.com/%C2%85image", "https://example.com/%ff", "https://example.com/%zz", "https://example.com:abc/image", "https://example.com:65536/image", "https://example.com:/image", "https://[invalid]/img", "https:///img", strings.Repeat("a", 4097)} {
		if ValidPageImageURL(raw) {
			t.Errorf("unsafe URL accepted %q", raw)
		}
	}
}
func TestContextViewsOriginalSchemaAndFrozenOwners(t *testing.T) {
	p := contextViewsPage()
	lookup := contextViewsLookup(p)
	people := p.Sections[2].Object
	for _, mutate := range []func(map[AssetRef]ReleaseAsset){
		func(l map[AssetRef]ReleaseAsset) {
			ref := p.Document.Queries["all"].Query.Ref
			a := l[ref]
			a.Body = Raw(NamedQuery{Object: people.Name, Limit: 5})
			l[ref] = a
		},
		func(l map[AssetRef]ReleaseAsset) {
			ref := p.Document.Queries["related"].Query.Ref
			a := l[ref]
			a.Body = Raw(NamedQuery{Object: people.Name, By: "asset", Limit: 5})
			l[ref] = a
		},

		func(l map[AssetRef]ReleaseAsset) {
			a := l[people]
			a.Body = Raw(EntityInfo{App: "wrong", Type: people.Name, Fields: []FieldInfo{{Name: "name", Type: "text"}}})
			l[people] = a
		},
		func(l map[AssetRef]ReleaseAsset) {
			ref := p.Document.Queries["related"].Query.Ref
			a := l[ref]
			a.Body = Raw(NamedQuery{Object: people.Name})
			l[ref] = a
		},
		func(l map[AssetRef]ReleaseAsset) {
			ref := p.Document.Queries["all"].Query.Ref
			a := l[ref]
			a.Body = Raw(NamedQuery{Object: people.Name, By: "asset"})
			l[ref] = a
		},
		func(l map[AssetRef]ReleaseAsset) {
			ref := p.Document.Queries["related"].Query.Ref
			a := l[ref]
			a.SourceVersion = "v2"
			l[ref] = a
		},
		func(l map[AssetRef]ReleaseAsset) { delete(l, p.Object) },
	} {
		lookup = contextViewsLookup(p)
		mutate(lookup)
		if checkFrozenQueries(p, lookup) == nil {
			t.Fatal("tampered original frozen edge accepted")
		}
	}
	info := EntityInfo{Fields: []FieldInfo{{Name: "name", Type: "text"}, {Name: "shift", Type: "choice"}, {Name: "secret", Type: "text"}}}
	if err := p.Sections[2].CheckContextViews(info); err != nil {
		t.Fatal(err)
	}
	info.Fields = info.Fields[1:]
	if p.Sections[2].CheckContextViews(info) == nil {
		t.Fatal("private title accepted")
	}
	p.Sections[2].Avatar.LabelField = "id"
	if err := p.Sections[2].CheckContextViews(info); err != nil {
		t.Fatal(err)
	}
}
func TestContextViewsExactOwnersAndVisibleDependencies(t *testing.T) {
	p := contextViewsPage()
	d := p.Document
	visible := d.Visible(p.Sections[1:])
	if _, ok := visible.Nodes["crumb"]; ok {
		t.Fatal("denied original record retained breadcrumb")
	}
	if _, ok := visible.Nodes["avatars"]; ok {
		t.Fatal("denied original context retained avatar")
	}
	if _, ok := visible.Nodes["image"]; !ok {
		t.Fatal("pure static image lost independent presentation")
	}
	// Dormant widgets retain exactly the same input ownership checks.
	root := d.Nodes["root"]
	root.Children = slices.DeleteFunc(root.Children, func(id string) bool { return id == "avatars" })
	d.Nodes["root"] = root
	d.UnusedWidgets = []PageUnusedWidget{{Node: "avatars", Parent: "root"}}
	if err := d.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	d.Nodes["panelRoot"] = PageLayoutNode{Kind: "rows", Children: []string{"avatars"}}
	d.Variables["open"] = PageVariable{Scope: "page", Type: "boolean", Mode: "state", Initial: Raw(false)}
	d.Overlays = map[string]PageOverlay{"panel": {Root: "panelRoot", Kind: "modal", Title: "People", OpenVariable: "open"}}
	d.UnusedWidgets = nil
	if d.Check(p.Sections) == nil {
		t.Fatal("page original context entered another overlay")
	}
	p = contextViewsPage()
	d = p.Document
	d.Queries["loop"] = PageQuery{Object: p.Object, Limit: 2}
	d.Variables["loopSet"] = PageVariable{Scope: "page", Type: "object-set", Mode: "resource", Source: &PageResourceSource{Kind: "plan", Query: "loop"}}
	d.Variables["item"] = PageVariable{Scope: "loop-item", Owner: "loop", Type: "record", Mode: "resource", Source: &PageResourceSource{Kind: "item", Node: "loop"}}
	d.Nodes["loop"] = PageLayoutNode{Kind: "loop", Children: []string{"image"}, Loop: &PageLoop{Collection: "loopSet", ItemVariable: "item", Limit: 2}}
	root = d.Nodes["root"]
	root.Children = []string{"table", "crumb", "avatars", "loop"}
	d.Nodes["root"] = root
	if d.Check(p.Sections) == nil {
		t.Fatal("static image entered unsupported loop")
	}
}

func overlayContextViewsPage() Page {
	p := contextViewsPage()
	d := p.Document
	original := d.Nodes["root"]
	d.Nodes["panelRoot"] = original
	d.Nodes["root"] = PageLayoutNode{Kind: "rows", Children: []string{"trigger"}}
	d.Nodes["trigger"] = PageLayoutNode{Kind: "widget", Section: "trigger"}
	p.Sections = append(p.Sections, Section{ID: "trigger", Widget: "button", ConfigVersion: 1})
	for id, v := range d.Variables {
		v.Scope = "overlay"
		v.Owner = "panel"
		d.Variables[id] = v
	}
	for id, q := range d.Queries {
		q.Owner = "panel"
		d.Queries[id] = q
	}
	d.Variables["open"] = PageVariable{Scope: "page", Type: "boolean", Mode: "state", Initial: Raw(false)}
	d.Overlays = map[string]PageOverlay{"panel": {Root: "panelRoot", Kind: "drawer", Title: "Original context panel", OpenVariable: "open"}}
	d.Events = append(d.Events, PageEventBinding{Source: "trigger", Event: "click", Effects: []PageEffect{{Kind: "set", Target: "open", Value: Raw(true)}}})
	return p
}
func TestContextViewsPlannedProducerInOriginalOverlay(t *testing.T) {
	p := overlayContextViewsPage()
	if err := p.Document.Check(p.Sections); err != nil {
		t.Fatal("same-overlay original planned table refused", err)
	}
	if err := p.CheckRecordPorts(); err != nil {
		t.Fatal(err)
	}
	if err := checkFrozenQueries(p, contextViewsLookup(p)); err != nil {
		t.Fatal("frozen original overlay refused", err)
	}
	if _, err := PageReleaseAsset("sample", "v1", p); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Page){
		func(p *Page) { q := p.Document.Queries["related"]; q.Owner = ""; p.Document.Queries["related"] = q },
		func(p *Page) {
			v := p.Document.Variables["active"]
			v.Owner = "foreign"
			p.Document.Variables["active"] = v
		},
		func(p *Page) {
			v := p.Document.Variables["active"]
			v.Scope = "page"
			v.Owner = ""
			p.Document.Variables["active"] = v
		},
		func(p *Page) {
			n := p.Document.Nodes["panelRoot"]
			n.Children = []string{"table", "crumb", "image"}
			p.Document.Nodes["panelRoot"] = n
			n = p.Document.Nodes["root"]
			n.Children = append(n.Children, "avatars")
			p.Document.Nodes["root"] = n
		},
	} {
		p := overlayContextViewsPage()
		change(&p)
		if p.Document.Check(p.Sections) == nil {
			t.Fatal("foreign owner accepted after allowing independent planned producer")
		}
	}
}
func TestAvatarContextRejectsActualPlannedFeedback(t *testing.T) {
	for _, kind := range []string{"direct", "transitive", "set"} {
		t.Run(kind, func(t *testing.T) {
			p := contextViewsPage()
			d := p.Document
			switch kind {
			case "direct":
				p.Sections[0].CollectionVariable = "related"
			case "transitive":
				d.Variables["fromContext"] = PageVariable{Scope: "page", Type: "record", Mode: "resource", Source: &PageResourceSource{Kind: "record", Section: "contextTable"}}
				p.Sections = append(p.Sections, Section{ID: "contextTable", Widget: "table", ConfigVersion: 1, CollectionVariable: "related"})
				q := d.Queries["assets"]
				q.For = &PageValue{Variable: "fromContext"}
				q.Query = d.Queries["related"].Query
				d.Queries["assets"] = q
			case "set":
				q := d.Queries["assets"]
				q.Set = &PageQuerySet{Op: "union", Inputs: []string{"related", "all"}}
				d.Queries["assets"] = q
			}
			if !d.variableDependsOnQuery("active", "related", p.Sections) {
				t.Fatal("actual producer feedback graph was not found")
			}
			if d.CheckQueries(p.Sections) == nil {
				t.Fatal("actual contextual query cycle accepted")
			}
		})
	}
}
