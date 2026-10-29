package platformserver

import (
	"platformserver/apps/core"
	"platformserver/platform"
	"platformserver/studio/build"
	"testing"
)

func TestRuleQueriesUseReadableSubjectKeysAndNativeOwnerOutcomes(t *testing.T) {
	tn := builderTenant(t, "subject-outcome-query", core.New("subject-outcome-query"))
	decide(t, tn, "builder", "build", build.QueryType+".create", build.QueryType, "OUTCOMES", map[string]any{"name": "ownoutcomes", "title": "Own coordination outcomes", "description": "Read visible outcomes for this exact subject.", "object": "build.confirmation", "by": "subject", "sort": []string{"id"}, "limit": 200}, txnAt)
	decide(t, tn, "builder", "build", build.QueryType+".publish", build.QueryType, "OUTCOMES", nil, txnAt)
	key := platform.Expr{Op: "concat", Kind: "text", Args: []platform.Expr{platform.ExprLiteralValue("text", "build.outcomeconsumer/"), platform.ExprReference("text", "id")}}
	source := platform.RuleReadSpec{Collections: map[string]platform.RuleCollection{"outcomes": {App: "build", Object: "build.confirmation", Query: "ownoutcomes", Version: 1, Of: "$subject.coordination", Policy: "visible"}}}
	count := platform.Expr{Op: "count", Kind: "integer", Agg: &platform.ExprAggregate{Source: "outcomes"}}
	guard := booleanRule("=", count, platform.ExprLiteralValue("integer", 0))
	transaction := &platform.BusinessTxn{Subject: "build.outcomeconsumer", Reads: &source, Guard: guard, Message: "No previous outcome exists.", Steps: []platform.TxnStep{{Name: "checked", Kind: "update", Target: platform.TxnRef{Type: "build.outcomeconsumer", ID: "$subject"}, Changes: []platform.TxnSet{{Field: "checked", From: "=true"}}}}}
	fields := []build.Field{{Name: "coordination", Title: "Coordination subject", Type: "text", FormulaExpr: &key}, {Name: "checked", Title: "Checked", Type: "boolean"}}
	publishObject(t, tn, "builder", "CONSUMER", map[string]any{"name": "outcomeconsumer", "title": "Outcome consumer", "fields": fields, "states": []build.State{{Name: "draft", Title: "Draft"}}, "actions": []build.Action{{Name: "check", Title: "Check original outcome", From: []string{"draft"}, Roles: []string{"user"}, Transaction: transaction}}}, txnAt)
	decide(t, tn, "user", "build", "build.outcomeconsumer.create", "build.outcomeconsumer", "one", nil, txnAt)
	decide(t, tn, "user", "build", "build.outcomeconsumer.check", "build.outcomeconsumer", "one", nil, txnAt)
	if got := declaredRecord(t, tn, "user", "build.outcomeconsumer", "one", txnAt); got["coordination"] != "build.outcomeconsumer/one" || got["checked"] != true {
		t.Fatalf("typed subject identity/query key was not retained: %+v", got)
	}
	fields[0].Read = []string{"builder"}
	decide(t, tn, "builder", "build", "build.object.edit", build.ObjectType, "CONSUMER", map[string]any{"fields": fields}, txnAt)
	decide(t, tn, "builder", "build", "build.object.publish", build.ObjectType, "CONSUMER", nil, txnAt)
	before := txnState(tn)
	if got := refuse(t, tn, "user", "build", "build.outcomeconsumer.check", "build.outcomeconsumer", "one", nil, txnAt); got == "ok" || txnState(tn) != before {
		t.Fatal("a withheld subject query key was treated as a readable empty collection")
	}
	if _, known := tn.entity(core.AccountType); !known {
		t.Fatal("the other owner fixture is unavailable")
	}
	decide(t, tn, "builder", "build", build.QueryType+".create", build.QueryType, "FOREIGN", map[string]any{"name": "foreignaccounts", "title": "Foreign accounts", "description": "A different owner remains outside this query.", "object": core.AccountType, "sort": []string{"id"}, "limit": 20}, txnAt)
	if got := refuse(t, tn, "builder", "build", build.QueryType+".publish", build.QueryType, "FOREIGN", nil, txnAt); got == "ok" {
		t.Fatal("an owner query expanded into another authority")
	}
}
