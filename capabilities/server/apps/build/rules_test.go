package build

import "platformserver/internal/construction"

import (
	"encoding/json"
	"platformserver/platform"
	"testing"
)

func TestRuleSugarMigrationPreservesFrozenBytes(t *testing.T) {
	raw := `{"name":"job","title":"Job","fields":[{"name":"qty","title":"Qty","type":"integer"},{"name":"total","title":"Total","type":"decimal","formula":"qty * 2"}],"states":[{"name":"ready","title":"Ready"}],"actions":[{"name":"go","title":"Go","from":["ready"],"conditions":[{"field":"qty","operator":">","value":"0","message":"Positive quantity required"}]}]}`
	var o construction.Object
	if err := json.Unmarshal([]byte(raw), &o); err != nil {
		t.Fatal(err)
	}
	o.Published = raw
	b := New("rules")
	if err := b.normalizeObjectRules(&o); err != nil {
		t.Fatal(err)
	}
	if o.Published != raw || o.Fields[1].FormulaExpr == nil || o.Fields[1].Formula != "" || len(o.Actions[0].Conditions) > 0 || o.Actions[0].Transaction == nil || o.Actions[0].Transaction.Guard == nil {
		t.Fatal("migration rewrote frozen bytes or retained old writer")
	}
	var table construction.Table
	tableRaw := `{"name":"table","title":"Table","inputs":[{"name":"qty","type":"number"}],"outputs":[{"name":"ok","type":"boolean"}],"rows":[{"when":["1..5"],"then":["true"]}]}`
	json.Unmarshal([]byte(tableRaw), &table)
	table.Published = tableRaw
	if err := table.NormalizeRules(); err != nil {
		t.Fatal(err)
	}
	if table.Published != tableRaw || len(table.Rows[0].When) > 0 || table.Rows[0].Expressions[0].Op != "range" {
		t.Fatal("table migration changed publication")
	}
	d := platform.PageDocument{UIProfile: platform.PageUIProfile(), Variables: map[string]platform.PageVariable{"text": {Scope: "page", Type: "string", Mode: "state", Initial: platform.Raw("2")}, "value": {Scope: "page", Type: "decimal", Mode: "derived", Expression: &platform.PageExpression{Op: "parse-decimal", Args: []platform.PageValue{{Variable: "text"}}}}}}
	before, _ := json.Marshal(d)
	if err := d.NormalizePageRules(); err != nil {
		t.Fatal(err)
	}
	if d.Variables["value"].Expression != nil || d.Variables["value"].Rule == nil {
		t.Fatal("page graph retained old writer")
	}
	var historic platform.PageDocument
	if err := json.Unmarshal(before, &historic); err != nil || historic.Variables["value"].Expression == nil {
		t.Fatal("historical graph is no longer readable")
	}
}

func TestRulePublicationRejectsEveryBranch(t *testing.T) {
	b := New("rules")
	base := construction.Object{Name: "job", Title: "Job", Fields: []construction.Field{{Name: "ok", Title: "OK", Type: "boolean"}}, States: []construction.State{{Name: "ready", Title: "Ready"}}}
	for _, e := range []platform.Expr{{Op: "unknown", Kind: "boolean"}, {Op: "=", Kind: "boolean", Args: []platform.Expr{platform.ExprReference("boolean", "ok"), platform.ExprLiteralValue("text", "true")}}, {Op: "count", Kind: "integer", Agg: &platform.ExprAggregate{Source: "undeclared"}}, {Op: "and", Kind: "boolean", Args: []platform.Expr{platform.ExprLiteralValue("boolean", false), {Op: "unknown", Kind: "boolean"}}}} {
		o := base
		o.Actions = []construction.Action{{Name: "go", Title: "Go", From: []string{"ready"}, Conditions: []construction.Condition{{Expr: &e, Message: "Rule rejected"}}}}
		if err := b.checkObjectRules(o); err == nil {
			t.Fatalf("invalid published rule accepted: %+v", e)
		}
	}
}
