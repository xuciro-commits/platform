package platform

import (
	"encoding/json"
	"testing"
)

func recordWorkPage() Page {
	object := AssetRef{App: "sample", Kind: AssetObject, Name: "sample.note"}
	d := &PageDocument{FormatVersion: 2, UIProfile: PageUIProfile(), Root: "root", Nodes: map[string]PageLayoutNode{"root": {Kind: "rows", Children: []string{"actions", "tiles", "note"}}}, Queries: map[string]PageQuery{"rows": {Object: object, Limit: 50, Sort: []string{"id"}}, "cards": {Object: object, Limit: 8, Sort: []string{"id"}}}, Variables: map[string]PageVariable{"rows": {Scope: "page", Type: "object-set", Mode: "resource", Source: &PageResourceSource{Kind: "plan", Query: "rows"}}, "cards": {Scope: "page", Type: "object-set", Mode: "resource", Source: &PageResourceSource{Kind: "plan", Query: "cards"}}, "note": {Scope: "page", Type: "string", Mode: "state", Initial: Raw("Original note")}}}
	p := Page{Name: "work", Object: object, Layout: "composed", Document: d, Sections: []Section{{ID: "actions", Widget: "action-table", ConfigVersion: 1, CollectionVariable: "rows", Actions: []AssetRef{{App: "sample", Kind: AssetAction, Name: "sample.note.adjust"}}, ActionTable: &PageActionTable{Parameters: []PageActionParameter{{Parameter: "next", Field: "qty"}}}}, {ID: "tiles", Widget: "record-list", ConfigVersion: 1, CollectionVariable: "cards", CardLabel: "name", RecordList: &PageRecordList{Layout: "tiles"}}, {ID: "note", Widget: "notepad", ConfigVersion: 1, NotepadVariable: "note"}}}
	for _, s := range p.Sections {
		d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	}
	return p
}
func TestRecordWorkOriginalPayloadIdentityAndFrozenBindings(t *testing.T) {
	p := recordWorkPage()
	if err := p.Document.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	info := EntityInfo{App: "sample", Type: "sample.note", Fields: []FieldInfo{{Name: "name", Type: "text"}, {Name: "qty", Type: "integer"}}}
	action := Action{Schema: "sample.note.adjust", Target: info.Type, Payload: []Field{{Name: "next", Type: "integer", Required: true}}}
	if err := p.Sections[0].CheckActionTable(info, action); err != nil {
		t.Fatal(err)
	}
	if err := p.Sections[1].CheckRecordList(info); err != nil {
		t.Fatal(err)
	}
	asset, err := PageReleaseAsset("sample", "1", p)
	if err != nil {
		t.Fatal(err)
	}
	p.Sections[0].ActionTable.Parameters[0].Field = "later"
	v := p.Document.Variables["note"]
	v.Initial = Raw("later")
	p.Document.Variables["note"] = v
	var saved Page
	if json.Unmarshal(asset.Body, &saved) != nil || saved.Sections[0].ActionTable.Parameters[0].Field != "qty" || string(saved.Document.Variables["note"].Initial) != `"Original note"` {
		t.Fatal("candidate adopted draft")
	}
	for _, change := range []func(*Action){func(a *Action) { a.New = true }, func(a *Action) { a.Automation = true }, func(a *Action) { a.Target = "foreign.item" }, func(a *Action) { a.Payload = append(a.Payload, Field{Name: "missing", Type: "string"}) }, func(a *Action) { a.Payload[0].Type = "string" }, func(a *Action) { a.Payload[0].From = "other" }} {
		a := action
		a.Payload = append([]Field(nil), action.Payload...)
		change(&a)
		if saved.Sections[0].CheckActionTable(info, a) == nil {
			t.Fatal("incompatible original action accepted", a)
		}
	}
	private := info
	lookup := map[AssetRef]ReleaseAsset{saved.Object: {Ref: saved.Object, Body: Raw(info)}, saved.Sections[0].Actions[0]: {Ref: saved.Sections[0].Actions[0], Body: Raw(map[string]any{"schema": action.Schema, "target": action.Target, "action": action})}}
	if err := checkFrozenQueries(saved, lookup); err != nil {
		t.Fatal(err)
	}
	badAction := action
	badAction.Payload = []Field{{Name: "next", Type: "string"}}
	ref := saved.Sections[0].Actions[0]
	lookup[ref] = ReleaseAsset{Ref: ref, Body: Raw(map[string]any{"schema": action.Schema, "target": action.Target, "action": badAction})}
	if checkFrozenQueries(saved, lookup) == nil {
		t.Fatal("frozen candidate accepted incompatible nested payload")
	}
	badAction = action
	badAction.Target = "foreign.item"
	lookup[ref] = ReleaseAsset{Ref: ref, Body: Raw(map[string]any{"schema": action.Schema, "target": action.Target, "action": badAction})}
	if checkFrozenQueries(saved, lookup) == nil {
		t.Fatal("frozen candidate accepted different nested identity")
	}
	private.Fields = private.Fields[:1]
	if saved.Sections[0].CheckActionTable(private, action) == nil {
		t.Fatal("unreadable seed field accepted")
	}
	for _, q := range []NamedQuery{{Limit: 7}, {Sort: []string{"-qty"}}} {
		if saved.CheckRecordWorkQuery("cards", &Definition{Query: &q}) == nil {
			t.Fatal("retained capacity/order changed")
		}
	}
}
func TestRecordWorkRejectsForeignOwnerBudgetAndIdentityPorts(t *testing.T) {
	for name, change := range map[string]func(*Page){"old profile": func(p *Page) { p.Document.UIProfile = "platform.page.v2.80" }, "identity parameter": func(p *Page) { p.Sections[0].ActionTable.Parameters[0].Parameter = "revision" }, "partial map": func(p *Page) { p.Sections[0].ActionTable.Parameters = nil }, "tile budget": func(p *Page) { q := p.Document.Queries["cards"]; q.Limit = 9; p.Document.Queries["cards"] = q }, "action order": func(p *Page) {
		q := p.Document.Queries["rows"]
		q.Sort = []string{"-qty"}
		p.Document.Queries["rows"] = q
	}, "note owner": func(p *Page) {
		v := p.Document.Variables["note"]
		v.Scope = "overlay"
		v.Owner = "elsewhere"
		p.Document.Variables["note"] = v
	}, "business note": func(p *Page) { p.Sections[2].RecordVariable = "rows" }, "action selection": func(p *Page) { p.Sections[0].Selection = "invented" }, "foreign configuration": func(p *Page) { p.Sections[2].ActionTable = p.Sections[0].ActionTable }} {
		t.Run(name, func(t *testing.T) {
			p := recordWorkPage()
			change(&p)
			if p.Document.Check(p.Sections) == nil {
				t.Fatal("invalid work ports accepted")
			}
		})
	}
}
