package platform

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestConstraintPublicationBoundaries(t *testing.T) {
	info := EntityInfo{Type: "app.entry", App: "app", Fields: []FieldInfo{{Name: "qty", Type: "integer"}, {Name: "parent", Type: "reference", Ref: "app.entry"}}, Lifecycle: &LifecycleInfo{Field: "state", Initial: "draft"}}
	lookup := func(typ string) (EntityInfo, bool) { return info, typ == info.Type }
	valid := Constraint{Name: "positive", Kind: "expression", Fields: []string{"qty"}, Message: "Quantity must be positive.", Expr: &Expr{Op: ">", Kind: "boolean", Args: []Expr{ExprReference("integer", "qty"), ExprLiteralValue("integer", 0)}}}
	info.Constraints = []Constraint{valid}
	if err := CheckConstraints(info, lookup, true); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"unknown kind", "unknown field", "unknown operator", "long scope", "unknown boundary", "unscoped set", "cross owner", "wrong type"} {
		t.Run(name, func(t *testing.T) {
			raw, _ := json.Marshal(valid)
			var invalid Constraint
			_ = json.Unmarshal(raw, &invalid)
			boundary := true
			switch name {
			case "unknown kind":
				invalid.Kind = "hook"
			case "unknown field":
				invalid.Fields = []string{"missing"}
			case "unknown operator":
				invalid.Expr.Op = "execute"
			case "long scope":
				invalid.Kind = "unique"
				invalid.Expr = nil
				invalid.Scope = strings.Repeat("parent.", 16) + "qty"
			case "unknown boundary":
				invalid.Level = "txn"
				boundary = false
			case "unscoped set", "cross owner":
				app := "app"
				if name == "cross owner" {
					app = "other"
				}
				invalid.Reads = &RuleReadSpec{Collections: map[string]RuleCollection{"items": {App: app, Query: "items", Object: info.Type, Policy: "visible"}}}
			case "wrong type":
				invalid.Expr.Args[1] = ExprLiteralValue("text", "0")
			}
			current := info
			current.Constraints = []Constraint{invalid}
			if err := CheckConstraints(current, lookup, boundary); err == nil {
				t.Fatal("invalid constraint reached runtime")
			}
		})
	}
	for _, raw := range []string{`{"name":"x","kind":"required","fields":["qty"],"message":"Required","skip":true}`, `{"name":"x","kind":"expression","fields":["qty"],"message":"Check","expr":{"op":"execute","kind":"boolean","code":"x"}}`} {
		var constraint Constraint
		if err := json.Unmarshal([]byte(raw), &constraint); err == nil {
			t.Fatal("unknown executable or bypass field ignored")
		}
	}
}
