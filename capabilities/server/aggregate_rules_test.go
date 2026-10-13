package platformserver

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"platformserver/apps/build"
	"platformserver/internal/rules"
	"platformserver/platform"
)

func TestAggregateUsesTheExactRuleFold(t *testing.T) {
	owner := newConstraintOwner("aggregate-rules")
	tn := composeTenant(t, "aggregate-rules", []Seat{seatOf("writer", "invariants:operator")}, owner)
	for i, qty := range []int64{9007199254740993, 2} {
		body := validConstraintEntry(string(rune('A' + i)))
		body["qty"] = qty
		decide(t, tn, "writer", "invariants", "invariants.entry.create", owner.entity.Type, string(rune('A'+i)), body, txnAt)
	}
	got, err := tn.Aggregate(memberOf(t, tn, "writer"), owner.entity.Type, AggregateQuery{Measures: []string{"count", "sum:qty", "min:qty", "max:qty", "avg:qty"}}, txnAt)
	if err != nil {
		t.Fatal(err)
	}
	if string(platform.Raw(got.Rows[0]["sum:qty"])) != "9007199254740995" || string(platform.Raw(got.Rows[0]["max:qty"])) != "9007199254740993" {
		t.Fatalf("HTTP aggregate lost precision: %v", got.Rows)
	}
	collection := platform.ExprCollection{Complete: true, Items: []platform.ExprItem{{ID: "A", Fields: map[string]platform.ExprValue{"qty": platform.ExprPresent("integer", int64(9007199254740993))}}, {ID: "B", Fields: map[string]platform.ExprValue{"qty": platform.ExprPresent("integer", 2)}}}}
	schema := platform.ExprSchema{Sets: map[string]platform.ExprSetType{"rows": {Policy: "complete", Element: platform.ExprType{Kind: "reference", Fields: map[string]platform.ExprType{"qty": {Kind: "integer"}}}}}}
	for _, op := range []string{"sum", "min", "max", "avg"} {
		kind := "integer"
		if op == "avg" {
			kind = "decimal"
		}
		expr := platform.Expr{Op: op, Kind: kind, Agg: &platform.ExprAggregate{Source: "rows", Field: []string{"qty"}}}
		program, err := platform.CompileExpr(expr, schema)
		if err != nil {
			t.Fatal(err)
		}
		result, err := program.Eval(platform.ExprContext{Sets: map[string]platform.ExprCollection{"rows": collection}})
		if err != nil {
			t.Fatal(err)
		}
		value, err := platform.ExprGoValue(result.Value)
		if err != nil || string(platform.Raw(value)) != string(platform.Raw(got.Rows[0][op+":qty"])) {
			t.Fatalf("HTTP and rule %s differ", op)
		}
	}
}

func TestDeclaredAggregateAndTenThousandRuleMembers(t *testing.T) {
	owner := newRecordSetOwner("record-sets")
	owner.link.Cardinality = "many-to-many"
	owner.entity.Sets[0].Limit = platform.RecordSetMaxRows
	tn := recordSetTenant(t, owner)
	for _, id := range []string{"root", "child"} {
		decide(t, tn, "writer", "invariants", "invariants.entry.create", owner.entity.Type, id, validConstraintEntry(id), txnAt)
	}
	decide(t, tn, "writer", "invariants", "invariants.entry.attach", owner.entity.Type, "root", map[string]any{"child": "child"}, txnAt)
	out, err := tn.Aggregate(memberOf(t, tn, "writer"), owner.entity.Type, AggregateQuery{Collection: &platform.RecordSetRef{Object: owner.entity.Type, ID: "root", Name: "children"}, Measures: []string{"count", "sum:qty"}}, txnAt)
	if err != nil || len(out.Rows) != 1 || out.Rows[0]["count"] != 1 || string(platform.Raw(out.Rows[0]["sum:qty"])) != "2" {
		t.Fatalf("declared aggregate: %v %v", out, err)
	}
	items := make([]platform.ExprItem, platform.RecordSetMaxRows)
	for i := range items {
		items[i] = platform.ExprItem{ID: fmt.Sprintf("row-%05d", i), Fields: map[string]platform.ExprValue{"qty": platform.ExprPresent("integer", 1)}}
	}
	expr := platform.Expr{Op: "sum", Kind: "integer", Agg: &platform.ExprAggregate{Source: "rows", Field: []string{"qty"}}}
	schema := platform.ExprSchema{Sets: map[string]platform.ExprSetType{"rows": {Policy: "complete", Element: platform.ExprType{Kind: "reference", Fields: map[string]platform.ExprType{"qty": {Kind: "integer"}}}}}}
	context := platform.ExprContext{Sets: map[string]platform.ExprCollection{"rows": {Complete: true, Items: items}}}
	raw := platform.Raw(map[string]any{"expr": expr, "schema": schema, "context": context})
	result, evalErr := rules.EvalJSON(raw)
	var answer platform.ExprResult
	_ = json.Unmarshal(result, &answer)
	if evalErr != nil || string(answer.Value.Data) != `"10000"` {
		t.Fatalf("ten thousand members did not reach the shared boundary: %s %v", answer.Value.Data, evalErr)
	}
	context.Sets["rows"] = platform.ExprCollection{Complete: true, Items: append(items, platform.ExprItem{ID: "overflow"})}
	program, _ := platform.CompileExpr(expr, schema)
	if _, err := program.Eval(context); err == nil || !strings.Contains(err.Error(), "10001") || !strings.Contains(err.Error(), "10000") {
		t.Fatalf("member limit was silently truncated: %v", err)
	}
}

func TestRequiredOwnedRelationChecksTheFinalTransaction(t *testing.T) {
	tn := builderTenant(t, "required-links")
	parent := publishObject(t, tn, "builder", "PARENT", map[string]any{"name": "parent", "title": "Parent", "fields": []build.Field{{Name: "name", Title: "Name", Type: "text"}}, "states": []build.State{{Name: "draft", Title: "Draft"}}}, txnAt)
	child := publishObject(t, tn, "builder", "CHILD", map[string]any{"name": "child", "title": "Child", "fields": []build.Field{{Name: "name", Title: "Name", Type: "text"}}}, txnAt)
	decide(t, tn, "builder", build.ID, "build.linktype.create", build.LinkTypeType, "LINK", map[string]any{"name": "children", "title": "Children", "description": "Required owned children", "storage": "edge", "parent": parent, "child": child, "forward": "children", "reverse": "parent", "required": true, "cardinality": "one-to-one"}, txnAt)
	decide(t, tn, "builder", build.ID, "build.linktype.publish", build.LinkTypeType, "LINK", nil, txnAt)
	if got := refuse(t, tn, "builder", build.ID, child+".create", child, "orphan", map[string]any{"name": "Orphan"}, txnAt); !strings.Contains(got, "relation.children") {
		t.Fatalf("required relationship accepted an orphan: %s", got)
	}
	binding := platform.AssetBinding{Ref: platform.AssetRef{App: build.ID, Kind: platform.AssetLinkType, Name: "children"}, SourceVersion: "1.link-1"}
	txn := &platform.BusinessTxn{Subject: parent, Steps: []platform.TxnStep{
		{Name: "child", Kind: "create", Target: platform.TxnRef{Type: child}, Changes: []platform.TxnSet{{Field: "name", From: "=Child"}}},
		{Name: "link", Kind: "link", Link: &platform.TxnLink{Binding: binding, Parent: platform.TxnRef{Type: parent, ID: "$subject"}, Child: platform.TxnRef{Type: child, ID: "step.child"}}},
		{Name: "parent", Kind: "update", Target: platform.TxnRef{Type: parent, ID: "$subject"}},
	}}
	decide(t, tn, "builder", build.ID, "build.object.edit", build.ObjectType, "PARENT", map[string]any{"actions": []build.Action{{Name: "attach", Title: "Attach", From: []string{"draft"}, Roles: []string{build.Builder}, Transaction: txn}}}, txnAt)
	decide(t, tn, "builder", build.ID, "build.object.publish", build.ObjectType, "PARENT", nil, txnAt)
	decide(t, tn, "builder", build.ID, parent+".create", parent, "parent", map[string]any{"name": "Parent"}, txnAt)
	first := decide(t, tn, "builder", build.ID, parent+".attach", parent, "parent", nil, txnAt)
	if _, err := tn.RecordOf(memberOf(t, tn, "builder"), child, first.GetChangeId()+"-child", txnAt); err != nil {
		t.Fatal(err)
	}
	if got := refuse(t, tn, "builder", build.ID, parent+".attach", parent, "parent", nil, txnAt); !strings.Contains(got, "relation.children") {
		t.Fatalf("second child did not refuse atomically: %s", got)
	}
	// Refusal audit changes the ledger, but may never add a child/edge row.
	page, err := tn.Records(memberOf(t, tn, "builder"), child, platform.Query{}, txnAt)
	if err != nil || page.Total != 1 {
		t.Fatalf("refused required transaction left rows: %+v %v", page, err)
	}
	remove := &platform.BusinessTxn{Subject: parent, Steps: []platform.TxnStep{
		{Name: "unlink", Kind: "unlink", Link: &platform.TxnLink{Binding: binding, Parent: platform.TxnRef{Type: parent, ID: "$subject"}, Child: platform.TxnRef{Type: child, ID: first.GetChangeId() + "-child"}}},
		{Name: "parent", Kind: "update", Target: platform.TxnRef{Type: parent, ID: "$subject"}},
	}}
	decide(t, tn, "builder", build.ID, "build.object.edit", build.ObjectType, "PARENT", map[string]any{"actions": []build.Action{{Name: "attach", Title: "Attach", From: []string{"draft"}, Roles: []string{build.Builder}, Transaction: txn}, {Name: "remove", Title: "Remove", From: []string{"draft"}, Roles: []string{build.Builder}, Transaction: remove}}}, txnAt)
	decide(t, tn, "builder", build.ID, "build.object.publish", build.ObjectType, "PARENT", nil, txnAt)
	if got := refuse(t, tn, "builder", build.ID, parent+".remove", parent, "parent", nil, txnAt); !strings.Contains(got, "relation.children") {
		t.Fatalf("unlink bypassed required parent: %s", got)
	}
}
