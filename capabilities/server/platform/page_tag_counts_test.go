package platform

import "testing"

func tagCountsPage() Page {
	p := treemapPage()
	s := &p.Sections[len(p.Sections)-1]
	s.Widget = "tag-counts"
	s.GroupSetVariable = ""
	s.GroupValueVariable = "tag"
	p.Document.Variables["tag"] = PageVariable{Scope: "page", Type: "string", Mode: "state", Initial: Raw("retired")}
	return p
}
func TestTagCountsOriginalGroupingAndOptionalStringOutput(t *testing.T) {
	p := tagCountsPage()
	if err := p.Document.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	i := len(p.Sections) - 1
	p.Sections[i].GroupValueVariable = ""
	if err := p.Document.Check(p.Sections); err != nil {
		t.Fatal("readonly tag groups rejected", err)
	}
	if (*PageDocument)(nil).Check([]Section{p.Sections[i]}) == nil {
		t.Fatal("documentless tags accepted")
	}
	for _, tc := range []struct {
		name   string
		change func(*Page)
	}{
		{"old profile", func(p *Page) { p.Document.UIProfile = "platform.page.v2.74" }},
		{"set output", func(p *Page) { p.Sections[i].GroupSetVariable = "chosen"; p.Sections[i].GroupValueVariable = "" }},
		{"wrong output type", func(p *Page) {
			p.Document.Variables["tag"] = PageVariable{Scope: "page", Type: "boolean", Mode: "state", Initial: Raw(false)}
		}},
		{"constant output", func(p *Page) { v := p.Document.Variables["tag"]; v.Mode = "constant"; p.Document.Variables["tag"] = v }},
		{"different owner", func(p *Page) {
			v := p.Document.Variables["tag"]
			v.Scope = "overlay"
			v.Owner = "other"
			p.Document.Variables["tag"] = v
		}},
		{"reserved grouping", func(p *Page) { p.Sections[i].Group = "count" }},
		{"date transformation", func(p *Page) { p.Sections[i].Group = "state:month" }},
		{"record window", func(p *Page) {
			v := p.Document.Variables["window"]
			v.Type = "record-set"
			p.Document.Variables["window"] = v
		}},
		{"single record", func(p *Page) { p.Sections[i].RecordVariable = "item" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := tagCountsPage()
			tc.change(&p)
			if p.Document.Check(p.Sections) == nil {
				t.Fatal("invalid tag counts accepted")
			}
		})
	}
	info := EntityInfo{Fields: []FieldInfo{{Name: "state", Type: "choice"}}}
	if err := p.Sections[i].CheckTerms(info); err != nil {
		t.Fatal(err)
	}
	info.Fields = nil
	if p.Sections[i].CheckTerms(info) == nil {
		t.Fatal("private tag field accepted")
	}
	info.Fields = []FieldInfo{{Name: "state", Type: "integer"}}
	if p.Sections[i].CheckTerms(info) == nil {
		t.Fatal("numeric tag field was coerced into labels")
	}
}
func TestTagCountsExactOwnerAndOriginalFrozenField(t *testing.T) {
	d, sections := overlayResourceDocument()
	d.Variables["tag"] = PageVariable{Scope: "overlay", Owner: "panel", Type: "string", Mode: "state", Initial: Raw("")}
	s := Section{ID: "tags", Widget: "tag-counts", ConfigVersion: 1, CollectionVariable: "window", Group: "note", GroupValueVariable: "tag"}
	sections = append(sections, s)
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	n := d.Nodes["body"]
	n.Children = append(n.Children, s.ID)
	d.Nodes["body"] = n
	if err := d.Check(sections); err != nil {
		t.Fatal(err)
	}
	// Moving the presentation keeps its original resource identity and must be refused.
	n.Children = n.Children[:len(n.Children)-1]
	d.Nodes["body"] = n
	n = d.Nodes[d.Root]
	n.Children = append(n.Children, s.ID)
	d.Nodes[d.Root] = n
	if d.Check(sections) == nil {
		t.Fatal("tags escaped their overlay collection owner")
	}
	p := tagCountsPage()
	fields := []FieldInfo{{Name: "bucket", Type: "text"}, {Name: "state", Type: "choice"}}
	lookup := map[AssetRef]ReleaseAsset{p.Object: {Ref: p.Object, Body: Raw(EntityInfo{Type: p.Object.Name, Fields: fields})}}
	if err := checkFrozenQueries(p, lookup); err != nil {
		t.Fatal(err)
	}
	lookup[p.Object] = ReleaseAsset{Ref: p.Object, Body: Raw(EntityInfo{Type: p.Object.Name, Fields: fields[:1]})}
	if checkFrozenQueries(p, lookup) == nil {
		t.Fatal("frozen tag grouping field was not checked")
	}
}

func TestTagCountsOriginalBooleanEnablePort(t *testing.T) {
	p := tagCountsPage()
	p.Document.Variables["enabled"] = PageVariable{Scope: "page", Type: "boolean", Mode: "state", Initial: Raw(false)}
	n := p.Document.Nodes["tree"]
	n.EnabledWhen = "enabled"
	p.Document.Nodes["tree"] = n
	if err := p.Document.Check(p.Sections); err != nil {
		t.Fatal("original boolean enable condition rejected", err)
	}
	if widgetPort("tag-counts", "enabledWhen") == nil || widgetPort("treemap", "enabledWhen") != nil {
		t.Fatal("tag enable port changed another presentation's contract")
	}
	p.Document.Variables["enabled"] = PageVariable{Scope: "page", Type: "string", Mode: "state", Initial: Raw("false")}
	if p.Document.Check(p.Sections) == nil {
		t.Fatal("tag enable condition coerced text to boolean")
	}
}
