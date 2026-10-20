package build

import "platformserver/internal/construction"

import (
	"encoding/json"
	"reflect"
	"testing"

	"platformserver/platform"
)

func TestTypedTransactionPublicationBoundaries(t *testing.T) {
	b, _ := lifecycleBuild()
	balance := construction.Object{Name: "balance", Title: "Balance", Fields: []construction.Field{{Name: "item", Title: "Item", Type: "text"}, {Name: "amount", Title: "Amount", Type: "decimal"}}}
	b.installed["build.balance"] = Entity(balance)
	attached := b.host.(lifecycleHost)
	attached.info, _ = platform.Describe(construction.ID, b.installed["build.balance"], func(reflect.Type) string { return "" })
	b.host = attached
	root := construction.Object{Name: "receipt", Title: "Receipt", Fields: []construction.Field{{Name: "status", Title: "Status", Type: "text"}}, States: []construction.State{{Name: "draft", Title: "Draft"}}, Actions: []construction.Action{{Name: "receive", Title: "Receive", From: []string{"draft"}, Transaction: &platform.BusinessTxn{Subject: "build.receipt", Steps: []platform.TxnStep{{Name: "subject", Kind: "update", Target: platform.TxnRef{Type: "build.receipt", ID: "$subject"}}, {Name: "stock", Kind: "post", Target: platform.TxnRef{Type: "build.balance"}, Post: &platform.TxnPost{Match: []platform.TxnSet{{Field: "item", From: "=one"}}, Field: "amount", Amount: platform.ExprLiteralValue("decimal", "0.1")}}}}}}}
	if err := b.checkTransactions(root); err != nil {
		t.Fatal(err)
	}
	for _, which := range []string{"foreign balance", "text balance", "wrong amount", "duplicate balance", "authoritative call", "call view", "effect without declaration", "work without recipient", "mixed payload"} {
		t.Run(which, func(t *testing.T) {
			var object construction.Object
			if err := json.Unmarshal(platform.Raw(root), &object); err != nil {
				t.Fatal(err)
			}
			step := &object.Actions[0].Transaction.Steps[1]
			switch which {
			case "foreign balance":
				step.Target.Type = "core.balance"
			case "text balance":
				step.Post.Field = "item"
			case "wrong amount":
				step.Post.Amount = platform.ExprLiteralValue("text", "0.1")
			case "duplicate balance":
				duplicate := *step
				duplicate.Name = "again"
				object.Actions[0].Transaction.Steps = append(object.Actions[0].Transaction.Steps, duplicate)
			case "authoritative call":
				*step = platform.TxnStep{Name: "call", Kind: "call", Call: &platform.TxnCall{Ref: platform.AssetRef{App: construction.ID, Kind: platform.AssetAction, Name: "build.receipt.receive"}, View: "snapshot", ResultType: platform.ExprType{Kind: "text"}}}
			case "call view":
				*step = platform.TxnStep{Name: "call", Kind: "call", Call: &platform.TxnCall{Ref: platform.AssetRef{App: construction.ID, Kind: platform.AssetQuery, Name: "stock"}, View: "latest", ResultType: platform.ExprType{Kind: "text"}}}
			case "effect without declaration":
				*step = platform.TxnStep{Name: "external", Kind: "effect", Effect: &platform.TxnEffect{}}
			case "work without recipient":
				*step = platform.TxnStep{Name: "task", Kind: "work", Work: &platform.TxnWork{Title: "Handle receipt"}}
			case "mixed payload":
				step.Work = &platform.TxnWork{Title: "Handle receipt", Role: construction.User}
			}
			if err := b.checkTransactions(object); err == nil {
				t.Fatal("Invalid typed transaction was allowed for publication")
			}
		})
	}
}

func TestCanonicalTransactionReplacesLegacyActionsAndNumbering(t *testing.T) {
	b, _ := lifecycleBuild()
	b.installed["build.balance"] = Entity(construction.Object{Name: "balance", Title: "Balance", Fields: []construction.Field{{Name: "item", Title: "Item", Type: "text"}, {Name: "amount", Title: "Amount", Type: "decimal"}}})
	attached := b.host.(lifecycleHost)
	attached.info, _ = platform.Describe(construction.ID, b.installed["build.balance"], func(reflect.Type) string { return "" })
	b.host = attached
	object := construction.Object{Name: "receipt", Title: "Receipt", Fields: []construction.Field{{Name: "number", Title: "Number", Type: "text"}, {Name: "item", Title: "Item", Type: "text"}, {Name: "amount", Title: "Amount", Type: "decimal"}}, States: []construction.State{{Name: "draft", Title: "Draft"}, {Name: "done", Title: "Done"}}, Numbering: &construction.Numbering{Field: "number", Prefix: "R", Yearly: true, Width: 12}, Actions: []construction.Action{{Name: "receive", Title: "Receive", From: []string{"draft"}, To: "done", Posts: []construction.Post{{Object: "build.balance", Match: []construction.Set{{Field: "item", From: "record.item"}}, Field: "amount", Amount: "record.amount"}}}}}
	object.Published = string(platform.Raw(object))
	frozen := object.Published
	canonical, err := b.CanonicalObject(object)
	if err != nil {
		t.Fatal(err)
	}
	if canonical.Published != frozen || canonical.Numbering != nil || canonical.CreateTransaction == nil || canonical.CreateTransaction.Steps[0].Sequence.SeedRecord != "build.receipt" {
		t.Fatal("Canonical definition lost historical bytes or retained count-based numbering")
	}
	if got := canonical.CreateTransaction.Steps[0].Sequence.Format(2026, 7); got != "R2026-000000000007" {
		t.Fatalf("Migrated number width changed: %s", got)
	}
	action := canonical.Actions[0]
	if action.Transaction == nil || action.To != "" || len(action.Posts) > 0 || action.Transaction.Steps[1].Post.Amount.Ref != "amount" {
		t.Fatal("Legacy posting was not replaced by a typed transaction")
	}
}
