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
	effective := p.EffectiveModelPage(lookup)
	if !slices.Equal(effective.Sections[0].Fields, []string{"name", "parent", "state"}) || effective.Sections[1].StatusTracker.Field != "state" || !slices.Equal(effective.Sections[1].StatusTracker.Stages, []string{"ready", "done"}) {
		t.Fatal("model defaults do not follow the original object and lifecycle")
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
