package platformserver

import "platformserver/internal/construction"

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"platformserver/apps/build"
	"platformserver/platform"
)

func modelTestPage(object string, sections []platform.Section) platform.Page {
	p := platform.Page{Name: "model", Title: "Model", Layout: "composed", Object: platform.ModelObject(object), Sections: sections, Document: &platform.PageDocument{FormatVersion: 2, UIProfile: platform.PageModelProfile, Root: "root", Nodes: map[string]platform.PageLayoutNode{"root": {Kind: "rows"}}, Variables: map[string]platform.PageVariable{}}}
	root := p.Document.Nodes["root"]
	for _, s := range sections {
		root.Children = append(root.Children, s.ID)
		p.Document.Nodes[s.ID] = platform.PageLayoutNode{Kind: "widget", Section: s.ID}
	}
	p.Document.Nodes["root"] = root
	return p
}

func modelAuthoringSections(sections []platform.Section) []build.Section {
	values := make([]build.Section, len(sections))
	for i, s := range sections {
		values[i] = build.Section{PageWidgetConfig: platform.PageWidgetConfig{Model: s.Model}, ID: s.ID, Widget: s.Widget, ConfigVersion: s.ConfigVersion, Title: s.Title}
	}
	return values
}

func TestPageSectionActionsSurviveSavedDraftAndPublishedReads(t *testing.T) {
	at := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	tn := builderTenant(t, "page-section-actions")
	object := publishObject(t, tn, "builder", "OBJECT", map[string]any{"name": "sectionactions", "title": "Section actions", "fields": []construction.Field{{Name: "name", Title: "Name", Type: "text"}}}, at)
	action := object + ".edit"
	page := modelTestPage(object, []platform.Section{{ID: "inherit", Widget: "table", ConfigVersion: 1}, {ID: "deny", Widget: "table", ConfigVersion: 1}, {ID: "narrow", Widget: "table", ConfigVersion: 1}})
	sections := []build.Section{{ID: "inherit", Widget: "table", ConfigVersion: 1, Fields: []string{"name"}}, {ID: "deny", Widget: "table", ConfigVersion: 1, Fields: []string{"name"}, Actions: []string{}}, {ID: "narrow", Widget: "table", ConfigVersion: 1, Fields: []string{"name"}, Actions: []string{action}}}
	decide(t, tn, "builder", construction.ID, "build.page.create", construction.PageType, "PAGE", map[string]any{"name": "sectionactions", "title": "Section actions", "object": object, "actions": []string{action}, "sections": sections, "document": page.Document}, at)
	readDraft := func() build.Page {
		t.Helper()
		view, err := tn.RecordOf(memberOf(t, tn, "builder"), construction.PageType, "PAGE", at)
		if err != nil {
			t.Fatal(err)
		}
		var saved build.Page
		if err := json.Unmarshal(platform.Raw(view.Record), &saved); err != nil {
			t.Fatal(err)
		}
		if saved.Sections[0].Actions != nil || saved.Sections[1].Actions == nil || len(saved.Sections[1].Actions) != 0 || !slices.Equal(saved.Sections[2].Actions, []string{action}) {
			t.Fatalf("draft read changed the original section restrictions: %+v", saved.Sections)
		}
		return saved
	}
	readDraft()
	decide(t, tn, "builder", construction.ID, "build.page.publish", construction.PageType, "PAGE", map[string]any{}, at)
	original := readDraft().Published
	for _, member := range []string{"builder", "user"} {
		var published *platform.Page
		for _, definition := range tn.Definitions(memberOf(t, tn, member)) {
			if definition.Ref == (platform.AssetRef{App: construction.ID, Kind: platform.AssetPage, Name: "sectionactions"}) {
				published = definition.Page
			}
		}
		if published == nil || len(published.Actions) != 1 || published.Sections[0].Actions != nil || published.Sections[1].Actions == nil || len(published.Sections[1].Actions) != 0 || len(published.Sections[2].Actions) != 1 || published.Sections[2].Actions[0].Name != action {
			t.Fatalf("%s discovery lost page or section restrictions: %+v", member, published)
		}
	}
	decide(t, tn, "builder", construction.ID, "build.page.edit", construction.PageType, "PAGE", map[string]any{"title": "Reopened draft"}, at)
	if saved := readDraft(); saved.Published != original {
		t.Fatal("editing the draft rewrote its published bytes")
	}
}

// A finite capability vocabulary expresses requirements as a subset: each
// required capability implies that the candidate equipment has it. The exact
// same Expr is the original start guard and the selector's rule reference.
func TestPageModelDeclaredInverseFilteredAndNamedCollections(t *testing.T) {
	at := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	tn := builderTenant(t, "model-sets")
	parent := publishObject(t, tn, "builder", "PARENT", map[string]any{"name": "parent", "title": "Parent", "fields": []construction.Field{{Name: "name", Title: "Name", Type: "text"}}}, at)
	child := publishObject(t, tn, "builder", "CHILD", map[string]any{"name": "child", "title": "Child", "fields": []construction.Field{{Name: "parent", Title: "Parent", Type: "reference", Ref: parent, Inverse: "children"}, {Name: "number", Title: "Number", Type: "integer"}}}, at)
	positive := platform.Expr{Op: ">", Kind: "boolean", Args: []platform.Expr{platform.ExprReference("integer", "number"), platform.ExprLiteralValue("integer", "0")}}
	sets := []platform.RecordSet{{Name: "children", From: "children", Direction: "inverse", Object: child, Order: []platform.RecordSetSort{{Expr: platform.ExprReference("integer", "number")}}, Limit: 100}, {Name: "positive", From: "children", Direction: "inverse", Object: child, Where: []platform.Expr{positive}, Order: []platform.RecordSetSort{{Expr: platform.ExprReference("integer", "number")}}, Limit: 100}}
	decide(t, tn, "builder", construction.ID, "build.object.edit", construction.ObjectType, "PARENT", map[string]any{"sets": sets}, at)
	decide(t, tn, "builder", construction.ID, "build.object.publish", construction.ObjectType, "PARENT", map[string]any{}, at)
	decide(t, tn, "user", construction.ID, parent+".create", parent, "parent", map[string]any{"name": "Parent"}, at)
	for i := -1; i < 4; i++ {
		decide(t, tn, "user", construction.ID, child+".create", child, fmt.Sprint("child", i), map[string]any{"parent": "parent", "number": i}, at)
	}
	p := modelTestPage(parent, []platform.Section{{PageWidgetConfig: platform.PageWidgetConfig{Model: &platform.ModelBinding{Type: parent}}, ID: "parent", Widget: "table", ConfigVersion: 1}, {PageWidgetConfig: platform.PageWidgetConfig{Model: &platform.ModelBinding{Type: child, Set: &platform.ModelRecordSet{RecordVariable: "parent", Name: "children"}, Visibility: "mark"}}, ID: "children", Widget: "table", ConfigVersion: 1}})
	p.Document.Variables["parent"] = platform.PageVariable{Scope: "page", Type: "record", Mode: "resource", Source: &platform.PageResourceSource{Kind: "record", Section: "parent"}}
	request := PageModelRead{Draft: &p, Section: "children", Records: map[string]PageModelRecord{"parent": {Object: parent, ID: "parent"}}, Offset: 1, Limit: 2, Sort: []string{"number"}}
	read := func() PageModelResult {
		tn.mu.Lock()
		defer tn.mu.Unlock()
		out, err := tn.readPageModel(memberOf(t, tn, "builder"), request, at)
		if err != nil {
			t.Fatalf("model read refused: code=%v message=%q result=%+v", err.Code, err.Message, out)
		}
		return out
	}
	out := read()
	if out.Total != 5 || len(out.Records) != 2 || out.Records[0]["number"].(json.Number).String() != "0" {
		t.Fatalf("inverse pagination: %+v", out)
	}
	p.Sections[1].Model.Set.Name = "positive"
	request.Offset = 0
	out = read()
	if out.Total != 3 || len(out.Records) != 2 {
		t.Fatalf("filtered set: %+v", out)
	}
	decide(t, tn, "builder", construction.ID, "build.query.create", construction.QueryType, "QUERY", map[string]any{"name": "children", "title": "Named children", "description": "Children of the selected parent", "object": child, "by": "parent", "sort": []string{"number"}, "limit": 2}, at)
	decide(t, tn, "builder", construction.ID, "build.query.publish", construction.QueryType, "QUERY", map[string]any{}, at)
	binding, ok := (hostView{t: tn}).ModelQueryBinding(platform.AssetRef{App: construction.ID, Kind: platform.AssetQuery, Name: "children"})
	if !ok {
		t.Fatal("named query unavailable")
	}
	p.Sections[1].Model.Set = &platform.ModelRecordSet{RecordVariable: "parent", Query: &binding}
	request.Offset = 3
	out = read()
	if out.Total != 5 || len(out.Records) != 2 || out.Records[0]["number"].(json.Number).String() != "2" {
		t.Fatalf("retained query pagination: %+v", out)
	}
	// A composite consumes these same declarations in the original page
	// context, including its separate typed parent producer.
	cell := platform.ExprLiteralValue("boolean", true)
	p.Sections[1] = platform.Section{PageWidgetConfig: platform.PageWidgetConfig{Composite: &platform.PageComposite{Bindings: map[string]platform.ModelViewBinding{
		"rows":    {Model: platform.ModelBinding{Type: child, Set: &platform.ModelRecordSet{RecordVariable: "parent", Name: "positive"}}},
		"columns": {Model: platform.ModelBinding{Type: child, Set: &platform.ModelRecordSet{RecordVariable: "parent", Query: &binding}}},
	}, Value: &cell}}, ID: "children", Widget: "model-matrix", ConfigVersion: 1,
	}
	p.Document.UIProfile = platform.PageCompositeProfile
	request.Offset = 0
	out = read()
	if out.Hidden || out.Partial || len(out.Composite.Sources["rows"].Records) != 3 || len(out.Composite.Sources["columns"].Records) != 5 || len(out.Composite.Cells) != 15 {
		t.Fatalf("composite collection context was lost: %+v", out)
	}
}

func TestPageModelMigrationPathsReadOnlyAndPublicationRefusals(t *testing.T) {
	at := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	tn := builderTenant(t, "model-paths")
	country := publishObject(t, tn, "builder", "COUNTRY", map[string]any{"name": "country", "title": "Country", "fields": []construction.Field{{Name: "name", Title: "Country name", Type: "text"}, {Name: "secret", Title: "Secret", Type: "text", Read: []string{construction.Builder}}}}, at)
	supplier := publishObject(t, tn, "builder", "SUPPLIER", map[string]any{"name": "supplier", "title": "Supplier", "fields": []construction.Field{{Name: "country", Title: "Country", Type: "reference", Ref: country}}}, at)
	order := publishObject(t, tn, "builder", "ORDER", map[string]any{"name": "order", "title": "Order", "fields": []construction.Field{{Name: "supplier", Title: "Supplier", Type: "reference", Ref: supplier}, {Name: "note", Title: "Note", Type: "text", Write: []string{construction.Builder}}}}, at)
	for _, r := range []struct {
		typ, id string
		payload any
	}{{country, "country", map[string]any{"name": "Canada", "secret": "hidden"}}, {supplier, "supplier", map[string]any{"country": "country"}}, {order, "order", map[string]any{"supplier": "supplier", "note": "Original"}}} {
		decide(t, tn, "builder", construction.ID, r.typ+".create", r.typ, r.id, r.payload, at)
	}
	legacy := modelTestPage(order, []platform.Section{{ID: "orders", Widget: "table", ConfigVersion: 1, Fields: []string{"note", "supplier.country.name"}}})
	legacy.Document.UIProfile = "platform.page.v2.109"
	before := platform.Raw(legacy)
	var migrated platform.Page
	_ = json.Unmarshal(before, &migrated)
	if err := migrated.NormalizeModelBindings(tn.entity); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, platform.Raw(legacy)) || migrated.Sections[0].Object.Name != "" || len(migrated.Sections[0].Fields) > 0 || migrated.Document.UIProfile != platform.PageModelProfile {
		t.Fatal("migration changed historical bytes or kept duplicate ports")
	}
	if !slices.Equal(migrated.Sections[0].Model.Fields[1].Path, []string{"supplier", "country", "name"}) {
		t.Fatal("multi-hop path was not canonical")
	}
	request := PageModelRead{Draft: &migrated, Section: "orders", Limit: 25}
	tn.mu.Lock()
	out, err := tn.readPageModel(memberOf(t, tn, "builder"), request, at)
	tn.mu.Unlock()
	if err != nil || out.Records[0]["supplier.country.name"] != "Canada" {
		t.Fatalf("multi-hop model read: %+v %v", out, err)
	}
	// The user consumes the installed page, with original per-field decisions.
	decide(t, tn, "builder", construction.ID, "build.page.create", construction.PageType, "PAGE", map[string]any{"name": "orders", "title": "Orders", "object": order, "sections": modelAuthoringSections(migrated.Sections), "document": migrated.Document}, at)
	decide(t, tn, "builder", construction.ID, "build.page.publish", construction.PageType, "PAGE", map[string]any{}, at)
	request.Draft = nil
	request.Page = platform.AssetRef{App: construction.ID, Kind: platform.AssetPage, Name: "orders"}
	tn.mu.Lock()
	out, err = tn.readPageModel(memberOf(t, tn, "user"), request, at)
	tn.mu.Unlock()
	if err != nil || out.Access["order"]["note"].Allow || out.Access["order"]["note"].Reason == "" || out.Access["order"]["supplier.country.name"].Allow {
		t.Fatalf("field access lost its owner reason: %+v %v", out, err)
	}
	for _, path := range [][]string{{"supplier", "missing", "name"}, {"supplier", "country", "secret"}, append([]string{"supplier"}, slices.Repeat([]string{"country"}, 16)...)} {
		p := migrated
		p.Sections = slices.Clone(migrated.Sections)
		model := *p.Sections[0].Model
		p.Sections[0].Model = &model
		model.Fields = []platform.FieldRef{{Path: path}}
		if err := (hostView{t: tn}).CheckPageModel(platform.NewCaller(runtime{tn}, memberOf(t, tn, "user"), construction.ID, false, false), p); err == nil {
			t.Fatalf("publish accepted unavailable path %v", path)
		}
	}
}

func TestPageModelPartialVisibilityIsExplicitAndStrictByDefault(t *testing.T) {
	at := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	id := "model-partial"
	owner := newRecordSetOwner(id)
	tn := composeTenant(t, id, []Seat{seatOf("writer", "invariants:operator", "build:builder"), seatOf("viewer", "invariants:observer", "build:builder")}, owner, build.New(id))
	for _, who := range []string{"writer", "viewer"} {
		decide(t, tn, who, "invariants", "invariants.entry.create", owner.entity.Type, who, validConstraintEntry(who), at)
	}
	p := modelTestPage(owner.entity.Type, []platform.Section{{PageWidgetConfig: platform.PageWidgetConfig{Model: &platform.ModelBinding{Type: owner.entity.Type, Visibility: "mark"}}, ID: "rows", Widget: "table", ConfigVersion: 1}})
	request := PageModelRead{Draft: &p, Section: "rows", Limit: 25}
	read := func() PageModelResult {
		tn.mu.Lock()
		defer tn.mu.Unlock()
		out, err := tn.readPageModel(memberOf(t, tn, "viewer"), request, at)
		if err != nil {
			t.Fatalf("model read refused: code=%v message=%q result=%+v", err.Code, err.Message, out)
		}
		return out
	}
	out := read()
	if !out.Partial || out.Hidden || out.Total != 1 || out.Records[0]["id"] != "viewer" {
		t.Fatalf("partial scope lost its explicit marker: %+v", out)
	}
	p.Sections[0].Model.Visibility = ""
	out = read()
	if !out.Hidden || !out.Partial || out.Total != 0 || len(out.Records) > 0 || len(out.Access) > 0 {
		t.Fatalf("strict model exposed an incomplete collection: %+v", out)
	}
}
