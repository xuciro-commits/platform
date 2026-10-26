package platformserver

import "platformserver/internal/construction"

import (
	"encoding/json"
	resultformat "platformserver/internal/acceptance/result"
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
	result, _, failure := resultformat.DecodeBatch(entries[len(entries)-1].Body)
	if failure != nil || result.Version != 9 || len(result.Iterations) != 1 || result.Iterations[0].Decision != "build/"+receipt.GetChangeId() || !reflect.DeepEqual(result.Iterations[0].IDs, []string{"A", "B", "C", "D"}) {
		t.Fatalf("ordered witness: %+v %v", result.Iterations, failure)
	}
	// Historical version 8 witnesses keep their original interpretation. This
	// constructs old bytes only in the fixture; production history is not rewritten.
	copyResult := func() resultformat.Batch {
		var copied resultformat.Batch
		if err := json.Unmarshal(platform.Raw(result), &copied); err != nil {
			t.Fatal(err)
		}
		return copied
	}
	encode := func(value resultformat.Batch) []byte {
		value.Digest, failure = resultformat.DigestBatch(value)
		if failure != nil {
			t.Fatal(failure)
		}
		return platform.Raw(value)
	}
	legacy := copyResult()
	legacy.Version, legacy.Iterations[0].Decision = 8, ""
	if _, _, err := resultformat.DecodeBatch(encode(legacy)); err != nil {
		t.Fatalf("version 8 witness is no longer readable: %v", err)
	}
	for _, identity := range []string{"", "build/missing", "flow/" + receipt.GetChangeId()} {
		forged := copyResult()
		forged.Iterations[0].Decision = identity
		if _, _, err := resultformat.DecodeBatch(encode(forged)); err == nil {
			t.Fatalf("forged iteration decision %q was accepted", identity)
		}
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

func TestTransactionIteratesReferencedRecordSetWithOriginalPermissions(t *testing.T) {
	tn := builderTenant(t, "referenced-set-transaction")
	var entries []Entry
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	setTxnSetup(t, tn)
	access := []construction.Access{{Role: construction.User, Read: "all", Create: true, Edit: true}}
	publishObject(t, tn, "builder", "COPY", map[string]any{"name": "setcopy", "title": "Copied line", "fields": []construction.Field{{Name: "source", Title: "Original line", Type: "reference", Ref: "build.line"}, {Name: "ordinal", Title: "Ordinal", Type: "integer"}}, "access": access}, txnAt)
	copyTxn := &platform.BusinessTxn{Subject: "build.setconsumer", Steps: []platform.TxnStep{{Name: "copy", Kind: "for-each", ForEach: &platform.TxnForEach{Of: "source", Set: "lines", Steps: []platform.TxnStep{{Name: "line", Kind: "create", Target: platform.TxnRef{Type: "build.setcopy"}, Changes: []platform.TxnSet{{Field: "source", From: "row.$subject"}, {Field: "ordinal", From: "row.ordinal"}}}}}}}}
	fields := []construction.Field{{Name: "source", Title: "Source batch", Type: "reference", Ref: "build.batch"}}
	publishObject(t, tn, "builder", "CONSUMER", map[string]any{"name": "setconsumer", "title": "Consumer", "fields": fields, "access": access, "states": []construction.State{{Name: "draft", Title: "Draft"}}, "actions": []construction.Action{{Name: "freeze", Title: "Freeze referenced lines", From: []string{"draft"}, Roles: []string{construction.User}, Transaction: copyTxn}}}, txnAt)
	for _, row := range []struct{ id, source string }{{"missing", ""}, {"consumer", "root"}} {
		decide(t, tn, "user", "build", "build.setconsumer.create", "build.setconsumer", row.id, map[string]any{"source": row.source}, txnAt)
	}
	before := txnState(tn)
	if got := refuse(t, tn, "user", "build", "build.setconsumer.freeze", "build.setconsumer", "missing", nil, txnAt); got == "ok" || txnState(tn) != before {
		t.Fatal("a missing collection owner was accepted or left partial copied lines")
	}
	// Withholding the declared reference itself must not be bypassed by an internal ID lookup.
	fields[0].Read = []string{"builder"}
	decide(t, tn, "builder", "build", "build.object.edit", construction.ObjectType, "CONSUMER", map[string]any{"fields": fields}, txnAt)
	decide(t, tn, "builder", "build", "build.object.publish", construction.ObjectType, "CONSUMER", nil, txnAt)
	before = txnState(tn)
	if got := refuse(t, tn, "user", "build", "build.setconsumer.freeze", "build.setconsumer", "consumer", nil, txnAt); got == "ok" || txnState(tn) != before {
		t.Fatal("a withheld collection owner was used to copy its lines")
	}
	fields[0].Read = nil
	decide(t, tn, "builder", "build", "build.object.edit", construction.ObjectType, "CONSUMER", map[string]any{"fields": fields}, txnAt)
	decide(t, tn, "builder", "build", "build.object.publish", construction.ObjectType, "CONSUMER", nil, txnAt)
	receipt := decide(t, tn, "user", "build", "build.setconsumer.freeze", "build.setconsumer", "consumer", nil, txnAt)
	batch, _, failure := resultformat.DecodeBatch(entries[len(entries)-1].Body)
	if failure != nil || len(batch.Iterations) != 1 || batch.Iterations[0].Decision != "build/"+receipt.GetChangeId() || !reflect.DeepEqual(batch.Iterations[0].IDs, []string{"A", "B", "C", "D"}) {
		t.Fatalf("referenced-set witness: %+v %v", batch.Iterations, failure)
	}
	copies, err := tn.Records(memberOf(t, tn, "user"), "build.setcopy", platform.Query{Limit: 10}, txnAt)
	if err != nil || copies.Total != 4 {
		t.Fatalf("original referenced lines were not all copied: %+v %v", copies, err)
	}
	restored := builderTenant(t, tn.ID)
	if e := restored.Replay(entries); e != nil || snapshot(tn) != snapshot(restored) {
		t.Fatalf("referenced-set recovery changed the accepted rows: %v", e)
	}
}
