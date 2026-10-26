package platformserver

import "platformserver/internal/construction"

import (
	"testing"

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
