package platformserver

import (
	"testing"

	"platformserver/platform"
	"platformserver/studio/build"
)

func TestConditionalImmutabilityUsesOriginalParentAndCurrentReadPermission(t *testing.T) {
	tn := builderTenant(t, "conditional-freeze")
	publishObject(t, tn, "builder", "PARENT", map[string]any{"name": "freezedocument", "title": "Document", "fields": []build.Field{{Name: "frozen", Title: "Frozen", Type: "boolean", Read: []string{"builder"}}}, "access": []build.Access{{Role: "user", Read: "all"}}}, txnAt)
	parent := platform.ExprReference("reference", "document")
	condition := platform.Expr{Op: "path", Kind: "boolean", Args: []platform.Expr{parent}, Path: []string{"frozen"}}
	publishObject(t, tn, "builder", "LINE", map[string]any{"name": "freezeline", "title": "Line", "fields": []build.Field{{Name: "document", Title: "Document", Type: "reference", Ref: "build.freezedocument", Required: true}, {Name: "amount", Title: "Amount", Type: "decimal"}}, "access": []build.Access{{Role: "user", Read: "all", Create: true, Edit: true, Archive: true}}, "constraints": []platform.Constraint{{Name: "originalfreeze", Kind: "immutable", Fields: []string{"document", "amount", "archived"}, Expr: &condition, Message: "The original document freezes this line."}}}, txnAt)
	for _, id := range []string{"one", "two"} {
		decide(t, tn, "builder", "build", "build.freezedocument.create", "build.freezedocument", id, map[string]any{"frozen": false}, txnAt)
	}
	decide(t, tn, "user", "build", "build.freezeline.create", "build.freezeline", "line", map[string]any{"document": "one", "amount": 1}, txnAt)
	before := txnState(tn)
	if got := refuse(t, tn, "user", "build", "build.freezeline.edit", "build.freezeline", "line", map[string]any{"amount": 2}, txnAt); got == "ok" || txnState(tn) != before {
		t.Fatal("an unreadable condition was interpreted as permission to change a line")
	}
	decide(t, tn, "builder", "build", "build.freezeline.edit", "build.freezeline", "line", map[string]any{"amount": 2}, txnAt)
	decide(t, tn, "builder", "build", "build.freezedocument.edit", "build.freezedocument", "one", map[string]any{"frozen": true}, txnAt)
	before = txnState(tn)
	if got := refuse(t, tn, "builder", "build", "build.freezeline.edit", "build.freezeline", "line", map[string]any{"document": "two", "amount": 3}, txnAt); got == "ok" || txnState(tn) != before {
		t.Fatal("repointing the parent bypassed its original freeze condition")
	}
	if got := refuse(t, tn, "builder", "build", "build.freezeline.archive", "build.freezeline", "line", nil, txnAt); got == "ok" || txnState(tn) != before {
		t.Fatal("archiving removed a line whose original document still freezes it")
	}
}

func TestRecursiveCreationRefusesWithoutKeepingAnyChild(t *testing.T) {
	tn := builderTenant(t, "bounded-creation")
	publishObject(t, tn, "builder", "NODE", map[string]any{"name": "creationnode", "title": "Creation node"}, txnAt)
	decide(t, tn, "builder", "build", "build.object.edit", build.ObjectType, "NODE", map[string]any{"fields": []build.Field{{Name: "parent", Title: "Parent", Type: "reference", Ref: "build.creationnode"}}, "createTransaction": &platform.BusinessTxn{Subject: "build.creationnode", Steps: []platform.TxnStep{{Name: "child", Kind: "create", Target: platform.TxnRef{Type: "build.creationnode"}, Changes: []platform.TxnSet{{Field: "parent", From: "$subject"}}}}}}, txnAt)
	decide(t, tn, "builder", "build", "build.object.publish", build.ObjectType, "NODE", nil, txnAt)
	before := txnState(tn)
	if got := refuse(t, tn, "user", "build", "build.creationnode.create", "build.creationnode", "root", nil, txnAt); got == "ok" || txnState(tn) != before {
		t.Fatal("recursive creation exceeded its depth bound or retained partial children")
	}
}

func TestRequiredBusinessScalarsAcceptZeroAndFalseButRejectAbsentInputs(t *testing.T) {
	tn := builderTenant(t, "required-business-scalars")
	publishObject(t, tn, "builder", "SCALARS", map[string]any{"name": "providedscalar", "title": "Provided scalar", "fields": []build.Field{{Name: "quantity", Title: "Quantity", Type: "decimal", Required: true}, {Name: "final", Title: "Final", Type: "boolean", Required: true}}}, txnAt)
	decide(t, tn, "user", "build", "build.providedscalar.create", "build.providedscalar", "present", map[string]any{"quantity": 0, "final": false}, txnAt)
	caller := tn.caller(memberOf(t, tn, "user"), tn.app("build"), false)
	for name, want := range map[string]string{"quantity": "0", "final": "false"} {
		value, _, failure := caller.ReadRecordPath("build.providedscalar", "present", []string{name}, txnAt)
		if failure != nil || string(value) != want {
			t.Fatalf("initialized %s became missing: %s %v", name, value, failure)
		}
	}
	for _, values := range []map[string]any{{"quantity": 0}, {"final": false}, {"quantity": nil, "final": false}} {
		before := txnState(tn)
		if got := refuse(t, tn, "user", "build", "build.providedscalar.create", "build.providedscalar", "absent", values, txnAt); got == "ok" || txnState(tn) != before {
			t.Fatalf("missing or null required input was accepted: %+v", values)
		}
	}
}
