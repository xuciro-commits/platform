package platform

import (
	"slices"
	"testing"
)

func collaborationPage() Page {
	object := AssetRef{App: "sample", Kind: AssetObject, Name: "sample.note"}
	p := Page{Name: "collaboration", Object: object, Layout: "composed", Document: &PageDocument{FormatVersion: 2, UIProfile: PageUIProfile(), Root: "root", Nodes: map[string]PageLayoutNode{"root": {Kind: "rows", Children: []string{"table"}}, "table": {Kind: "widget", Section: "table"}}, Variables: map[string]PageVariable{
		"active": {Scope: "page", Type: "record", Mode: "resource", Source: &PageResourceSource{Kind: "record", Section: "table"}},
		"draft":  {Scope: "page", Type: "string", Mode: "state", Initial: Raw("")}, "file": {Scope: "page", Type: "string", Mode: "state", Initial: Raw("")}, "page": {Scope: "page", Type: "string", Mode: "state", Initial: Raw("1")},
	}}, Sections: []Section{{ID: "table", Widget: "table", ConfigVersion: 1}}}
	for _, s := range []Section{{ID: "comments", Widget: "record-comments", ConfigVersion: 1, RecordVariable: "active", CommentDraftVariable: "draft"}, {ID: "uploader", Widget: "record-uploader", ConfigVersion: 1, RecordVariable: "active", FileVariable: "file"}, {ID: "preview", Widget: "media-preview", ConfigVersion: 1, RecordVariable: "active", FileVariable: "file"}, {ID: "pdf", Widget: "pdf-viewer", ConfigVersion: 1, RecordVariable: "active", FileVariable: "file", PdfPageVariable: "page"}} {
		p.Sections = append(p.Sections, s)
		p.Document.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
		n := p.Document.Nodes[p.Document.Root]
		n.Children = append(n.Children, s.ID)
		p.Document.Nodes[p.Document.Root] = n
	}
	return p
}
func TestCollaborationOriginalResourcesPortsAndProfile(t *testing.T) {
	p := collaborationPage()
	if err := p.Document.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	if err := p.CheckRecordPorts(); err != nil {
		t.Fatal(err)
	}
	if p.RecordResourceObject("active") != p.Object {
		t.Fatal("original record owner was lost")
	}
	if (*PageDocument)(nil).Check(p.Sections) == nil {
		t.Fatal("documentless collaboration accepted")
	}
	for _, tc := range []struct {
		name   string
		change func(*Page)
	}{
		{"old profile", func(p *Page) { p.Document.UIProfile = "platform.page.v2.75" }},
		{"draft constant", func(p *Page) {
			v := p.Document.Variables["draft"]
			v.Mode = "constant"
			p.Document.Variables["draft"] = v
		}},
		{"upload output constant", func(p *Page) {
			v := p.Document.Variables["file"]
			v.Mode = "constant"
			p.Document.Variables["file"] = v
		}},
		{"record constant", func(p *Page) {
			p.Document.Variables["active"] = PageVariable{Scope: "page", Type: "string", Mode: "constant", Initial: Raw("R1")}
		}},
		{"wrong scalar type", func(p *Page) {
			p.Document.Variables["draft"] = PageVariable{Scope: "page", Type: "boolean", Mode: "state", Initial: Raw(false)}
		}},
		{"missing scalar", func(p *Page) { p.Sections[1].CommentDraftVariable = "" }},
		{"extra media binding", func(p *Page) { p.Sections[1].FileVariable = "file" }},
		{"ordinary fields", func(p *Page) { p.Sections[1].Fields = []string{"name"} }},
		{"crossobject actions", func(p *Page) {
			p.Sections[1].Actions = []AssetRef{{App: "relations", Kind: AssetAction, Name: "platform.comment.add"}}
		}},
		{"page counter aliases file", func(p *Page) { p.Sections[4].PdfPageVariable = "file" }},
		{"constant page counter", func(p *Page) {
			v := p.Document.Variables["page"]
			v.Mode = "constant"
			p.Document.Variables["page"] = v
		}},
		{"unrelated widget binding", func(p *Page) { p.Sections[1].Widget = "detail" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := collaborationPage()
			tc.change(&p)
			if p.Document.Check(p.Sections) == nil {
				t.Fatal("invalid collaboration accepted")
			}
		})
	}
	// A preview may explicitly name an attachment through original readonly text.
	for _, i := range []int{3, 4} {
		p := collaborationPage()
		p.Document.Variables["fixedFile"] = PageVariable{Scope: "page", Type: "string", Mode: "constant", Initial: Raw("FILE-1")}
		p.Sections[i].FileVariable = "fixedFile"
		if err := p.Document.Check(p.Sections); err != nil {
			t.Fatal(err)
		}
	}
	p = collaborationPage()
	p.Sections[1].Object = AssetRef{App: "other", Kind: AssetObject, Name: p.Object.Name}
	if p.CheckRecordPorts() == nil {
		t.Fatal("same type name acquired another owner")
	}
}
func TestCollaborationSameProducerAliasesAndRetirement(t *testing.T) {
	p := collaborationPage()
	d := p.Document
	d.Variables["alias"] = d.Variables["active"]
	p.Sections[3].RecordVariable = "alias"
	if err := d.Check(p.Sections); err != nil {
		t.Fatal("same original producer alias refused", err)
	}
	d.Variables["other"] = PageVariable{Scope: "page", Type: "record", Mode: "resource", Source: &PageResourceSource{Kind: "record", Section: "otherTable"}}
	p.Sections = append(p.Sections, Section{ID: "otherTable", Widget: "table", ConfigVersion: 1})
	d.Nodes["otherTable"] = PageLayoutNode{Kind: "widget", Section: "otherTable"}
	n := d.Nodes[d.Root]
	n.Children = append(n.Children, "otherTable")
	d.Nodes[d.Root] = n
	for _, pair := range []struct {
		index  int
		scalar string
	}{{3, "file"}, {4, "page"}, {1, "draft"}} {
		original := p.Sections[pair.index].RecordVariable
		p.Sections[pair.index].RecordVariable = "other"
		if pair.scalar == "draft" {
			p.Sections = append(p.Sections, Section{ID: "otherComments", Widget: "record-comments", ConfigVersion: 1, RecordVariable: "active", CommentDraftVariable: "draft"})
			d.Nodes["otherComments"] = PageLayoutNode{Kind: "widget", Section: "otherComments"}
			n = d.Nodes[d.Root]
			n.Children = append(n.Children, "otherComments")
			d.Nodes[d.Root] = n
		}
		if d.Check(p.Sections) == nil {
			t.Fatal("shared collaboration state acquired a different original record")
		}
		p.Sections[pair.index].RecordVariable = original
	}
	p = collaborationPage()
	visible := p.Document.Visible(p.Sections[1:])
	if _, ok := visible.Variables["active"]; ok {
		t.Fatal("retired producer retained record resource")
	}
	for _, node := range visible.Nodes {
		if slices.Contains([]string{"comments", "uploader", "preview", "pdf"}, node.Section) {
			t.Fatal("retired producer retained collaboration")
		}
	}
	p = collaborationPage()
	delete(p.Document.Variables, "file")
	visible = p.Document.Visible(p.Sections)
	for _, node := range visible.Nodes {
		if slices.Contains([]string{"uploader", "preview", "pdf"}, node.Section) {
			t.Fatal("missing file port retained media consumer")
		}
	}
}
func collaborationAssets(p Page) []ReleaseAsset {
	assets := []ReleaseAsset{{Ref: p.Object, ContractVersion: 1, SourceVersion: "1", Body: Raw(EntityInfo{App: p.Object.App, Type: p.Object.Name})}}
	seen := map[AssetRef]bool{}
	for _, s := range p.Sections {
		for _, ref := range s.CollaborationDependencies() {
			if seen[ref] {
				continue
			}
			seen[ref] = true
			var body any
			if ref.Kind == AssetObject {
				body = map[string]any{"type": ref.Name, "entity": EntityInfo{App: ref.App, Type: ref.Name}}
			} else {
				body = Action{Schema: ref.Name, Target: map[string]string{"relations": "platform.comment", "files": "files.file"}[ref.App]}
			}
			assets = append(assets, ReleaseAsset{Ref: ref, ContractVersion: 1, SourceVersion: "1", Body: Raw(body)})
		}
	}
	return assets
}
func TestFrozenCollaborationFixedServiceClosure(t *testing.T) {
	p := collaborationPage()
	page, err := PageReleaseAsset("sample", "page-1", p)
	if err != nil {
		t.Fatal(err)
	}
	services := collaborationAssets(p)
	assets := append([]ReleaseAsset{page}, services...)
	if _, err := Candidate([]AssetRef{page.Ref}, assets); err != nil {
		t.Fatal(err)
	}
	for _, s := range p.Sections {
		for _, ref := range s.CollaborationDependencies() {
			if !slices.Contains(page.Requires, ref) {
				t.Fatalf("missing fixed dependency %s", ref)
			}
		}
	}
	// Tampering with only Requires cannot remove a service referenced by the page.
	for _, ref := range []AssetRef{{App: "relations", Kind: AssetAction, Name: "platform.comment.add"}, {App: "files", Kind: AssetObject, Name: "files.file"}, {App: "files", Kind: AssetAction, Name: "files.file.attach"}} {
		missing := page
		missing.Requires = slices.DeleteFunc(slices.Clone(page.Requires), func(r AssetRef) bool { return r == ref })
		if _, err := Candidate([]AssetRef{page.Ref}, append([]ReleaseAsset{missing}, services...)); err == nil {
			t.Fatalf("frozen service %s omitted", ref)
		}
	}
	// Service schemas have their original owner and target, independent of grants.
	for _, change := range []func([]ReleaseAsset){
		func(a []ReleaseAsset) {
			for i := range a {
				if a[i].Ref.App == "files" && a[i].Ref.Kind == AssetObject {
					a[i].Body = Raw(EntityInfo{App: "other", Type: "files.file"})
				}
			}
		},
		func(a []ReleaseAsset) {
			for i := range a {
				if a[i].Ref.Name == "files.file.attach" {
					a[i].Body = Raw(Action{Schema: "files.file.attach", Target: p.Object.Name})
				}
			}
		},
	} {
		bad := slices.Clone(assets)
		change(bad)
		if _, err := Candidate([]AssetRef{page.Ref}, bad); err == nil {
			t.Fatal("frozen service owner/schema changed")
		}
	}
}

func TestCollaborationExactOverlayOwnerAndLoopRefusal(t *testing.T) {
	p := collaborationPage()
	d := p.Document
	d.Nodes["panelRoot"] = d.Nodes[d.Root]
	d.Nodes[d.Root] = PageLayoutNode{Kind: "rows", Children: []string{"main"}}
	d.Nodes["main"] = PageLayoutNode{Kind: "widget", Section: "main"}
	p.Sections = append(p.Sections, Section{ID: "main", Widget: "text", ConfigVersion: 1})
	d.Variables["open"] = PageVariable{Scope: "page", Type: "boolean", Mode: "state", Initial: Raw(false)}
	d.Overlays = map[string]PageOverlay{"panel": {Kind: "modal", Root: "panelRoot", OpenVariable: "open", Title: "Collaboration"}}
	for _, id := range []string{"active", "draft", "file", "page"} {
		v := d.Variables[id]
		v.Scope, v.Owner = "overlay", "panel"
		d.Variables[id] = v
	}
	if err := d.Check(p.Sections); err != nil {
		t.Fatal("original overlay collaboration rejected", err)
	}
	n := d.Nodes["panelRoot"]
	n.Children = slices.DeleteFunc(n.Children, func(id string) bool { return id == "comments" })
	d.Nodes["panelRoot"] = n
	d.Nodes[d.Root] = PageLayoutNode{Kind: "rows", Children: []string{"main", "comments"}}
	if d.Check(p.Sections) == nil {
		t.Fatal("overlay collaboration escaped to page")
	}
	p = collaborationPage()
	d = p.Document
	d.Nodes["panelRoot"] = PageLayoutNode{Kind: "rows", Children: []string{"comments"}}
	n = d.Nodes[d.Root]
	n.Children = slices.DeleteFunc(n.Children, func(id string) bool { return id == "comments" })
	d.Nodes[d.Root] = n
	d.Variables["open"] = PageVariable{Scope: "page", Type: "boolean", Mode: "state", Initial: Raw(false)}
	d.Overlays = map[string]PageOverlay{"panel": {Kind: "modal", Root: "panelRoot", OpenVariable: "open", Title: "Collaboration"}}
	if d.Check(p.Sections) == nil {
		t.Fatal("page collaboration acquired an overlay owner")
	}
	p = collaborationPage()
	d = p.Document
	d.Queries = map[string]PageQuery{"read": {Object: p.Object, Limit: 2}}
	d.Variables["window"] = PageVariable{Scope: "page", Type: "object-set", Mode: "resource", Source: &PageResourceSource{Kind: "plan", Query: "read"}}
	d.Variables["item"] = PageVariable{Scope: "loop-item", Owner: "loop", Type: "record", Mode: "resource", Source: &PageResourceSource{Kind: "item", Node: "loop"}}
	d.Nodes["loop"] = PageLayoutNode{Kind: "loop", Children: []string{"comments"}, Loop: &PageLoop{Collection: "window", ItemVariable: "item", Limit: 2}}
	d.Nodes[d.Root] = PageLayoutNode{Kind: "rows", Children: []string{"table", "uploader", "preview", "pdf", "loop"}}
	if d.Check(p.Sections) == nil {
		t.Fatal("collaboration entered a loop template")
	}
}
