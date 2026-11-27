package main

import (
	"encoding/json"
	"platformserver/platform"
	"strings"
	"testing"
)

func TestAggregateReferencePath(t *testing.T) {
	scope := exprScope{
		objects: map[string]map[string]exprField{
			"line":     {"material": {kind: "reference", ref: "material"}},
			"material": {"state": {kind: "choice"}, "quantity": {kind: "integer"}},
		},
		sets: map[string]string{"lines": "line"},
	}
	for _, tc := range []struct {
		source string
		path   []string
	}{
		{"sum(lines, if(item.material.state = 'active', 0, 1))", []string{"state"}},
		{"sum(lines, item.material.quantity)", []string{"quantity"}},
	} {
		v, err := compileExpr(tc.source, scope, "integer")
		if err != nil {
			t.Fatal(err)
		}
		raw := []byte(stringify(v))
		var e platform.Expr
		if err := json.Unmarshal(raw, &e); err != nil {
			t.Fatal(err)
		}
		if e.In == nil {
			t.Fatalf("missing aggregate expression: %s", raw)
		}
		p := e.In
		if p.Op == "if" {
			p = &p.Args[0].Args[0]
		}
		if p.Op != "path" || len(p.Args) != 1 || p.Args[0].Ref != "item.material" || strings.Join(p.Path, ".") != strings.Join(tc.path, ".") {
			t.Fatalf("unexpected native path: %s", raw)
		}
	}
	if _, err := compileExpr("item.material.state", scope, "choice"); err == nil || !strings.Contains(err.Error(), "outside an aggregate") {
		t.Fatalf("outside aggregate: %v", err)
	}
	if _, err := compileExpr("sum(lines, item.material.state.invalid)", scope, "integer"); err == nil {
		t.Fatal("accepted traversal through a non-reference")
	}
}
