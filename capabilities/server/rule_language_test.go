package platformserver

import "platformserver/internal/construction"

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"platformserver/apps/build"
	"platformserver/platform"
)

func TestOneRuleAcrossBusinessFaces(t *testing.T) {
	tn := builderTenant(t, "rule-language")
	rule := platform.Expr{Op: "and", Kind: "boolean", Args: []platform.Expr{platform.ExprReference("boolean", "capable"), platform.ExprReference("boolean", "safe")}}
	publishObject(t, tn, "builder", "EQUIPMENT", map[string]any{"name": "equipment", "title": "Equipment", "fields": []construction.Field{{Name: "capable", Title: "Capable", Type: "boolean"}, {Name: "safe", Title: "Safe", Type: "boolean"}, {Name: "allowed", Title: "Allowed", Type: "boolean", FormulaExpr: &rule}}, "states": []construction.State{{Name: "ready", Title: "Ready"}}, "actions": []construction.Action{{Name: "start", Title: "Start", From: []string{"ready"}, Conditions: []construction.Condition{{Expr: &rule, Message: "Equipment must be capable and safe"}}}}}, txnAt)
	table := construction.Table{Name: "permission", Title: "Permission", Inputs: []construction.TableColumn{{Name: "capable", Type: "boolean"}, {Name: "safe", Type: "boolean"}}, Outputs: []construction.TableColumn{{Name: "allowed", Type: "boolean"}}, Rows: []construction.TableRow{{Expressions: []platform.Expr{rule, platform.ExprLiteralValue("boolean", true)}, Then: []string{"true"}}}, Default: []string{"false"}}
	decide(t, tn, "builder", construction.ID, "build.table.create", "build.table", "TABLE", map[string]any{"name": table.Name, "title": table.Title, "inputs": table.Inputs, "outputs": table.Outputs, "rows": table.Rows, "default": table.Default}, txnAt)
	decide(t, tn, "builder", construction.ID, "build.table.publish", "build.table", "TABLE", map[string]any{}, txnAt)
	page := platform.PageDocument{UIProfile: platform.PageUIProfile(), Variables: map[string]platform.PageVariable{"capable": {Type: "boolean", Scope: "page", Mode: "state", Initial: platform.Raw(true)}, "safe": {Type: "boolean", Scope: "page", Mode: "state", Initial: platform.Raw(true)}, "visible": {Type: "boolean", Scope: "page", Mode: "derived", Rule: &rule}}}
	if err := page.CheckVariables(); err != nil {
		t.Fatal(err)
	}
	for i, ok := range []bool{true, false} {
		id := []string{"GOOD", "BAD"}[i]
		decide(t, tn, "user", construction.ID, "build.equipment.create", "build.equipment", id, map[string]any{"capable": true, "safe": ok}, txnAt)
		view, refusal := tn.RecordOf(memberOf(t, tn, "user"), "build.equipment", id, txnAt)
		if refusal != nil {
			t.Fatal(refusal)
		}
		raw, _ := json.Marshal(view.Record)
		var row map[string]any
		json.Unmarshal(raw, &row)
		if row["allowed"] != ok {
			t.Fatalf("computed field differs: %s", raw)
		}
		if ok {
			decide(t, tn, "user", construction.ID, "build.equipment.start", "build.equipment", id, map[string]any{}, txnAt)
		} else {
			if reason := refuse(t, tn, "user", construction.ID, "build.equipment.start", "build.equipment", id, map[string]any{}, txnAt); !strings.Contains(reason, "capable") {
				t.Fatal(reason)
			}
		}
		out, err := tn.app(construction.ID).(*build.Build).Compute(context.Background(), "permission", platform.Raw(map[string]any{"capable": true, "safe": ok}))
		if err != nil {
			t.Fatal(err)
		}
		var result map[string]any
		json.Unmarshal(out, &result)
		if result["allowed"] != ok {
			t.Fatalf("table differs: %s", out)
		}
		compiled, err := platform.CompileExpr(rule, page.PageRuleSchema())
		if err != nil {
			t.Fatal(err)
		}
		visible, err := compiled.Eval(platform.ExprContext{Refs: map[string]platform.ExprValue{"capable": platform.ExprPresent("boolean", true), "safe": platform.ExprPresent("boolean", ok)}})
		if err != nil || string(visible.Value.Data) != []string{"true", "false"}[i] {
			t.Fatalf("page visibility differs: %+v %v", visible, err)
		}
	}
}

func TestStrictRuleFailureLeavesBusinessStateUntouched(t *testing.T) {
	tn := builderTenant(t, "rule-language")
	divide := platform.Expr{Op: "/", Kind: "decimal", Args: []platform.Expr{platform.ExprReference("decimal", "numerator"), platform.ExprReference("decimal", "denominator")}}
	publishObject(t, tn, "builder", "RATIO", map[string]any{"name": "ratio", "title": "Ratio", "fields": []construction.Field{{Name: "numerator", Title: "Numerator", Type: "decimal"}, {Name: "denominator", Title: "Denominator", Type: "decimal"}, {Name: "value", Title: "Value", Type: "decimal", FormulaExpr: &divide}}}, txnAt)
	before := txnState(tn)
	for _, payload := range []map[string]any{{"numerator": 10.0}, {"numerator": 10.0, "denominator": 0.0}} {
		reason := refuse(t, tn, "user", construction.ID, "build.ratio.create", "build.ratio", "REFUSED", payload, txnAt)
		if !strings.Contains(reason, "missing") && !strings.Contains(reason, "zero") {
			t.Fatal(reason)
		}
		if txnState(tn) != before {
			t.Fatal("refused rule changed business records")
		}
	}
}

func TestRuleMigrationRetainsFrozenPublicationBytes(t *testing.T) {
	tn := builderTenant(t, "rule-language")
	var entries []Entry
	tn.Record = func(e Entry) { entries = append(entries, e) }
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	// Saving each legacy surface produces Expr while leaving supplied immutable
	// publication bytes untouched. Restore applies accepted bytes without rules.

	object := map[string]any{"name": "migrated", "title": "Migrated", "fields": []construction.Field{{Name: "base", Title: "Base", Type: "decimal"}, {Name: "total", Title: "Total", Type: "decimal", Formula: "base * 2"}}, "states": []construction.State{{Name: "ready", Title: "Ready"}}, "actions": []construction.Action{{Name: "go", Title: "Go", From: []string{"ready"}, Conditions: []construction.Condition{{Field: "base", Operator: ">", Value: "0", Message: "Positive base required"}}}}}
	// Published is read-only: install the historical image through the original
	// fixture/accepted-state lane, then only edit a live definition.

	decide(t, tn, "builder", construction.ID, "build.object.create", construction.ObjectType, "MIGRATED", object, txnAt)
	read, refusal := tn.RecordOf(memberOf(t, tn, "builder"), construction.ObjectType, "MIGRATED", txnAt)
	if refusal != nil {
		t.Fatal(refusal)
	}
	var saved construction.Object
	raw, _ := json.Marshal(read.Record)
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved.Fields) != 2 || saved.Fields[1].Formula != "" || saved.Fields[1].FormulaExpr == nil || len(saved.Actions) != 1 {
		t.Fatal("legacy object was not saved as Expr")
	}
	action := saved.Actions[0]
	if len(action.Conditions) != 0 || action.Transaction == nil || action.Transaction.Guard == nil || action.Transaction.Guard.Op != ">" || action.Transaction.Message != "Positive base required" {
		t.Fatal("legacy condition was not moved into the business transaction guard")
	}
	table := map[string]any{"name": "cells", "title": "Cells", "inputs": []construction.TableColumn{{Name: "qty", Type: "number"}}, "outputs": []construction.TableColumn{{Name: "ok", Type: "boolean"}}, "rows": []construction.TableRow{{When: []string{"1..5"}, Then: []string{"true"}}}}
	decide(t, tn, "builder", construction.ID, "build.table.create", construction.TableType, "CELLS", table, txnAt)
	var tb construction.Table
	if raw, _ := json.Marshal(func() any {
		v, e := tn.RecordOf(memberOf(t, tn, "builder"), construction.TableType, "CELLS", txnAt)
		if e != nil {
			t.Fatal(e)
		}
		return v.Record
	}()); json.Unmarshal(raw, &tb) != nil || len(tb.Rows) != 1 || len(tb.Rows[0].Expressions) != 1 || len(tb.Rows[0].When) != 0 {
		t.Fatal("old table cells were not saved as Expr")
	}
	CheckReplay(t, tn, entries, func() *Tenant { return builderTenant(t, "rule-language") })

	e := platform.Expr{Op: "/", Kind: "decimal", Args: []platform.Expr{platform.ExprLiteralValue("decimal", "1"), platform.ExprLiteralValue("decimal", "0")}}
	p, err := platform.CompileExpr(e, platform.ExprSchema{})
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := p.EvalFrozenFormula(platform.ExprContext{})
	if err != nil || string(frozen.Value.Data) != `"0"` {
		t.Fatal("frozen legacy behavior changed")
	}
	if _, err = p.Eval(platform.ExprContext{}); err == nil {
		t.Fatal("new formula silently produced zero")
	}

}

func TestDeclaredRuleCollectionsRespectCallerScope(t *testing.T) {
	tn := composeTenant(t, "scoped-rules", []Seat{seatOf("builder", "build:builder"), seatOf("operator", "build:operator")}, build.New("scoped-rules"))
	publishObject(t, tn, "builder", "JOB", map[string]any{"name": "job", "title": "Job", "fields": []construction.Field{{Name: "title", Title: "Title", Type: "text"}}, "states": []construction.State{{Name: "ready", Title: "Ready"}}, "access": []construction.Access{{Role: "operator", Read: "all", Create: true, Edit: true}}}, txnAt)
	publishObject(t, tn, "builder", "CAP", map[string]any{"name": "capability", "title": "Capability", "fields": []construction.Field{{Name: "job", Title: "Job", Type: "reference", Ref: "build.job", Required: true}, {Name: "owner", Title: "Owner", Type: "text", Required: true}, {Name: "certified", Title: "Certified", Type: "boolean"}}, "scope": construction.ObjectScope{Owner: "owner"}, "access": []construction.Access{{Role: "operator", Read: "own", Create: true, Edit: true}}}, txnAt)
	decide(t, tn, "builder", construction.ID, "build.query.create", construction.QueryType, "CAPQUERY", map[string]any{"name": "capabilities", "title": "Capabilities", "description": "Capabilities for a job", "object": "build.capability", "by": "job", "limit": 200}, txnAt)
	decide(t, tn, "builder", construction.ID, "build.query.publish", construction.QueryType, "CAPQUERY", map[string]any{}, txnAt)
	decide(t, tn, "operator", construction.ID, "build.job.create", "build.job", "JOB1", map[string]any{"title": "Job 1"}, txnAt)
	for i, owner := range []string{"operator", "builder"} {
		decide(t, tn, "builder", construction.ID, "build.capability.create", "build.capability", []string{"VISIBLE", "HIDDEN"}[i], map[string]any{"job": "JOB1", "owner": owner, "certified": i == 0}, txnAt)
	}
	expr := platform.Expr{Op: "all", Kind: "boolean", Agg: &platform.ExprAggregate{Source: "capabilities"}, In: &platform.Expr{Kind: "boolean", Ref: "item.certified"}}
	spec := platform.RuleReadSpec{Object: "build.job", Collections: map[string]platform.RuleCollection{"capabilities": {App: construction.ID, Query: "capabilities", Version: 1, Object: "build.capability", Of: "$subject", Policy: "visible"}}}
	schema, err := platform.RuleSchema(expr, spec, tn.entity)
	if err != nil {
		t.Fatal(err)
	}
	program, err := platform.CompileExpr(expr, schema)
	if err != nil {
		t.Fatal(err)
	}
	for _, who := range []string{"operator", "builder"} {
		caller := tn.caller(memberOf(t, tn, who), tn.app(construction.ID), false)
		ctx, err := caller.RuleContext(expr, spec, tn.entity, map[string]any{"id": "JOB1"}, nil, txnAt)
		if err != nil {
			t.Fatal(err)
		}
		result, err := program.Eval(ctx)
		if err != nil {
			t.Fatal(err)
		}
		want := "false"
		if who == "operator" {
			want = "true"
		}
		if string(result.Value.Data) != want || len(result.Reasons) == 0 {
			t.Fatalf("%s: %+v", who, result)
		}
	}
	for _, who := range []string{"operator", "builder"} {
		caller := tn.caller(memberOf(t, tn, who), tn.app(construction.ID), false)
		ctx, err := caller.RuleContext(expr, spec, tn.entity, map[string]any{"id": "JOB1"}, nil, txnAt)
		if err != nil {
			t.Fatal(err)
		}
		count := platform.Expr{Op: "count", Kind: "integer", Agg: &platform.ExprAggregate{Source: "capabilities"}}
		p, err := platform.CompileExpr(count, schema)
		if err != nil {
			t.Fatal(err)
		}
		r, err := p.Eval(ctx)
		want := `"1"`
		if who == "builder" {
			want = `"2"`
		}
		if err != nil || string(r.Value.Data) != want {
			t.Fatalf("scoped count %s: %+v %v", who, r, err)
		}
		any := expr
		any.Op = "any"
		p, err = platform.CompileExpr(any, schema)
		if err != nil {
			t.Fatal(err)
		}
		r, err = p.Eval(ctx)
		if err != nil || string(r.Value.Data) != "true" {
			t.Fatalf("scoped any %s: %+v %v", who, r, err)
		}
	}

	spec.Collections["capabilities"] = platform.RuleCollection{App: construction.ID, Query: "capabilities", Version: 1, Object: "build.capability", Of: "$subject", Policy: "complete"}
	schema, err = platform.RuleSchema(expr, spec, tn.entity)
	if err != nil {
		t.Fatal(err)
	}
	program, err = platform.CompileExpr(expr, schema)
	if err != nil {
		t.Fatal(err)
	}
	caller := tn.caller(memberOf(t, tn, "operator"), tn.app(construction.ID), false)
	ctx, err := caller.RuleContext(expr, spec, tn.entity, map[string]any{"id": "JOB1"}, nil, txnAt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = program.Eval(ctx); err == nil || !strings.Contains(err.Error(), "withheld") {
		t.Fatalf("partial privilege pretended to be complete: %v", err)
	}
}

func TestTransactionRulesUseCapturedInputsAndRollback(t *testing.T) {
	tn := txnTenant(t)
	txnSetup(t, tn)
	txn := txnDefinition()
	guard := platform.Expr{Kind: "boolean", Ref: "input.permit"}
	txn.Guard = &guard
	txn.Message = "Permission is required"
	txn.Steps[4].When = &guard
	for i := range txn.Steps {
		for j := range txn.Steps[i].Changes {
			if txn.Steps[i].Changes[j].Field == "note" {
				txn.Steps[i].Changes[j] = platform.TxnSet{Field: "note", Expr: &platform.Expr{Op: "concat", Kind: "text", Args: []platform.Expr{platform.ExprReference("text", "input.label"), platform.ExprLiteralValue("text", "!")}}}
			}
		}
	}
	decide(t, tn, "builder", construction.ID, "build.object.edit", construction.ObjectType, "REQUEST", map[string]any{"actions": []construction.Action{{Name: "apply", Title: "Apply transaction", From: []string{"draft"}, Roles: []string{"operator", "limited"}, Inputs: []construction.Input{{Name: "label", Title: "Label", Type: "longtext"}, {Name: "permit", Title: "Permit", Type: "boolean", Required: true}}, Transaction: txn}}}, txnAt)
	decide(t, tn, "builder", construction.ID, "build.object.publish", construction.ObjectType, "REQUEST", map[string]any{}, txnAt)
	before := txnState(tn)
	reason := refuse(t, tn, "operator", construction.ID, "build.request.apply", "build.request", "operator", map[string]any{"label": "rule", "permit": false}, txnAt)
	if !strings.Contains(reason, "Permission") {
		t.Fatal(reason)
	}
	txnUnchanged(t, tn, before)
	decide(t, tn, "operator", construction.ID, "build.request.apply", "build.request", "operator", map[string]any{"label": "rule", "permit": true}, txnAt)
	view, err := tn.RecordOf(memberOf(t, tn, "operator"), "build.request", "operator", txnAt)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(view.Record)
	if !strings.Contains(string(raw), `"note":"rule!"`) {
		t.Fatalf("assignment bypassed Expr: %s", raw)
	}
}

func TestRulePathsDistinguishUnsetValuesFromWithheldFields(t *testing.T) {
	tn := builderTenant(t, "optional-rule-path")
	publishObject(t, tn, "builder", "SOURCE", map[string]any{"name": "optionalsource", "title": "Optional source", "fields": []construction.Field{{Name: "optional", Title: "Optional value", Type: "text"}, {Name: "optionalnumber", Title: "Optional number", Type: "integer"}, {Name: "private", Title: "Private value", Type: "text", Read: []string{"builder"}}, {Name: "privatecount", Title: "Private count", Type: "integer", Required: true, Read: []string{"builder"}}}}, txnAt)
	publishObject(t, tn, "builder", "READER", map[string]any{"name": "optionalreader", "title": "Optional reader", "fields": []construction.Field{{Name: "source", Title: "Source", Type: "reference", Ref: "build.optionalsource"}}}, txnAt)
	decide(t, tn, "builder", "build", "build.optionalsource.create", "build.optionalsource", "source", map[string]any{"privatecount": 0}, txnAt)
	caller := tn.caller(memberOf(t, tn, "user"), tn.app(construction.ID), false)
	raw, failure := caller.ReadRecord("build.optionalsource", "source", txnAt)
	var values map[string]json.RawMessage
	if failure != nil || json.Unmarshal(raw, &values) != nil {
		t.Fatal("the scoped source could not be read")
	}
	for _, name := range []string{"privatecount", "optionalnumber"} {
		if _, exists := values[name]; exists {
			t.Fatalf("%s was fabricated from a withheld or unset scalar", name)
		}
	}
	for _, field := range []string{"optional", "private"} {
		expr := platform.Expr{Op: "empty", Kind: "boolean", Args: []platform.Expr{{Op: "coalesce", Kind: "text", Args: []platform.Expr{{Op: "path", Kind: "text", Args: []platform.Expr{platform.ExprReference("reference", "source")}, Path: []string{field}}, platform.ExprLiteralValue("text", "")}}}}
		spec := platform.RuleReadSpec{Object: "build.optionalreader"}
		schema, err := platform.RuleSchema(expr, spec, tn.entity)
		if err != nil {
			t.Fatal(err)
		}
		program, err := platform.CompileExpr(expr, schema)
		if err != nil {
			t.Fatal(err)
		}
		captured, err := caller.RuleContext(expr, spec, tn.entity, map[string]any{"id": "reader", "source": "source"}, nil, txnAt)
		if err != nil {
			t.Fatal(err)
		}
		result, err := program.Eval(captured)
		if field == "optional" {
			if err != nil || string(result.Value.Data) != "true" {
				t.Fatalf("readable unset field became a permission refusal: %+v %v", result, err)
			}
			if !strings.Contains(strings.Join(captured.Sources, ","), "build.optionalsource/source#optional") {
				t.Fatal("an explicit default lost the provenance of its unset source")
			}
		} else if err == nil || !strings.Contains(err.Error(), "withheld") {
			t.Fatalf("hidden field was interpreted as unset: %+v %v", result, err)
		}
	}
}
