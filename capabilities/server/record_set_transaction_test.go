package platformserver

import "platformserver/internal/construction"

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"platformserver/platform"
)

func recordSetRule(op, kind string) *platform.Expr {
	return &platform.Expr{Op: op, Kind: kind, Agg: &platform.ExprAggregate{Source: "lines"}}
}

func splitTxn() *platform.BusinessTxn {
	left := booleanRule("<=", platform.ExprReference("integer", "row.ordinal"), platform.ExprLiteralValue("integer", 2))
	right := booleanRule(">", platform.ExprReference("integer", "row.ordinal"), platform.ExprLiteralValue("integer", 2))
	return &platform.BusinessTxn{Subject: "build.batch", Steps: []platform.TxnStep{
		{Name: "left", Kind: "create", Target: platform.TxnRef{Type: "build.batch"}, Changes: []platform.TxnSet{{Field: "label", From: "=Left"}, {Field: "consumed", From: "=false"}}},
		{Name: "right", Kind: "create", Target: platform.TxnRef{Type: "build.batch"}, Changes: []platform.TxnSet{{Field: "label", From: "=Right"}, {Field: "consumed", From: "=false"}}},
		{Name: "split", Kind: "for-each", ForEach: &platform.TxnForEach{Set: "lines", Steps: []platform.TxnStep{
			{Name: "assignleft", Kind: "update", Target: platform.TxnRef{Type: "build.line", ID: "$item"}, When: left, Changes: []platform.TxnSet{{Field: "batch", From: "step.left"}}},
			{Name: "assignright", Kind: "update", Target: platform.TxnRef{Type: "build.line", ID: "$item"}, When: right, Changes: []platform.TxnSet{{Field: "batch", From: "step.right"}}},
		}}},
		{Name: "consume", Kind: "update", Target: platform.TxnRef{Type: "build.batch", ID: "$subject"}, Changes: []platform.TxnSet{{Field: "consumed", From: "=true"}}},
	}}
}

func setTxnSetup(t *testing.T, tn *Tenant) {
	t.Helper()
	access := []construction.Access{{Role: construction.User, Read: "all", Create: true, Edit: true, Archive: true}}
	fields := []construction.Field{{Name: "label", Title: "Label", Type: "text"}, {Name: "consumed", Title: "Consumed", Type: "boolean"}}
	publishObject(t, tn, "builder", "BATCH", map[string]any{"name": "batch", "title": "Batch", "fields": fields, "access": access, "states": []construction.State{{Name: "draft", Title: "Draft"}}}, txnAt)
	publishObject(t, tn, "builder", "LINE", map[string]any{"name": "line", "title": "Line", "fields": []construction.Field{{Name: "batch", Title: "Batch", Type: "reference", Ref: "build.batch", Inverse: "lines", Required: true}, {Name: "ordinal", Title: "Ordinal", Type: "integer"}, {Name: "done", Title: "Done", Type: "boolean"}}, "access": access}, txnAt)
	reads := &platform.RuleReadSpec{Object: "build.batch", Collections: map[string]platform.RuleCollection{"lines": {App: construction.ID, Set: "lines", Object: "build.line", Policy: "complete"}}}
	all := recordSetRule("all", "boolean")
	predicate := platform.ExprReference("boolean", "item.done")
	all.In = &predicate
	fields = append(fields, construction.Field{Name: "total", Title: "Total", Type: "integer", FormulaExpr: recordSetRule("count", "integer")}, construction.Field{Name: "complete", Title: "Complete", Type: "boolean", FormulaExpr: all})
	constraint := platform.Constraint{Name: "complete", Kind: "expression", Level: "txn", Fields: []string{"consumed"}, Reads: reads, Message: "Every line must be complete.", Translations: map[string]string{"zh-CN": "每行都必须完成。"}, Expr: booleanRule("or", *booleanRule("not", platform.ExprReference("boolean", "consumed")), *all)}
	split := splitTxn()
	split.Reads, split.Guard, split.Message = reads, all, "Every line must be complete."
	seal := &platform.BusinessTxn{Subject: "build.batch", Reads: reads, Guard: all, Message: "Every line must be complete.", Steps: []platform.TxnStep{{Name: "seal", Kind: "update", Target: platform.TxnRef{Type: "build.batch", ID: "$subject"}, Changes: []platform.TxnSet{{Field: "consumed", From: "=true"}}}}}
	decide(t, tn, "builder", construction.ID, "build.object.edit", construction.ObjectType, "BATCH", map[string]any{
		"sets":  []platform.RecordSet{{Name: "lines", From: "lines", Direction: "inverse", Object: "build.line", Order: []platform.RecordSetSort{{Expr: platform.ExprReference("integer", "ordinal")}}, Limit: platform.RecordSetMaxRows}},
		"reads": reads, "fields": fields, "constraints": []platform.Constraint{constraint},
		"actions": []construction.Action{{Name: "split", Title: "Split", From: []string{"draft"}, Roles: []string{construction.User}, Transaction: split}, {Name: "seal", Title: "Seal", From: []string{"draft"}, Roles: []string{construction.User}, Transaction: seal}},
	}, txnAt)
	decide(t, tn, "builder", construction.ID, "build.object.publish", construction.ObjectType, "BATCH", nil, txnAt)
	decide(t, tn, "user", construction.ID, "build.batch.create", "build.batch", "root", map[string]any{"label": "Input", "consumed": false}, txnAt)
	for _, ordinal := range []int{4, 1, 3, 2} {
		id := string(rune('A' + ordinal - 1))
		decide(t, tn, "user", construction.ID, "build.line.create", "build.line", id, map[string]any{"batch": "root", "ordinal": ordinal, "done": true}, txnAt)
	}
}

func TestRecordSetTransactionSplitConstraintsAndRecovery(t *testing.T) {
	tn := builderTenant(t, "set-transaction")
	var entries []Entry
	tn.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) {
		entries = append(entries, entry)
		return entry.Body, nil
	}
	setTxnSetup(t, tn)
	caller := tn.caller(memberOf(t, tn, "user"), tn.app(construction.ID), false)
	initial, readErr := caller.ReadRecordSet("build.batch", "root", "lines", txnAt)
	if readErr != nil || len(initial.Records) != 4 {
		t.Fatalf("initial inverse: %v", readErr)
	}
	decide(t, tn, "user", construction.ID, "build.batch.create", "build.batch", "other", map[string]any{"label": "Other", "consumed": false}, txnAt)
	decide(t, tn, "user", construction.ID, "build.line.edit", "build.line", "A", map[string]any{"batch": "other"}, txnAt)
	old, oldErr := caller.ReadRecordSet("build.batch", "root", "lines", txnAt)
	next, nextErr := caller.ReadRecordSet("build.batch", "other", "lines", txnAt)
	if oldErr != nil || nextErr != nil || len(old.Records) != 3 || len(next.Records) != 1 {
		t.Fatal("reference owner index did not update both inverse views")
	}
	decide(t, tn, "user", construction.ID, "build.line.edit", "build.line", "A", map[string]any{"batch": "root"}, txnAt)
	decide(t, tn, "user", construction.ID, "build.batch.seal", "build.batch", "root", nil, txnAt)
	if got := refuse(t, tn, "user", construction.ID, "build.line.edit", "build.line", "C", map[string]any{"done": false}, txnAt); !strings.Contains(got, "Constraint complete") {
		t.Fatalf("child write bypassed parent set constraint: %s", got)
	}
	root, err := tn.RecordOf(memberOf(t, tn, "user"), "build.batch", "root", txnAt)
	var values map[string]any
	_ = json.Unmarshal(platform.Raw(root.Record), &values)
	if err != nil || values["total"] != float64(4) || values["complete"] != true {
		t.Fatalf("rule consumers differ: %v %v", values, err)
	}
	receipt := decide(t, tn, "user", construction.ID, "build.batch.split", "build.batch", "root", nil, txnAt)
	result, _, failure := decodeAcceptedBatch(entries[len(entries)-1].Body)
	if failure != nil || result.Version != 5 || len(result.Iterations) != 1 || !reflect.DeepEqual(result.Iterations[0].IDs, []string{"A", "B", "C", "D"}) {
		t.Fatalf("ordered witness: %+v %v", result.Iterations, failure)
	}
	for _, side := range []struct {
		step string
		ids  []string
	}{{"left", []string{"A", "B"}}, {"right", []string{"C", "D"}}} {
		caller := tn.caller(memberOf(t, tn, "user"), tn.app(construction.ID), false)
		set, err := caller.ReadRecordSet("build.batch", receipt.GetChangeId()+"-"+side.step, "lines", txnAt)
		if err != nil || len(set.Records) != 2 {
			t.Fatalf("split %s: %d %v", side.step, len(set.Records), err)
		}
		for i, raw := range set.Records {
			var identity struct{ ID string }
			_ = json.Unmarshal(raw, &identity)
			if identity.ID != side.ids[i] {
				t.Fatalf("split order differs: %s", identity.ID)
			}
		}
	}
	restored := builderTenant(t, "set-transaction")
	if err := restored.Replay(entries); err != nil {
		t.Fatal(err)
	}
	if snapshot(tn) != snapshot(restored) {
		t.Fatal("recovery re-evaluated or changed the accepted split")
	}
}
