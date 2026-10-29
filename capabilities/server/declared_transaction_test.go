package platformserver

import (
	"testing"

	"platformserver/platform"
	"platformserver/studio/build"
)

func TestDeclaredPeriodDateEnumeration(t *testing.T) {
	tn := builderTenant(t, "period-dates")
	publishObject(t, tn, "builder", "DAY", map[string]any{"name": "periodday", "title": "Period day", "fields": []build.Field{{Name: "date", Title: "Date", Type: "date"}, {Name: "ordinal", Title: "Ordinal", Type: "integer"}}}, txnAt)
	publishObject(t, tn, "builder", "PERIOD", map[string]any{"name": "periodcalendar", "title": "Period calendar", "fields": []build.Field{{Name: "from", Title: "From", Type: "date"}, {Name: "until", Title: "Until", Type: "date"}}, "states": []build.State{{Name: "draft", Title: "Draft"}}, "actions": []build.Action{{Name: "expand", Title: "Expand dates", From: []string{"draft"}, Roles: []string{"user"}, Transaction: &platform.BusinessTxn{Subject: "build.periodcalendar", Steps: []platform.TxnStep{{Name: "days", Kind: "for-each", ForEach: &platform.TxnForEach{Range: &platform.TxnRange{From: platform.ExprReference("date", "from"), Until: platform.ExprReference("date", "until"), Unit: "day", Step: 1, Limit: 31}, Steps: []platform.TxnStep{{Name: "day", Kind: "create", Target: platform.TxnRef{Type: "build.periodday"}, Changes: []platform.TxnSet{{Field: "date", From: "row.start"}, {Field: "ordinal", From: "row.index"}}}}}}}}}}}, txnAt)
	decide(t, tn, "user", "build", "build.periodcalendar.create", "build.periodcalendar", "october", map[string]any{"from": "2026-10-01", "until": "2026-11-01"}, txnAt)
	decide(t, tn, "user", "build", "build.periodcalendar.expand", "build.periodcalendar", "october", nil, txnAt)
	rows, failure := tn.Records(memberOf(t, tn, "user"), "build.periodday", platform.Query{Limit: 100}, txnAt)
	if failure != nil || rows.Total != 31 || len(rows.Records) != 31 {
		t.Fatalf("period dates: %+v %v", rows, failure)
	}
}

func TestDeclaredQuantityCreatesStableChildren(t *testing.T) {
	tn := builderTenant(t, "quantity-groups")
	publishObject(t, tn, "builder", "GROUP", map[string]any{"name": "groupplan", "title": "Group plan", "fields": []build.Field{{Name: "quantity", Title: "Quantity", Type: "integer", Required: true}}, "states": []build.State{{Name: "draft", Title: "Draft"}, {Name: "split", Title: "Split"}}}, txnAt)
	publishObject(t, tn, "builder", "ITEM", map[string]any{"name": "groupitem", "title": "Group item", "fields": []build.Field{{Name: "group", Title: "Group", Type: "reference", Ref: "build.groupplan"}, {Name: "ordinal", Title: "Ordinal", Type: "integer"}}}, txnAt)
	transaction := &platform.BusinessTxn{Subject: "build.groupplan", Steps: []platform.TxnStep{
		{Name: "items", Kind: "for-each", ForEach: &platform.TxnForEach{Range: &platform.TxnRange{From: platform.ExprLiteralValue("integer", 0), Until: platform.ExprReference("integer", "quantity"), Unit: "item", Step: 1, Limit: 20}, Steps: []platform.TxnStep{{Name: "item", Kind: "create", Target: platform.TxnRef{Type: "build.groupitem"}, Changes: []platform.TxnSet{{Field: "group", From: "$subject"}, {Field: "ordinal", From: "row.start"}}}}}},
		{Name: "finish", Kind: "update", Target: platform.TxnRef{Type: "build.groupplan", ID: "$subject"}, Changes: []platform.TxnSet{{Field: "state", From: "=split"}}},
	}}
	decide(t, tn, "builder", "build", "build.object.edit", build.ObjectType, "GROUP", map[string]any{"actions": []build.Action{{Name: "split", Title: "Split group", From: []string{"draft"}, Roles: []string{"user"}, Transaction: transaction}}}, txnAt)
	decide(t, tn, "builder", "build", build.SchemaPublish, build.ObjectType, "GROUP", nil, txnAt)
	decide(t, tn, "user", "build", "build.groupplan.create", "build.groupplan", "group", map[string]any{"quantity": 3}, txnAt)
	request := submission(tn, memberOf(t, tn, "user"), "build", "build.groupplan.split", "build.groupplan", "group", nil)
	first, failure := tn.Submit(memberOf(t, tn, "user"), request, txnAt)
	if failure != nil {
		t.Fatal(failure)
	}
	again, failure := tn.Submit(memberOf(t, tn, "user"), request, txnAt)
	rows, readFailure := tn.Records(memberOf(t, tn, "user"), "build.groupitem", platform.Query{Limit: 100}, txnAt)
	if failure != nil || readFailure != nil || first.GetChangeId() != again.GetChangeId() || rows.Total != 3 {
		t.Fatalf("quantity split or retry duplicated children: %+v %v %v", rows, failure, readFailure)
	}
}
