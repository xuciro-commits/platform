package platform

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"
)

func TestModelBindingDefaultsMigrationAndDeclaredPaths(t *testing.T) {
	info := EntityInfo{Type: "sample.item", App: "sample", Display: "name", Fields: []FieldInfo{{Name: "name", Type: "text"}, {Name: "parent", Type: "reference", Ref: "sample.item", Inverse: "children"}, {Name: "state", Type: "choice", ReadOnly: true}}, Lifecycle: &LifecycleInfo{Field: "state", States: []State{{Name: "ready", Title: "Ready"}, {Name: "done", Title: "Done"}}}}
	lookup := func(typ string) (EntityInfo, bool) { return info, typ == info.Type }
	p := Page{Name: "model", Object: ModelObject(info.Type), Document: &PageDocument{UIProfile: PageModelProfile}, Sections: []Section{{ID: "list", Widget: "table", Model: &ModelBinding{Type: info.Type}}, {ID: "status", Widget: "status-tracker", Model: &ModelBinding{Type: info.Type}}}}
	original := Raw(p)
	effective := p.EffectiveModelPage(lookup)
	if !slices.Equal(effective.Sections[0].Fields, []string{"name", "parent", "state"}) || effective.Sections[1].StatusTracker.Field != "state" || !slices.Equal(effective.Sections[1].StatusTracker.Stages, []string{"ready", "done"}) {
		t.Fatal("model defaults do not follow the original object and lifecycle")
	}
	effective.Sections[0].Model.Fields = []FieldRef{{Name: "private"}}
	if !bytes.Equal(original, Raw(p)) {
		t.Fatal("member projection shares mutable model data with the installed page")
	}
	legacy := p
	_ = json.Unmarshal(Raw(p), &legacy)
	legacy.Document.UIProfile = "platform.page.v2.109"
	legacy.Sections = []Section{{ID: "list", Widget: "table", Fields: []string{"parent.parent.name"}}}
	before := Raw(legacy)
	migrated := Page{}
	_ = json.Unmarshal(before, &migrated)
	if err := migrated.NormalizeModelBindings(lookup); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, Raw(legacy)) || len(migrated.Sections[0].Fields) != 0 || migrated.Sections[0].Model.Type != info.Type {
		t.Fatal("migration mutated historical bytes or retained two bindings")
	}
	one := Raw(migrated)
	if err := migrated.NormalizeModelBindings(lookup); err != nil || !bytes.Equal(one, Raw(migrated)) {
		t.Fatal("canonical migration is not idempotent")
	}
	if err := migrated.CheckModelBindings(lookup, nil); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*ModelBinding){func(m *ModelBinding) { m.Type = "missing.type" }, func(m *ModelBinding) { m.Fields = []FieldRef{{Path: []string{"name", "name"}}} }, func(m *ModelBinding) { m.Fields = []FieldRef{{Path: slices.Repeat([]string{"parent"}, 17)}} }, func(m *ModelBinding) { m.Where = []Expr{ExprLiteralValue("text", "true")} }, func(m *ModelBinding) { value := ExprLiteralValue("boolean", true); m.Label = &value }, func(m *ModelBinding) {
		m.Fields = []FieldRef{{Name: "name", Slot: "recordPicker.labelField"}, {Name: "parent", Slot: "recordPicker.labelField"}}
	}} {
		var copy Page
		_ = json.Unmarshal(one, &copy)
		change(copy.Sections[0].Model)
		if copy.CheckModelBindings(lookup, nil) == nil {
			t.Fatal("invalid model binding survived publication validation")
		}
	}
}

func TestModelComputeResourceFollowsOriginalReferences(t *testing.T) {
	p, info, op := computeResourcePage()
	info.Fields = append(info.Fields, FieldInfo{Name: "source", Type: "reference", Ref: info.Type})
	c := *p.Document.Variables["number"].Source.Compute
	c.Inputs = map[string]Binding{"qty": {Source: "subject", Path: []string{"source", "source", "qty"}}}
	lookup := func(typ string) (EntityInfo, bool) { return info, typ == info.Type }
	if err := p.CheckComputeResource(c, info, op, "number", lookup); err != nil {
		t.Fatal(err)
	}
	c.Inputs["qty"] = Binding{Source: "subject", Path: []string{"source", "source", "unknown"}}
	if p.CheckComputeResource(c, info, op, "number", lookup) == nil {
		t.Fatal("computation accepted an undeclared final field")
	}
}

func TestModelBindingsFollowOriginalInterfaceQueryAfterEditing(t *testing.T) {
	root := ModelObject("sample.note")
	info := EntityInfo{App: root.App, Type: root.Name, Fields: []FieldInfo{{Name: "name", Type: "text"}, {Name: "secret", Type: "text"}}}
	lookup := func(typ string) (EntityInfo, bool) { return info, typ == info.Type }
	binding := AssetBinding{Ref: AssetRef{App: root.App, Kind: AssetQuery, Name: "coded"}, SourceVersion: "query-1"}
	shape := Interface{Name: "sample.coded", Title: "Coded", Description: "Common names", Fields: []InterfaceField{{Name: "name", Type: "text", Title: "Name"}}}
	named := NamedQuery{Name: "coded", Title: "Coded", Interface: shape.Name, InterfaceShape: &shape, Implementations: []string{root.Name}, Sort: []string{"id"}, Limit: 20}
	definition := Definition{Ref: binding.Ref, Version: binding.SourceVersion, Query: &named}
	p := Page{Name: "picker", Object: root, Layout: "composed", Sections: []Section{
		{ID: "picker", Widget: "record-picker", ConfigVersion: 1, CollectionVariable: "window", Model: &ModelBinding{Type: root.Name, Fields: []FieldRef{{Name: "name", Slot: "recordPicker.labelField"}}}},
		{ID: "card", Widget: "record-card", ConfigVersion: 1, RecordVariable: "selected", RecordCard: &PageRecordCard{Tone: "neutral"}, Model: &ModelBinding{Type: root.Name, Fields: []FieldRef{{Name: "name"}, {Name: "name", Slot: "recordCard.labelField"}}}},
	}, Document: &PageDocument{FormatVersion: 2, UIProfile: PageUIProfile(), Root: "root", Nodes: map[string]PageLayoutNode{"root": {Kind: "rows", Children: []string{"picker", "card"}}, "picker": {Kind: "widget", Section: "picker"}, "card": {Kind: "widget", Section: "card"}}, Queries: map[string]PageQuery{"coded": {Object: root, Sort: []string{"id"}, Limit: 20}}, Variables: map[string]PageVariable{
		"window":   {Scope: "page", Type: "object-set", Mode: "resource", Source: &PageResourceSource{Kind: "plan", Query: "coded"}},
		"selected": {Scope: "page", Type: "record", Mode: "resource", Source: &PageResourceSource{Kind: "record", Section: "picker"}},
	}}}
	beforeFields := [][]byte{Raw(p.Sections[0].Model.Fields), Raw(p.Sections[1].Model.Fields)}
	// Choosing the original typed query changes the document source, not the
	// existing normalized field ports submitted by the widget editors.
	p.Document.Queries["coded"] = PageQuery{Interface: shape.Name, Query: &binding, Sort: []string{"id"}, Limit: 20}
	beforeQuery := Raw(p.Document.Queries["coded"])
	if err := p.NormalizeModelBindings(lookup); err != nil {
		t.Fatal(err)
	}
	for i, section := range p.Sections {
		if section.Model.Type != shape.Name || !bytes.Equal(beforeFields[i], Raw(section.Model.Fields)) {
			t.Fatalf("section %s did not follow its original interface producer", section.ID)
		}
		if err := p.CheckInterfaceSection(p.EffectiveModelSection(section, lookup), &definition); err != nil {
			t.Fatalf("section %s lost its retained typed query: %v", section.ID, err)
		}
	}
	if !bytes.Equal(beforeQuery, Raw(p.Document.Queries["coded"])) {
		t.Fatal("normalizing model types changed the original query binding or version")
	}
	if err := p.CheckModelBindings(lookup, nil); err != nil {
		t.Fatal(err)
	}
	canonical := Raw(p)
	if err := p.NormalizeModelBindings(lookup); err != nil || !bytes.Equal(canonical, Raw(p)) {
		t.Fatal("interface model normalization is not idempotent")
	}
	card := p.Sections[1]
	model := *card.Model
	model.Fields = append(slices.Clone(model.Fields), FieldRef{Name: "secret"})
	card.Model = &model
	if p.CheckInterfaceSection(p.EffectiveModelSection(card, lookup), &definition) == nil {
		t.Fatal("interface card accepted a subtype-only field after normalization")
	}
	for _, change := range []func(*AssetBinding){func(b *AssetBinding) { b.Ref.Name = "other" }, func(b *AssetBinding) { b.SourceVersion = "query-2" }} {
		query := p.Document.Queries["coded"]
		changed := *query.Query
		change(&changed)
		query.Query = &changed
		if p.CheckInterfaceQuery(query, &definition) == nil {
			t.Fatal("normalized model accepted a different query source or version")
		}
	}
}
