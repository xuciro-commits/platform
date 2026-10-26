package platformserver

import "platformserver/internal/construction"

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
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

func TestPageDraftRulesUseSavedModelsWithoutPublishingThem(t *testing.T) {
	for _, installed := range []bool{false, true} {
		t.Run(map[bool]string{false: "unpublished object", true: "published object with a new field"}[installed], func(t *testing.T) {
			at := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
			tn := builderTenant(t, fmt.Sprint("page-draft-rules-", installed))
			member := memberOf(t, tn, "builder")
			const object = "build.draftrules"
			fields := []construction.Field{{Name: "note", Title: "Note", Type: "text"}}
			decide(t, tn, "builder", construction.ID, "build.object.create", construction.ObjectType, "OBJECT", map[string]any{"name": "draftrules", "title": "Draft rules", "fields": fields}, at)
			page := modelTestPage(object, []platform.Section{{PageWidgetConfig: platform.PageWidgetConfig{Model: &platform.ModelBinding{Type: object}}, ID: "records", Widget: "table", ConfigVersion: 1}})
			if installed {
				decide(t, tn, "builder", construction.ID, "build.object.publish", construction.ObjectType, "OBJECT", map[string]any{}, at)
				decide(t, tn, "builder", construction.ID, "build.page.create", construction.PageType, "PAGE", map[string]any{"name": page.Name, "title": page.Title, "object": object, "sections": modelAuthoringSections(page.Sections), "document": page.Document}, at)
				decide(t, tn, "builder", construction.ID, "build.page.publish", construction.PageType, "PAGE", map[string]any{}, at)
			}
			beforeObject, _ := platform.Get[construction.Object](tn.automation(construction.ID, false), "OBJECT")
			beforePage, _ := platform.Get[build.Page](tn.automation(construction.ID, false), "PAGE")
			fields = append(fields, construction.Field{Name: "followup", Title: "Follow up", Type: "boolean"})
			decide(t, tn, "builder", construction.ID, "build.object.edit", construction.ObjectType, "OBJECT", map[string]any{"fields": fields}, at)
			ref := platform.ModelObject(object)
			path := platform.Expr{Op: "path", Kind: "boolean", Args: []platform.Expr{platform.ExprReference("reference", "selected")}, Path: []string{"followup"}}
			page.Document.Variables = map[string]platform.PageVariable{
				"selected": {Scope: "page", Type: "record", Mode: "resource", Source: &platform.PageResourceSource{Kind: "record", Section: "records"}},
				"followup": {Scope: "page", Type: "boolean", Mode: "property", Source: &platform.PageResourceSource{Kind: "property", Variable: "selected", Object: &ref, Field: "followup"}},
				"visible":  {Scope: "page", Type: "boolean", Mode: "derived", Rule: &path},
			}
			if installed {
				decide(t, tn, "builder", construction.ID, "build.page.edit", construction.PageType, "PAGE", map[string]any{"document": page.Document}, at)
			} else {
				decide(t, tn, "builder", construction.ID, "build.page.create", construction.PageType, "PAGE", map[string]any{"name": page.Name, "title": page.Title, "object": object, "sections": modelAuthoringSections(page.Sections), "document": page.Document}, at)
			}
			decide(t, tn, "builder", construction.ID, "build.page.edit", construction.PageType, "PAGE", map[string]any{"title": "Saved draft rules"}, at)
			saved, _ := platform.Get[build.Page](tn.automation(construction.ID, false), "PAGE")
			resourceType := saved.Document.Variables["selected"].RuleType
			propertyType := saved.Document.Variables["followup"].RuleType
			if resourceType == nil || resourceType.Fields["followup"].Kind != "boolean" || propertyType == nil || propertyType.Kind != "boolean" || saved.Document.Variables["visible"].Rule == nil {
				t.Fatalf("saved rules did not retain their draft field types: %+v", saved.Document.Variables)
			}
			if got := refuse(t, tn, "builder", construction.ID, "build.page.publish", construction.PageType, "PAGE", map[string]any{}, at); got == "ok" {
				t.Fatal("direct publication used an unselected object draft")
			}
			if alone, err := tn.PreviewRelease(member, platform.AssetPage, "PAGE"); err == nil && alone.Diagnostic == "" {
				t.Fatal("page-only candidate used an unselected object draft")
			}
			selection := []construction.JointDraftRef{{Kind: platform.AssetObject, ID: "OBJECT"}, {Kind: platform.AssetPage, ID: "PAGE"}}
			joint, err := tn.PreviewReleaseDrafts(member, selection)
			if err != nil || joint.Diagnostic != "" || joint.CandidateID == "" {
				t.Fatalf("joint model and page rules: %+v %v", joint, err)
			}
			candidate, err := tn.SaveReleaseCandidates(member, selection, joint.CandidateID, "save-page-rules", at)
			if err != nil {
				t.Fatal(err)
			}
			afterObject, _ := platform.Get[construction.Object](tn.automation(construction.ID, false), "OBJECT")
			afterPage, _ := platform.Get[build.Page](tn.automation(construction.ID, false), "PAGE")
			if afterObject.Published != beforeObject.Published || afterPage.Published != beforePage.Published {
				t.Fatal("saving drafts or reviewing the candidate rewrote an original publication")
			}
			if _, err := tn.ActivateRelease(member, candidate, "activate-page-rules", at); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// A finite capability vocabulary expresses requirements as a subset: each
// required capability implies that the candidate equipment has it. The exact
// same Expr is the original start guard and the selector's rule reference.
func TestPageModelSelectorAndStartShareOneRuleAndRecover(t *testing.T) {
	at := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	compose := func() *Tenant { return builderTenant(t, "model-rule") }
	tn := compose()
	var entries []Entry
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	op := publishObject(t, tn, "builder", "OP", map[string]any{"name": "operation", "title": "Operation", "fields": []construction.Field{{Name: "name", Title: "Name", Type: "text"}, {Name: "cut", Title: "Requires cutting", Type: "boolean"}, {Name: "drill", Title: "Requires drilling", Type: "boolean"}}}, at)
	path := func(name string) platform.Expr {
		return platform.Expr{Op: "path", Kind: "boolean", Args: []platform.Expr{platform.ExprReference("reference", "input.operation")}, Path: []string{name}}
	}
	implication := func(name string) platform.Expr {
		return platform.Expr{Op: "or", Kind: "boolean", Args: []platform.Expr{{Op: "not", Kind: "boolean", Args: []platform.Expr{path(name)}}, platform.ExprReference("boolean", name)}}
	}
	rule := platform.Expr{Op: "and", Kind: "boolean", Args: []platform.Expr{implication("cut"), implication("drill")}}
	action := construction.Action{Name: "start", Title: "Start", From: []string{"ready"}, To: "running", Inputs: []construction.Input{{Name: "operation", Title: "Operation", Type: "reference", Ref: op, Required: true}, {Name: "note", Title: "Optional note", Type: "text"}}, Conditions: []construction.Condition{{Expr: &rule, Message: "Equipment lacks required capabilities"}}}
	eq := publishObject(t, tn, "builder", "EQ", map[string]any{"name": "equipment", "title": "Equipment", "fields": []construction.Field{{Name: "name", Title: "Name", Type: "text"}, {Name: "cut", Title: "Cutting", Type: "boolean"}, {Name: "drill", Title: "Drilling", Type: "boolean"}}, "states": []construction.State{{Name: "ready", Title: "Ready", Tone: "info"}, {Name: "running", Title: "Running", Tone: "success"}}, "actions": []construction.Action{action}}, at)
	decide(t, tn, "user", construction.ID, op+".create", op, "operation", map[string]any{"name": "Cut", "cut": true, "drill": false}, at)
	for _, row := range []struct {
		id         string
		cut, drill bool
	}{{"cut", true, false}, {"drill", false, true}, {"zero", false, false}} {
		decide(t, tn, "user", construction.ID, eq+".create", eq, row.id, map[string]any{"name": row.id, "cut": row.cut, "drill": row.drill}, at)
	}
	p := modelTestPage(op, []platform.Section{{PageWidgetConfig: platform.PageWidgetConfig{Model: &platform.ModelBinding{Type: op}}, ID: "operations", Widget: "table", ConfigVersion: 1}, {PageWidgetConfig: platform.PageWidgetConfig{Model: &platform.ModelBinding{Type: eq, Options: &platform.OptionsRef{Rule: &platform.ModelRule{Action: platform.AssetRef{App: construction.ID, Kind: platform.AssetAction, Name: eq + ".start"}, Inputs: map[string]string{"operation": "operation"}}}}}, ID: "equipment", Widget: "record-picker", ConfigVersion: 1}})
	p.Document.Variables["operation"] = platform.PageVariable{Scope: "page", Type: "record", Mode: "resource", Source: &platform.PageResourceSource{Kind: "record", Section: "operations"}}
	request := PageModelRead{Draft: &p, Section: "equipment", Records: map[string]PageModelRecord{"operation": {Object: op, ID: "operation"}}, Limit: 20, Sort: []string{"id"}}
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
	if out.Total != 1 || out.Records[0]["id"] != "cut" || out.Partial {
		t.Fatalf("subset candidates or initialized false changed: %+v", out)
	}
	if got := refuse(t, tn, "user", construction.ID, eq+".start", eq, "drill", map[string]any{"operation": "operation"}, at); !strings.Contains(got, action.Conditions[0].Message) {
		t.Fatalf("start did not use selector's rule: %s", got)
	}
	// Only the original action definition changes. The selector is unchanged.
	changed := platform.ExprReference("boolean", "drill")
	action.Conditions[0].Expr = &changed
	decide(t, tn, "builder", construction.ID, "build.object.edit", construction.ObjectType, "EQ", map[string]any{"actions": []construction.Action{action}}, at)
	decide(t, tn, "builder", construction.ID, "build.object.publish", construction.ObjectType, "EQ", map[string]any{}, at)
	out = read()
	if out.Total != 1 || out.Records[0]["id"] != "drill" {
		t.Fatalf("rule edit did not change candidates: %+v", out)
	}
	if got := refuse(t, tn, "user", construction.ID, eq+".start", eq, "cut", map[string]any{"operation": "operation"}, at); !strings.Contains(got, action.Conditions[0].Message) {
		t.Fatalf("rule edit did not change start: %s", got)
	}
	decide(t, tn, "user", construction.ID, eq+".start", eq, "drill", map[string]any{"operation": "operation"}, at)
	// A new model page is canonical in the accepted result and release closure.
	decide(t, tn, "builder", construction.ID, "build.page.create", construction.PageType, "PAGE", map[string]any{"name": p.Name, "title": p.Title, "object": op, "sections": modelAuthoringSections(p.Sections), "document": p.Document}, at)
	decide(t, tn, "builder", construction.ID, "build.page.publish", construction.PageType, "PAGE", map[string]any{}, at)
	preview, err := tn.PreviewRelease(memberOf(t, tn, "builder"), platform.AssetPage, "PAGE")
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("model closure: %+v %v", preview, err)
	}
	CheckReplay(t, tn, entries, compose)
}

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
