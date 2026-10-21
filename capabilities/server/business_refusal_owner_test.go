package platformserver

import "platformserver/internal/construction"

import (
	"encoding/json"
	resultformat "platformserver/internal/acceptance/result"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/apps/work"
	"platformserver/platform"
)

func businessRefusalOwnerSetup(t *testing.T, tenant *Tenant) {
	businessRefusalOwnerSetupApproval(t, tenant, false)
}

func businessRefusalOwnerSetupApproval(t *testing.T, tenant *Tenant, approval bool) {
	t.Helper()
	publishObject(t, tenant, "builder", "REQUEST", map[string]any{"name": "request", "title": "Request", "plural": "Requests", "fields": []construction.Field{{Name: "number", Title: "Number", Type: "text"}, {Name: "done", Title: "Done", Type: "boolean"}, {Name: "note", Title: "Note", Type: "text"}}, "states": []construction.State{{Name: "draft", Title: "Draft"}}, "access": []construction.Access{{Role: "operator", Read: "own", Create: true, Edit: true}, {Role: "limited", Read: "own", Create: true, Edit: true}, {Role: "manager", Read: "all"}}}, txnAt)
	publishObject(t, tenant, "builder", "DISPOSITION", map[string]any{"name": "disposition", "title": "Disposition", "plural": "Dispositions", "fields": []construction.Field{{Name: "request", Title: "Request", Type: "reference", Ref: "build.request", Required: true}, {Name: "reason", Title: "Reason", Type: "text", Required: true}}, "access": []construction.Access{{Role: "operator", Read: "own", Create: true}, {Role: "limited", Read: "own"}}}, txnAt)
	declarations := []platform.Refusal{}
	for _, level := range []string{"block", "flag", "route"} {
		when := platform.Expr{Op: "=", Kind: "boolean", Args: []platform.Expr{platform.ExprReference("text", "input.label"), platform.ExprLiteralValue("text", level)}}
		declaration := platform.Refusal{When: &when, Level: level, Code: level + "reason", Message: "The selected resource needs a business disposition", Translations: map[string]string{"zh-CN": "所选资源需要业务处置"}}
		if level == "flag" {
			declaration.Record = &platform.TxnStep{Name: "concession", Kind: "create", Target: platform.TxnRef{Type: "build.disposition"}, Changes: []platform.TxnSet{{Field: "request", From: "$subject"}, {Field: "reason", From: "=Concession approved"}}}
		}
		if level == "route" {
			declaration.Route = &platform.TxnRef{Type: "build.request", ID: "$subject", Action: "rework"}
			reason := platform.ExprReference("text", "note")
			declaration.Record = &platform.TxnStep{Name: "original", Kind: "create", Target: platform.TxnRef{Type: "build.disposition"}, Changes: []platform.TxnSet{{Field: "request", From: "$subject"}, {Field: "reason", Expr: &reason}}}
		}
		declarations = append(declarations, declaration)
	}
	transaction := &platform.BusinessTxn{Subject: "build.request", Steps: []platform.TxnStep{
		{Name: "number", Kind: "sequence", Sequence: &platform.Sequence{Name: "refusalnumbers", Pattern: "R-{n:3}"}},
		{Name: "consume", Kind: "update", Target: platform.TxnRef{Type: "build.request", ID: "$subject"}, Changes: []platform.TxnSet{{Field: "number", From: "step.number"}, {Field: "done", From: "=true"}, {Field: "note", From: "label"}}},
		{Name: "finish", Kind: "create", Target: platform.TxnRef{Type: "build.disposition"}, Changes: []platform.TxnSet{{Field: "request", From: "$subject"}, {Field: "reason", From: "=Completed"}}, Refusals: declarations},
	}}
	alternative := &platform.BusinessTxn{Subject: "build.request", Steps: []platform.TxnStep{
		{Name: "hold", Kind: "update", Target: platform.TxnRef{Type: "build.request", ID: "$subject"}, Changes: []platform.TxnSet{{Field: "done", From: "=false"}, {Field: "note", From: "=Rework required"}}},
		{Name: "rework", Kind: "create", Target: platform.TxnRef{Type: "build.disposition"}, Changes: []platform.TxnSet{{Field: "request", From: "$subject"}, {Field: "reason", From: "=Rework route"}}},
	}}
	inputs := []construction.Input{{Name: "label", Title: "Label", Type: "text", Required: true}}
	apply := construction.Action{Name: "apply", Title: "Apply", From: []string{"draft"}, Roles: []string{"operator", "limited"}, Inputs: inputs, Transaction: transaction}
	if approval {
		apply.Approval = &construction.ActionApproval{Levels: []construction.ApproverLevel{{Title: "Manager disposition", Role: "manager"}}}
	}
	decide(t, tenant, "builder", construction.ID, "build.object.edit", construction.ObjectType, "REQUEST", map[string]any{"actions": []construction.Action{apply, {Name: "rework", Title: "Rework", From: []string{"draft"}, Roles: []string{"operator"}, Inputs: inputs, Transaction: alternative}}}, txnAt)
	decide(t, tenant, "builder", construction.ID, "build.object.publish", construction.ObjectType, "REQUEST", map[string]any{}, txnAt)
	decide(t, tenant, "operator", construction.ID, "build.request.create", "build.request", "operator", map[string]any{"note": "Original request"}, txnAt)
	decide(t, tenant, "limited", construction.ID, "build.request.create", "build.request", "limited", map[string]any{"note": "Original request"}, txnAt)
}

func TestBusinessRefusalOwnerApproval(t *testing.T) {
	compose := func() *Tenant {
		return composeTenant(t, "business-txn", []Seat{seatOf("builder", "build:builder"), seatOf("operator", "build:operator"), seatOf("limited", "build:limited"), seatOf("manager", "build:manager")}, work.New("business-txn"), build.New("business-txn"))
	}
	for _, level := range []string{"block", "flag", "route"} {
		t.Run(level, func(t *testing.T) {
			tenant := compose()
			var entries []Entry
			tenant.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) {
				entries = append(entries, entry)
				return entry.Body, nil
			}
			businessRefusalOwnerSetupApproval(t, tenant, true)
			request := txnRequest(t, tenant, "operator", level)
			request.IdempotencyKey = "approval-" + level
			before := txnState(tenant)
			receipt, err := tenant.Submit(memberOf(t, tenant, "operator"), request, txnAt)
			if level == "block" {
				if err == nil || receipt != nil {
					t.Fatalf("approval preflight accepted a blocked transaction: %v", err)
				}
				saved, _, decodeErr := resultformat.DecodeRefusal(entries[len(entries)-1].Body)
				if decodeErr != nil || len(saved.Issues) != 1 || saved.Issues[0].Code != "business_refusal" {
					t.Fatalf("approval preflight discarded its business refusal diagnosis: %+v %v", saved.Issues, decodeErr)
				}
				txnUnchanged(t, tenant, before)
				CheckReplay(t, tenant, entries, compose)
				return
			}
			if err != nil || receipt == nil {
				t.Fatalf("approval request failed: %v", err)
			}
			if len(tenant.sequences.Clone()) != 0 || tenant.records.types["build.disposition"].rows.Len() != 0 {
				t.Fatal("approval preflight committed a business flag or route")
			}
			id := construction.ID + "." + request.IdempotencyKey
			position := len(entries)
			decide(t, tenant, "manager", work.ID, work.ApprovalType+".approve", work.ApprovalType, id, map[string]any{}, txnAt.Add(time.Minute))
			if len(entries) != position+1 || tenant.records.types["build.disposition"].rows.Len() != 2 {
				t.Fatal("approved business branch did not commit with its original work decision")
			}
			requestView, readErr := tenant.RecordOf(memberOf(t, tenant, "manager"), work.ApprovalType, id, txnAt.Add(time.Minute))
			if readErr != nil {
				t.Fatal(readErr)
			}
			var approval work.ApprovalRequest
			_ = json.Unmarshal(platform.Raw(requestView.Record), &approval)
			if approval.State != "approved" {
				t.Fatalf("business branch was not adopted by its approval: %+v", approval)
			}
			CheckReplay(t, tenant, entries, compose)
		})
	}
}

func TestBusinessRefusalOwnerAtomicBranches(t *testing.T) {
	for _, level := range []string{"block", "flag", "route"} {
		t.Run(level, func(t *testing.T) {
			tenant := txnTenant(t)
			var entries []Entry
			tenant.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) {
				entries = append(entries, entry)
				return entry.Body, nil
			}
			businessRefusalOwnerSetup(t, tenant)
			member := memberOf(t, tenant, "operator")
			request := txnRequest(t, tenant, "operator", level)
			request.IdempotencyKey = "business-" + level
			before := txnState(tenant)
			position := len(entries)
			receipt, refused := tenant.Submit(member, request, txnAt)
			if level == "block" {
				if refused == nil || refused.Code != pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT || receipt != nil {
					t.Fatalf("business block did not return its ordinary refusal: %v %+v", refused, receipt)
				}
				txnUnchanged(t, tenant, before)
			} else if refused != nil || receipt == nil {
				t.Fatalf("%s branch failed: %v", level, refused)
			}
			if len(entries) != position+1 {
				t.Fatal("one business attempt did not produce exactly one saved answer")
			}
			count := 0
			if rows := tenant.records.types["build.disposition"]; rows != nil {
				count = rows.rows.Len()
			}
			if level == "block" && count != 0 || level == "flag" && count != 2 || level == "route" && count != 2 {
				t.Fatalf("%s left the wrong business records: %d", level, count)
			}
			view, err := tenant.RecordOf(member, "build.request", "operator", txnAt)
			if err != nil {
				t.Fatal(err)
			}
			var value map[string]any
			_ = json.Unmarshal(platform.Raw(view.Record), &value)
			if level == "flag" && (value["done"] != true || value["number"] != "R-001") {
				t.Fatalf("flag did not continue its transaction: %+v", value)
			}
			if level == "route" && (value["done"] != false || value["note"] != "Rework required" || value["number"] != nil && value["number"] != "" || len(tenant.sequences.Clone()) != 0) {
				t.Fatalf("route retained the discarded branch: %+v %+v", value, tenant.sequences.Clone())
			}
			if level == "route" {
				found := false
				for _, row := range tenant.records.types["build.disposition"].rows.All() {
					var disposition map[string]any
					_ = json.Unmarshal(platform.Raw(row.Value.Interface()), &disposition)
					found = found || disposition["reason"] == "Original request"
				}
				if !found {
					t.Fatal("route record used the discarded branch's values instead of original facts")
				}
			}
			again, refusal := tenant.Submit(member, request, txnAt.Add(time.Hour))
			if !proto.Equal(again, receipt) || (refused == nil) != (refusal == nil) || refused != nil && *refused != *refusal || len(entries) != position+1 {
				t.Fatal("same input repeated a business result")
			}
			CheckReplay(t, tenant, entries, func() *Tenant { return txnTenant(t) })
		})
	}
	t.Run("target action permissions", func(t *testing.T) {
		tenant := txnTenant(t)
		businessRefusalOwnerSetup(t, tenant)
		before := txnState(tenant)
		_, refusal := tenant.Submit(memberOf(t, tenant, "limited"), txnRequest(t, tenant, "limited", "route"), txnAt)
		if refusal == nil || refusal.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
			t.Fatalf("routing bypassed target action permissions: %v", refusal)
		}
		txnUnchanged(t, tenant, before)
	})
	t.Run("target action input checks", func(t *testing.T) {
		tenant := txnTenant(t)
		businessRefusalOwnerSetup(t, tenant)
		catalog := tenant.app(construction.ID).AcceptedLedger().Catalog
		action, ok := catalog.Action("build.request.rework")
		if !ok || len(action.Payload) != 1 {
			t.Fatal("target action input is unavailable")
		}
		action.Payload = append([]platform.Field(nil), action.Payload...)
		action.Payload[0].Choices = []string{"permitted"}
		catalog.Add(action)
		before := txnState(tenant)
		_, refusal := tenant.Submit(memberOf(t, tenant, "operator"), txnRequest(t, tenant, "operator", "route"), txnAt)
		if refusal == nil || refusal.Code != pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT {
			t.Fatalf("routing skipped target action input validation: %v", refusal)
		}
		txnUnchanged(t, tenant, before)
	})
}
