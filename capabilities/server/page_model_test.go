package platformserver

import (
	agentowner "platformserver/engine/agents"
	"platformserver/studio/build"
)

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"platformserver/platform"
)

func modelTestPage(object string, bindings []platform.PageDataBinding) platform.Page {
	p := platform.Page{Format: 3, Name: "model", Title: "Model", Object: platform.ModelObject(object), DataBindings: bindings,
		InputSchema:     platform.PageInputSchema{Variables: map[string]platform.PageVariable{}},
		ScopeVisibility: platform.PageScopeVisibility{Root: "root", Owners: map[string]string{}, Scopes: []platform.PageScope{{ID: "root", Kind: "page"}}},
		WidgetSpec:      platform.PageWidgetSpec{Format: "platform.page.web.v1", Body: json.RawMessage(`{"sections":[]}`)}}
	for i, binding := range bindings {
		if binding.Model == nil && len(binding.Models) == 0 {
			binding.Model = &platform.ModelBinding{Type: object}
		}
		if binding.Object.Name == "" {
			if model := binding.PrimaryModel(); model != nil {
				binding.Object = platform.ModelObject(model.Type)
			}
		}
		binding.Owner = binding.ID
		p.DataBindings[i] = binding
		p.ScopeVisibility.Owners[binding.ID] = binding.ID
		p.ScopeVisibility.Scopes = append(p.ScopeVisibility.Scopes, platform.PageScope{ID: binding.ID, Parent: "root", Kind: "page", Binding: binding.ID})
	}
	return p
}

func modelDraftPayload(page platform.Page) map[string]any {
	return map[string]any{"format": 3, "name": page.Name, "title": page.Title, "object": page.Object.Name, "dataBindings": page.DataBindings, "actionTriggers": page.ActionTriggers, "inputSchema": page.InputSchema, "scopeVisibility": page.ScopeVisibility, "widgetSpec": page.WidgetSpec}
}

func TestPageSectionActionsSurviveSavedDraftAndPublishedReads(t *testing.T) {
	at := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	tn := builderTenant(t, "page-section-actions")
	object := publishObject(t, tn, "builder", "OBJECT", map[string]any{"name": "sectionactions", "title": "Section actions", "fields": []build.Field{{Name: "name", Title: "Name", Type: "text"}}}, at)
	action := object + ".edit"
	page := modelTestPage(object, []platform.PageDataBinding{platform.PageDataBinding{ID: "inherit", Kind: "records"}, platform.PageDataBinding{ID: "deny", Kind: "records"}, platform.PageDataBinding{ID: "narrow", Kind: "records"}})
	page.Name, page.Title = "sectionactions", "Section actions"
	ref := platform.AssetRef{App: build.ID, Kind: platform.AssetAction, Name: action}
	page.ActionTriggers = []platform.PageActionTrigger{{ID: "page", Binding: "page", Actions: []platform.AssetRef{ref}}, {ID: "inherit", Binding: "inherit"}, {ID: "deny", Binding: "deny", Actions: []platform.AssetRef{}}, {ID: "narrow", Binding: "narrow", Actions: []platform.AssetRef{ref}}}
	decide(t, tn, "builder", build.ID, "build.page.create", build.PageType, "PAGE", modelDraftPayload(page), at)
	readDraft := func() build.Page {
		t.Helper()
		view, err := tn.RecordOf(memberOf(t, tn, "builder"), build.PageType, "PAGE", at)
		if err != nil {
			t.Fatal(err)
		}
		var saved build.Page
		if err := json.Unmarshal(platform.Raw(view.Record), &saved); err != nil {
			t.Fatal(err)
		}
		if saved.ActionTriggers[1].Actions != nil || saved.ActionTriggers[2].Actions == nil || len(saved.ActionTriggers[2].Actions) != 0 || !slices.Equal(saved.ActionTriggers[3].Actions, []platform.AssetRef{ref}) {
			t.Fatalf("draft read changed the original section restrictions: %+v", saved.ActionTriggers)
		}
		return saved
	}
	readDraft()
	decide(t, tn, "builder", build.ID, "build.page.publish", build.PageType, "PAGE", map[string]any{}, at)
	original := readDraft().Published
	for _, member := range []string{"builder", "user"} {
		var published *platform.Page
		for _, definition := range tn.Definitions(memberOf(t, tn, member)) {
			if definition.Ref == (platform.AssetRef{App: build.ID, Kind: platform.AssetPage, Name: "sectionactions"}) {
				published = definition.Page
			}
		}
		if published == nil || len(published.ActionTriggers) != 4 || len(published.ActionTriggers[0].Actions) != 1 || published.ActionTriggers[1].Actions != nil || published.ActionTriggers[2].Actions == nil || len(published.ActionTriggers[2].Actions) != 0 || !slices.Equal(published.ActionTriggers[3].Actions, []platform.AssetRef{ref}) {
			t.Fatalf("%s discovery lost page or section restrictions: %+v", member, published)
		}
	}
	decide(t, tn, "builder", build.ID, "build.page.edit", build.PageType, "PAGE", map[string]any{"title": "Reopened draft"}, at)
	if saved := readDraft(); saved.Published != original {
		t.Fatal("editing the draft rewrote its published bytes")
	}
}

// Dynamic pages and model tools may read owner projections but never bind a
// control-plane write, whatever the builder's own role could do directly.
func TestDynamicDefinitionsCannotBindStaticControlActions(t *testing.T) {
	at := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	tn := builderTenant(t, "static-control", agentowner.New("static-control"))
	object := publishObject(t, tn, "builder", "OBJECT", map[string]any{"name": "staticbind", "title": "Static bind", "fields": []build.Field{{Name: "name", Title: "Name", Type: "text"}}}, at)
	control := platform.AssetRef{App: build.ID, Kind: platform.AssetAction, Name: "build.object.publish"}
	page := modelTestPage(object, []platform.PageDataBinding{{ID: "records", Kind: "records"}})
	page.Name, page.Title = "controlbind", "Control bind"
	page.ActionTriggers = []platform.PageActionTrigger{{ID: "page", Binding: "page", Actions: []platform.AssetRef{control}}}
	decide(t, tn, "builder", build.ID, "build.page.create", build.PageType, "PAGE", modelDraftPayload(page), at)
	if got := refuse(t, tn, "builder", build.ID, "build.page.publish", build.PageType, "PAGE", map[string]any{}, at); !strings.Contains(got, "cannot bind static control action") {
		t.Fatalf("a published page bound a control-plane action: %s", got)
	}
	for _, definition := range tn.Definitions(memberOf(t, tn, "user")) {
		if definition.Ref.Name == "controlbind" && definition.Ref.Kind == platform.AssetPage {
			t.Fatalf("the refused page became discoverable: %+v", definition.Ref)
		}
	}
	agent := map[string]any{"name": "controller", "title": "Controller", "description": "Review", "instructions": "Report Ready", "tools": []string{control.Name}, "cases": []build.AgentCase{{Name: "answer", Goal: "Give a result", Expect: "Ready"}}}
	if got := refuse(t, tn, "builder", build.ID, "build.agent.create", build.AgentType, "AG", agent, at); !strings.Contains(got, "not an action of a published object") {
		t.Fatalf("an agent bound a control-plane action as a tool: %s", got)
	}
	proposal := map[string]any{"owner": build.ID, "action": control.Name, "target": "OBJECT", "payload": map[string]any{}}
	if got := refuse(t, tn, "builder", platform.AgentApp, platform.SchemaToolPropose, platform.RunType, "P1", proposal, at); got == "ok" || !strings.Contains(got, "cannot propose") {
		t.Fatalf("a model tool proposed a control-plane action: %s", got)
	}
}

// A finite capability vocabulary expresses requirements as a subset: each
// required capability implies that the candidate equipment has it. The exact
// same Expr is the original start guard and the selector's rule reference.
func TestPageModelDeclaredInverseFilteredAndNamedCollections(t *testing.T) {
	at := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	tn := builderTenant(t, "model-sets")
	parent := publishObject(t, tn, "builder", "PARENT", map[string]any{"name": "parent", "title": "Parent", "fields": []build.Field{{Name: "name", Title: "Name", Type: "text"}}}, at)
	child := publishObject(t, tn, "builder", "CHILD", map[string]any{"name": "child", "title": "Child", "fields": []build.Field{{Name: "parent", Title: "Parent", Type: "reference", Ref: parent, Inverse: "children"}, {Name: "number", Title: "Number", Type: "integer"}}}, at)
	positive := platform.Expr{Op: ">", Kind: "boolean", Args: []platform.Expr{platform.ExprReference("integer", "number"), platform.ExprLiteralValue("integer", "0")}}
	sets := []platform.RecordSet{{Name: "children", From: "children", Direction: "inverse", Object: child, Order: []platform.RecordSetSort{{Expr: platform.ExprReference("integer", "number")}}, Limit: 100}, {Name: "positive", From: "children", Direction: "inverse", Object: child, Where: []platform.Expr{positive}, Order: []platform.RecordSetSort{{Expr: platform.ExprReference("integer", "number")}}, Limit: 100}}
	decide(t, tn, "builder", build.ID, "build.object.edit", build.ObjectType, "PARENT", map[string]any{"sets": sets}, at)
	decide(t, tn, "builder", build.ID, "build.object.publish", build.ObjectType, "PARENT", map[string]any{}, at)
	decide(t, tn, "user", build.ID, parent+".create", parent, "parent", map[string]any{"name": "Parent"}, at)
	for i := -1; i < 4; i++ {
		decide(t, tn, "user", build.ID, child+".create", child, fmt.Sprint("child", i), map[string]any{"parent": "parent", "number": i}, at)
	}
	p := modelTestPage(parent, []platform.PageDataBinding{platform.PageDataBinding{Model: &platform.ModelBinding{Type: parent}, ID: "parent", Kind: "records"}, platform.PageDataBinding{Model: &platform.ModelBinding{Type: child, Set: &platform.ModelRecordSet{RecordVariable: "parent", Name: "children"}, Visibility: "mark"}, ID: "children", Kind: "records"}})
	p.InputSchema.Variables["parent"] = platform.PageVariable{Scope: "page", Type: "record", Mode: "resource", Source: &platform.PageResourceSource{Kind: "record", Section: "parent"}}
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
	p.DataBindings[1].Model.Set.Name = "positive"
	request.Offset = 0
	out = read()
	if out.Total != 3 || len(out.Records) != 2 {
		t.Fatalf("filtered set: %+v", out)
	}
	decide(t, tn, "builder", build.ID, "build.query.create", build.QueryType, "QUERY", map[string]any{"name": "children", "title": "Named children", "description": "Children of the selected parent", "object": child, "by": "parent", "sort": []string{"number"}, "limit": 2}, at)
	decide(t, tn, "builder", build.ID, "build.query.publish", build.QueryType, "QUERY", map[string]any{}, at)
	binding, ok := (hostView{t: tn}).ModelQueryBinding(platform.AssetRef{App: build.ID, Kind: platform.AssetQuery, Name: "children"})
	if !ok {
		t.Fatal("named query unavailable")
	}
	p.DataBindings[1].Model.Set = &platform.ModelRecordSet{RecordVariable: "parent", Query: &binding}
	request.Offset = 3
	out = read()
	if out.Total != 5 || len(out.Records) != 2 || out.Records[0]["number"].(json.Number).String() != "2" {
		t.Fatalf("retained query pagination: %+v", out)
	}
	// A composite consumes these same declarations in the original page
	// context, including its separate typed parent producer.
	cell := platform.ExprLiteralValue("boolean", true)
	p.DataBindings[1] = platform.PageDataBinding{Models: map[string]platform.ModelViewBinding{
		"rows":    {Model: platform.ModelBinding{Type: child, Set: &platform.ModelRecordSet{RecordVariable: "parent", Name: "positive"}}},
		"columns": {Model: platform.ModelBinding{Type: child, Set: &platform.ModelRecordSet{RecordVariable: "parent", Query: &binding}}},
	}, Value: &cell, ID: "children", Kind: "composite",
	}

	request.Offset = 0
	out = read()
	if out.Hidden || out.Partial || len(out.Composite.Sources["rows"].Records) != 3 || len(out.Composite.Sources["columns"].Records) != 5 || len(out.Composite.Cells) != 15 {
		t.Fatalf("composite collection context was lost: %+v", out)
	}
}

func TestPageModelMigrationPathsReadOnlyAndPublicationRefusals(t *testing.T) {
	at := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	tn := builderTenant(t, "model-paths")
	country := publishObject(t, tn, "builder", "COUNTRY", map[string]any{"name": "country", "title": "Country", "fields": []build.Field{{Name: "name", Title: "Country name", Type: "text"}, {Name: "secret", Title: "Secret", Type: "text", Read: []string{build.Builder}}}}, at)
	supplier := publishObject(t, tn, "builder", "SUPPLIER", map[string]any{"name": "supplier", "title": "Supplier", "fields": []build.Field{{Name: "country", Title: "Country", Type: "reference", Ref: country}}}, at)
	order := publishObject(t, tn, "builder", "ORDER", map[string]any{"name": "order", "title": "Order", "fields": []build.Field{{Name: "supplier", Title: "Supplier", Type: "reference", Ref: supplier}, {Name: "note", Title: "Note", Type: "text", Write: []string{build.Builder}}}}, at)
	for _, r := range []struct {
		typ, id string
		payload any
	}{{country, "country", map[string]any{"name": "Canada", "secret": "hidden"}}, {supplier, "supplier", map[string]any{"country": "country"}}, {order, "order", map[string]any{"supplier": "supplier", "note": "Original"}}} {
		decide(t, tn, "builder", build.ID, r.typ+".create", r.typ, r.id, r.payload, at)
	}
	legacy := platform.Page{Name: "model", Title: "Model", Layout: "composed", Object: platform.ModelObject(order), Sections: []platform.Section{{ID: "orders", Widget: "table", ConfigVersion: 1, Fields: []string{"note", "supplier.country.name"}}}, Document: &platform.PageDocument{FormatVersion: 2, UIProfile: "platform.page.v2.109", Root: "root", Nodes: map[string]platform.PageLayoutNode{"root": {Kind: "rows", Children: []string{"orders"}}, "orders": {Kind: "widget", Section: "orders"}}, Variables: map[string]platform.PageVariable{}}}
	before := platform.Raw(legacy)
	var migrated platform.Page
	_ = json.Unmarshal(before, &migrated)
	if err := migrated.NormalizeModelBindings(tn.entity); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, platform.Raw(legacy)) || migrated.Format != 3 || len(migrated.DataBindings) != 1 || len(migrated.Sections) > 0 || migrated.Document != nil {
		t.Fatal("migration changed historical bytes or kept duplicate ports")
	}
	if !slices.Equal(migrated.DataBindings[0].Fields[1].Path, []string{"supplier", "country", "name"}) {
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
	migrated.Name, migrated.Title = "orders", "Orders"
	decide(t, tn, "builder", build.ID, "build.page.create", build.PageType, "PAGE", modelDraftPayload(migrated), at)
	decide(t, tn, "builder", build.ID, "build.page.publish", build.PageType, "PAGE", map[string]any{}, at)
	request.Draft = nil
	request.Page = platform.AssetRef{App: build.ID, Kind: platform.AssetPage, Name: "orders"}
	tn.mu.Lock()
	out, err = tn.readPageModel(memberOf(t, tn, "user"), request, at)
	tn.mu.Unlock()
	if err != nil || out.Access["order"]["note"].Allow || out.Access["order"]["note"].Reason == "" || out.Access["order"]["supplier.country.name"].Allow {
		t.Fatalf("field access lost its owner reason: %+v %v", out, err)
	}
	for _, path := range [][]string{{"supplier", "missing", "name"}, {"supplier", "country", "secret"}, append([]string{"supplier"}, slices.Repeat([]string{"country"}, 16)...)} {
		p := migrated
		p.DataBindings = slices.Clone(migrated.DataBindings)
		model := *p.DataBindings[0].Model
		p.DataBindings[0].Model = &model
		model.Fields = []platform.FieldRef{{Path: path}}
		if err := (hostView{t: tn}).CheckPageModel(platform.NewCaller(runtime{tn}, memberOf(t, tn, "user"), build.ID, false, false), p); err == nil {
			t.Fatalf("publish accepted unavailable path %v", path)
		}
	}
}

func TestPageModelPartialVisibilityIsExplicitAndStrictByDefault(t *testing.T) {
	at := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	id := "model-partial"
	owner := newRecordSetOwner(id)
	tn := composeTenant(t, id, []platform.Seat{seatOf("writer", "invariants:operator", "build:builder"), seatOf("viewer", "invariants:observer", "build:builder")}, owner, build.New(id))
	for _, who := range []string{"writer", "viewer"} {
		decide(t, tn, who, "invariants", "invariants.entry.create", owner.entity.Type, who, validConstraintEntry(who), at)
	}
	p := modelTestPage(owner.entity.Type, []platform.PageDataBinding{platform.PageDataBinding{Model: &platform.ModelBinding{Type: owner.entity.Type, Visibility: "mark"}, ID: "rows", Kind: "records"}})
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
	p.DataBindings[0].Model.Visibility = ""
	out = read()
	if !out.Hidden || !out.Partial || out.Total != 0 || len(out.Records) > 0 || len(out.Access) > 0 {
		t.Fatalf("strict model exposed an incomplete collection: %+v", out)
	}
}
