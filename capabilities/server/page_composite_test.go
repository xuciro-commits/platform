package platformserver

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"platformserver/apps/build"
	"platformserver/platform"
)

func compositeRead(t *testing.T, tn *Tenant, who string, page platform.Page, section string) PageModelResult {
	t.Helper()
	tn.mu.Lock()
	defer tn.mu.Unlock()
	result, err := tn.readPageModel(memberOf(t, tn, who), PageModelRead{Draft: &page, Section: section, Limit: 200}, time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("composite read: %v %s", err.Code, err.Message)
	}
	return result
}

func TestPageCompositeMatrixUsesAuthorizedOriginalRuleAndRetainsRelease(t *testing.T) {
	at := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	compose := func() *Tenant { return builderTenant(t, "composite-matrix") }
	tn := compose()
	var entries []Entry
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	row := publishObject(t, tn, "builder", "ROW", map[string]any{"name": "resource", "title": "Resource", "fields": []build.Field{{Name: "name", Title: "Name", Type: "text"}, {Name: "capacity", Title: "Capacity", Type: "decimal"}}}, at)
	column := publishObject(t, tn, "builder", "COL", map[string]any{"name": "requirement", "title": "Requirement", "fields": []build.Field{{Name: "name", Title: "Name", Type: "text"}, {Name: "minimum", Title: "Minimum", Type: "decimal"}}}, at)
	for _, r := range []struct {
		id       string
		capacity float64
	}{{"press", 120}, {"drill", 40}} {
		decide(t, tn, "user", build.ID, row+".create", row, r.id, map[string]any{"name": r.id, "capacity": r.capacity}, at)
	}
	for _, r := range []struct {
		id      string
		minimum float64
	}{{"light", 30}, {"heavy", 100}} {
		decide(t, tn, "user", build.ID, column+".create", column, r.id, map[string]any{"name": r.id, "minimum": r.minimum}, at)
	}
	path := func(root, name string) platform.Expr {
		return platform.Expr{Op: "path", Kind: "decimal", Args: []platform.Expr{platform.ExprReference("reference", "input."+root)}, Path: []string{name}}
	}
	expr := platform.Expr{Op: ">=", Kind: "boolean", Args: []platform.Expr{path("row", "capacity"), path("column", "minimum")}}
	p := modelTestPage(row, []platform.Section{{ID: "matrix", Widget: "model-matrix", ConfigVersion: 1, Composite: &platform.PageComposite{Bindings: map[string]platform.ModelViewBinding{"rows": {Model: platform.ModelBinding{Type: row}}, "columns": {Model: platform.ModelBinding{Type: column}}}, Value: &expr}}})
	p.Document.UIProfile = platform.PageCompositeProfile
	result := compositeRead(t, tn, "builder", p, "matrix")
	if result.Hidden || result.Partial || len(result.Composite.Cells) != 4 {
		t.Fatalf("matrix: %+v", result)
	}
	for _, cell := range result.Composite.Cells {
		expected := cell.Row != "drill" || cell.Column != "heavy"
		if cell.Value != expected {
			t.Fatalf("original expression result: %+v", cell)
		}
	}
	// Both inputs are original identities. A subsequent model edit changes the
	// answer without replacing the page or adding client-side calculations.
	decide(t, tn, "user", build.ID, row+".edit", row, "drill", map[string]any{"capacity": 150.0}, at)
	for _, cell := range compositeRead(t, tn, "builder", p, "matrix").Composite.Cells {
		if cell.Value != true {
			t.Fatalf("stale cell: %+v", cell)
		}
	}
	sections := []build.Section{{ID: "matrix", Widget: "model-matrix", ConfigVersion: 1, Composite: p.Sections[0].Composite}}
	decide(t, tn, "builder", build.ID, "build.page.create", build.PageType, "PAGE", map[string]any{"name": p.Name, "title": p.Title, "object": row, "sections": sections, "document": p.Document}, at)
	decide(t, tn, "builder", build.ID, "build.page.publish", build.PageType, "PAGE", map[string]any{}, at)
	preview, err := tn.PreviewRelease(memberOf(t, tn, "builder"), platform.AssetPage, "PAGE")
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("composite closure: %+v %v", preview, err)
	}
	CheckReplay(t, tn, entries, compose)
}

func TestPageCompositeGraphLayersOverlayAndDAG(t *testing.T) {
	at := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	tn := builderTenant(t, "composite-graph")
	layer := publishObject(t, tn, "builder", "LAYER", map[string]any{"name": "division", "title": "Division", "fields": []build.Field{{Name: "name", Title: "Name", Type: "text"}}}, at)
	node := publishObject(t, tn, "builder", "NODE", map[string]any{"name": "unit", "title": "Unit", "fields": []build.Field{{Name: "name", Title: "Name", Type: "text"}, {Name: "division", Title: "Division", Type: "reference", Ref: layer}}}, at)
	edge := publishObject(t, tn, "builder", "EDGE", map[string]any{"name": "reporting", "title": "Reporting", "fields": []build.Field{{Name: "superior", Title: "Superior", Type: "reference", Ref: node}, {Name: "subordinate", Title: "Subordinate", Type: "reference", Ref: node}, {Name: "division", Title: "Division", Type: "reference", Ref: layer}}}, at)
	overlay := publishObject(t, tn, "builder", "OVERLAY", map[string]any{"name": "tenure", "title": "Tenure", "fields": []build.Field{{Name: "unit", Title: "Unit", Type: "reference", Ref: node}, {Name: "period", Title: "Period", Type: "text"}, {Name: "current", Title: "Current", Type: "boolean"}}}, at)
	decide(t, tn, "user", build.ID, layer+".create", layer, "division", map[string]any{"name": "Delivery"}, at)
	for _, id := range []string{"parent", "child"} {
		decide(t, tn, "user", build.ID, node+".create", node, id, map[string]any{"name": id, "division": "division"}, at)
	}
	decide(t, tn, "user", build.ID, edge+".create", edge, "report", map[string]any{"superior": "parent", "subordinate": "child"}, at)
	decide(t, tn, "user", build.ID, overlay+".create", overlay, "period", map[string]any{"unit": "child", "period": "2026-01 to 2026-12", "current": true}, at)
	p := modelTestPage(node, []platform.Section{{ID: "graph", Widget: "layered-graph", ConfigVersion: 1, Composite: &platform.PageComposite{Acyclic: true, Bindings: map[string]platform.ModelViewBinding{
		"nodes":  {Model: platform.ModelBinding{Type: node}, Roles: map[string]platform.FieldRef{"layer": {Name: "division"}, "detail": {Path: []string{"division", "name"}}}},
		"layers": {Model: platform.ModelBinding{Type: layer}}, "edges": {Model: platform.ModelBinding{Type: edge}, Roles: map[string]platform.FieldRef{"from": {Name: "superior"}, "to": {Name: "subordinate"}}},
		"overlay": {Model: platform.ModelBinding{Type: overlay}, Roles: map[string]platform.FieldRef{"node": {Name: "unit"}, "detail": {Name: "period"}, "current": {Name: "current"}}},
	}}}})
	p.Document.UIProfile = platform.PageCompositeProfile
	originalEdges := p.Sections[0].Composite.Bindings["edges"]
	p.Sections[0].Composite.Bindings["edges"] = platform.ModelViewBinding{Model: originalEdges.Model, Roles: map[string]platform.FieldRef{"to": {Name: "subordinate"}}, Expressions: map[string]platform.Expr{"from": platform.ExprReference("reference", "division")}}
	if err := p.CheckModelBindings(tn.entity, tn.modelCriteria); err == nil || !strings.Contains(err.Error(), "reference target differs") {
		t.Fatalf("expression changed the reference owner: %v", err)
	}
	p.Sections[0].Composite.Bindings["edges"] = originalEdges
	result := compositeRead(t, tn, "builder", p, "graph")
	if result.Partial || result.Hidden || len(result.Composite.Sources["nodes"].Records) != 2 || result.Composite.Sources["overlay"].Records[0]["$view.current"] != true {
		t.Fatalf("graph projection: %+v", result)
	}
	for _, row := range result.Composite.Sources["nodes"].Records {
		if row["$view.detail"] != "Delivery" {
			t.Fatalf("model path not projected: %+v", row)
		}
	}
	decide(t, tn, "user", build.ID, edge+".create", edge, "cycle", map[string]any{"superior": "child", "subordinate": "parent"}, at)
	tn.mu.Lock()
	_, err := tn.readPageModel(memberOf(t, tn, "builder"), PageModelRead{Draft: &p, Section: "graph", Limit: 200}, at)
	tn.mu.Unlock()
	if err == nil || !strings.Contains(err.Message, "cycle") {
		t.Fatalf("cyclic lineage accepted: %v", err)
	}
}

func TestPageCompositePartialNeverTurnsHiddenCellsIntoFalse(t *testing.T) {
	at := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	id := "composite-partial"
	owner := newRecordSetOwner(id)
	tn := composeTenant(t, id, []Seat{seatOf("writer", "invariants:operator", "build:builder"), seatOf("viewer", "invariants:observer", "build:builder")}, owner, build.New(id))
	for _, who := range []string{"writer", "viewer"} {
		decide(t, tn, who, "invariants", "invariants.entry.create", owner.entity.Type, who, validConstraintEntry(who), at)
	}
	expr := platform.ExprLiteralValue("boolean", true)
	p := modelTestPage(owner.entity.Type, []platform.Section{{ID: "matrix", Widget: "model-matrix", ConfigVersion: 1, Composite: &platform.PageComposite{Bindings: map[string]platform.ModelViewBinding{"rows": {Model: platform.ModelBinding{Type: owner.entity.Type}}, "columns": {Model: platform.ModelBinding{Type: owner.entity.Type}}}, Value: &expr}}})
	p.Document.UIProfile = platform.PageCompositeProfile
	result := compositeRead(t, tn, "viewer", p, "matrix")
	if !result.Hidden || !result.Partial || len(result.Composite.Cells) > 0 || len(result.Composite.Sources) > 0 {
		t.Fatalf("hidden matrix leaked: %+v", result)
	}
	for name, b := range p.Sections[0].Composite.Bindings {
		b.Model.Visibility = "mark"
		p.Sections[0].Composite.Bindings[name] = b
	}
	result = compositeRead(t, tn, "viewer", p, "matrix")
	if result.Hidden || !result.Partial || len(result.Composite.Cells) != 1 || result.Composite.Cells[0].Value != true {
		t.Fatalf("marked matrix: %+v", result)
	}
	if !slices.Equal([]string{fmt.Sprint(result.Composite.Sources["rows"].Records[0]["id"])}, []string{"viewer"}) {
		t.Fatal("scope changed")
	}
	raw, _ := json.Marshal(result)
	if strings.Contains(string(raw), "writer") {
		t.Fatal("withheld axis identity leaked")
	}
}

func TestPageCompositeRefusesAnOversizedCrossProductWithoutWriting(t *testing.T) {
	at := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	tn := builderTenant(t, "composite-budget")
	typ := publishObject(t, tn, "builder", "AXIS", map[string]any{"name": "axis", "title": "Axis", "fields": []build.Field{{Name: "name", Title: "Name", Type: "text"}}}, at)
	for i := 0; i < 41; i++ {
		decide(t, tn, "user", build.ID, typ+".create", typ, fmt.Sprint(i), map[string]any{"name": fmt.Sprint(i)}, at)
	}
	expr := platform.ExprLiteralValue("boolean", true)
	p := modelTestPage(typ, []platform.Section{{ID: "matrix", Widget: "model-matrix", ConfigVersion: 1, Composite: &platform.PageComposite{Bindings: map[string]platform.ModelViewBinding{"rows": {Model: platform.ModelBinding{Type: typ}}, "columns": {Model: platform.ModelBinding{Type: typ}}}, Value: &expr}}})
	p.Document.UIProfile = platform.PageCompositeProfile
	before := txnState(tn)
	tn.mu.Lock()
	_, err := tn.readPageModel(memberOf(t, tn, "builder"), PageModelRead{Draft: &p, Section: "matrix", Limit: 200}, at)
	tn.mu.Unlock()
	if err == nil || !strings.Contains(err.Message, "1600") {
		t.Fatalf("oversized matrix accepted: %v", err)
	}
	if txnState(tn) != before {
		t.Fatal("refused view changed authoritative records")
	}
}
