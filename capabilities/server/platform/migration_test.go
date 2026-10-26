package platform

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func migrationFixture() (MigrationPlan, map[string]EntityInfo, map[string]EntityInfo) {
	from := EntityInfo{App: "build", Type: "build.asset", Fields: []FieldInfo{{Name: "name", Type: "text"}, {Name: "quantity", Type: "text"}, {Name: "note", Type: "text"}, {Name: "link", Type: "reference", Ref: "build.old-target"}}}
	to := from
	to.Fields = append([]FieldInfo{}, from.Fields...)
	to.Fields[0].ReadOnly, to.Fields[2].ReadOnly = true, true
	to.Fields[0].AliasOf = "title"
	to.Fields = append(to.Fields, FieldInfo{Name: "title", Type: "text"}, FieldInfo{Name: "amount", Type: "decimal"}, FieldInfo{Name: "enabled", Type: "boolean", Required: true}, FieldInfo{Name: "public", Type: "text"}, FieldInfo{Name: "private", Type: "text"}, FieldInfo{Name: "replacement", Type: "reference", Ref: "build.new-target"})
	parse := Expr{Kind: "decimal", Op: "parse-decimal", Args: []Expr{ExprReference("text", "quantity")}}
	literal := ExprLiteralValue("boolean", true)
	text := ExprReference("text", "note")
	plan := MigrationPlan{From: "sha256-v1:" + strings.Repeat("1", 64), To: "sha256-v1:" + strings.Repeat("2", 64), Scope: MigrationScope{Type: from.Type}, Steps: []MigrateStep{
		{Kind: "rename", Types: []string{from.Type}, Batch: 2, Fields: []MigrateField{{From: "name", To: "title"}}},
		{Kind: "default", Types: []string{from.Type}, Batch: 2, Fields: []MigrateField{{To: "enabled", Expr: &literal}}},
		{Kind: "retype", Types: []string{from.Type}, Batch: 2, OnFailure: "quarantine", Fields: []MigrateField{{From: "quantity", To: "amount", Expr: &parse}}},
		{Kind: "split", Types: []string{from.Type}, Batch: 2, Fields: []MigrateField{{From: "note", To: "public", Expr: &text}, {From: "note", To: "private", Expr: &text}}},
		{Kind: "rebind", Types: []string{from.Type}, Batch: 2, Mapping: map[string]string{"old": "new"}, Fields: []MigrateField{{From: "link", To: "replacement"}}},
		{Kind: "retire", Types: []string{from.Type}, Batch: 2, Fields: []MigrateField{{From: "note", To: "note"}}},
	}}
	return plan, map[string]EntityInfo{from.Type: from}, map[string]EntityInfo{to.Type: to}
}

func TestMigrationConversionUsesTheOriginalRuleCompilerAndKeepsSources(t *testing.T) {
	plan, from, to := migrationFixture()
	program, err := CompileMigrationPlan(plan, from, to)
	if err != nil {
		t.Fatal(err)
	}
	original := json.RawMessage(`{"id":"A","name":"Asset","quantity":"12.50","note":"retained note","link":"old"}`)
	before := bytes.Clone(original)
	for i := range plan.Steps {
		out, err := program.Transform(i, "build.asset", original, ExprContext{})
		if err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
		if !bytes.Equal(before, original) || string(out.Values["name"]) != `"Asset"` || string(out.Values["note"]) != `"retained note"` {
			t.Fatal("conversion rewrote its original fields or bytes")
		}
		switch i {
		case 0:
			if out.Aliases["name"] != "title" || string(out.Values["title"]) != `"Asset"` {
				t.Fatal("rename did not retain its old read alias")
			}
		case 1:
			if string(out.Values["enabled"]) != "true" {
				t.Fatal("required default was not evaluated")
			}
		case 2:
			if !out.Changed || len(out.Values["amount"]) == 0 {
				t.Fatal("explicit decimal conversion was omitted")
			}
		case 3:
			if string(out.Values["public"]) != `"retained note"` || string(out.Values["private"]) != `"retained note"` || !reflect.DeepEqual(out.Retired, []string{"note", "note"}) {
				t.Fatal("split lost a result or its retired source")
			}
		case 4:
			if string(out.Values["replacement"]) != `"new"` {
				t.Fatal("identity mapping was not applied")
			}
		case 5:
			if !reflect.DeepEqual(out.Retired, []string{"note"}) {
				t.Fatal("retirement deleted rather than retained its field")
			}
		}
	}
	if _, err := program.Transform(2, "build.asset", json.RawMessage(`{"quantity":"not a number"}`), ExprContext{}); err == nil {
		t.Fatal("conversion failure silently coerced an invalid value")
	}
	if _, err := program.Transform(4, "build.asset", json.RawMessage(`{"link":"unmapped"}`), ExprContext{}); err == nil {
		t.Fatal("unmapped reference was silently dropped")
	}
}

func TestMigrationRejectsUncontrolledAndDestructivePlans(t *testing.T) {
	plan, from, to := migrationFixture()
	for _, kind := range []string{"delete", "script", "unknown"} {
		copy := plan
		copy.Steps = append([]MigrateStep{}, plan.Steps...)
		copy.Steps[0].Kind = kind
		if copy.Check() == nil {
			t.Fatalf("kind %s was accepted", kind)
		}
	}
	copy := plan
	copy.Steps = append([]MigrateStep{}, plan.Steps...)
	copy.Steps[2].OnFailure = ""
	if copy.Check() == nil {
		t.Fatal("conversion lacks an explicit failure strategy")
	}
	if err := json.Unmarshal([]byte(`{"from":"a","to":"b","scope":{"type":"x"},"steps":[],"script":"arbitrary code"}`), &copy); err == nil {
		t.Fatal("unknown plan property was accepted")
	}
	deleted := map[string]EntityInfo{}
	for name, info := range to {
		info.Fields = append([]FieldInfo{}, info.Fields[1:]...)
		deleted[name] = info
	}
	if _, err := CompileMigrationPlan(plan, from, deleted); err == nil || !strings.Contains(err.Error(), "physically deleted") {
		t.Fatalf("physical field removal passed the plan compiler: %v", err)
	}
	shape := to["build.asset"]
	shape.Fields = append([]FieldInfo{}, shape.Fields...)
	shape.Fields[0].ReadOnly = false
	to[shape.Type] = shape
	if _, err := CompileMigrationPlan(plan, from, to); err == nil {
		t.Fatal("rename left its old field writable")
	}
}

func TestMigrationDefaultPreservesAnExplicitFalseAndPredicateIsStrict(t *testing.T) {
	plan, from, to := migrationFixture()
	predicate := ExprLiteralValue("boolean", false)
	plan.Scope.Where = &predicate
	program, err := CompileMigrationPlan(plan, from, to)
	if err != nil {
		t.Fatal(err)
	}
	out, err := program.Transform(1, "build.asset", json.RawMessage(`{"enabled":false}`), ExprContext{})
	if err != nil || out.Changed || string(out.Values["enabled"]) != "false" {
		t.Fatal("unselected row or explicit false was replaced by a default")
	}
	plan.Scope.Where = nil
	program, err = CompileMigrationPlan(plan, from, to)
	if err != nil {
		t.Fatal(err)
	}
	out, err = program.Transform(1, "build.asset", json.RawMessage(`{"enabled":false}`), ExprContext{})
	if err != nil || out.Changed || string(out.Values["enabled"]) != "false" {
		t.Fatal("default treated an explicit false as missing")
	}
}
