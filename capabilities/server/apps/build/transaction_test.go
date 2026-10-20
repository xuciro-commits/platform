package build

import "platformserver/internal/construction"

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"platformserver/platform"
)

func TestBusinessTxnPublicationBoundaries(t *testing.T) {
	b, _ := lifecycleBuild()
	header := construction.Object{Name: "header", Title: "Header", Plural: "Headers", Fields: []construction.Field{{Name: "number", Title: "Number", Type: "text"}}}
	detail := construction.Object{Name: "detail", Title: "Detail", Plural: "Details", Fields: []construction.Field{{Name: "header", Title: "Header", Type: "reference", Ref: "build.header"}}}
	b.installed = map[string]platform.Entity{"build.header": Entity(header), "build.detail": Entity(detail)}
	b.jointEntities = map[string]platform.EntityInfo{}
	for typ, entity := range b.installed {
		info, _ := platform.Describe(construction.ID, entity, func(reflect.Type) string { return "" })
		b.jointEntities[typ] = info
	}
	root := construction.Object{Name: "request", Title: "Request", Plural: "Requests", Fields: []construction.Field{{Name: "number", Title: "Number", Type: "text"}}, States: []construction.State{{Name: "draft", Title: "Draft"}}, Actions: []construction.Action{{Name: "apply", Title: "Apply", From: []string{"draft"}, Transaction: &platform.BusinessTxn{Subject: "build.request", Steps: []platform.TxnStep{
		{Name: "number", Kind: "sequence", Sequence: &platform.Sequence{Name: "documents", Pattern: "{n}"}},
		{Name: "root", Kind: "update", Target: platform.TxnRef{Type: "build.request", ID: "$subject"}, Changes: []platform.TxnSet{{Field: "number", From: "step.number"}}},
		{Name: "head", Kind: "create", Target: platform.TxnRef{Type: "build.header"}, Changes: []platform.TxnSet{{Field: "number", From: "step.number"}}},
		{Name: "line", Kind: "create", Target: platform.TxnRef{Type: "build.detail"}, Changes: []platform.TxnSet{{Field: "header", From: "step.head"}}},
	}}}}}
	if err := b.checkTransactions(root); err != nil {
		t.Fatal(err)
	}
	for _, conditional := range []bool{false, true} {
		var candidate construction.Object
		_ = json.Unmarshal(platform.Raw(root), &candidate)
		steps := candidate.Actions[0].Transaction.Steps
		if conditional {
			when := platform.ExprLiteralValue("boolean", false)
			steps[1].When = &when
		} else {
			candidate.Actions[0].Transaction.Steps = append(steps[:1], steps[2:]...)
		}
		if err := b.checkTransactions(candidate); err != nil {
			t.Fatalf("a subject without an unconditional field write was rejected: %v", err)
		}
	}
	root.Sets = []platform.RecordSet{{Name: "lines", From: "lines", Direction: "inverse", Object: "build.detail", Order: []platform.RecordSetSort{{Expr: platform.ExprReference("text", "id")}}, Limit: 10000}}
	for _, which := range []string{"nested iteration", "undeclared set", "missing order", "step limit", "duplicate item writes"} {
		t.Run(which, func(t *testing.T) {
			raw, _ := json.Marshal(root)
			var candidate construction.Object
			_ = json.Unmarshal(raw, &candidate)
			item := platform.TxnStep{Name: "editline", Kind: "update", Target: platform.TxnRef{Type: "build.detail", ID: "$item"}}
			loop := platform.TxnStep{Name: "lines", Kind: "for-each", ForEach: &platform.TxnForEach{Set: "lines", Steps: []platform.TxnStep{item}}}
			candidate.Actions[0].Transaction.Steps = append(candidate.Actions[0].Transaction.Steps, loop)
			last := &candidate.Actions[0].Transaction.Steps[len(candidate.Actions[0].Transaction.Steps)-1]
			switch which {
			case "nested iteration":
				nested := loop
				nested.Name = "nested"
				last.ForEach.Steps = []platform.TxnStep{nested}
			case "undeclared set":
				last.ForEach.Set = "missing"
			case "missing order":
				candidate.Sets[0].Order = nil
			case "step limit":
				for i := 0; i < platform.TxnMaxSteps; i++ {
					step := candidate.Actions[0].Transaction.Steps[0]
					step.Name = fmt.Sprintf("sequence%d", i)
					candidate.Actions[0].Transaction.Steps = append(candidate.Actions[0].Transaction.Steps, step)
				}
			case "duplicate item writes":
				again := item
				again.Name = "again"
				last.ForEach.Steps = append(last.ForEach.Steps, again)
			}
			if err := b.checkTransactions(candidate); err == nil {
				t.Fatalf("%s was published", which)
			}
		})
	}
	for _, kind := range []string{"archive", "link", "unlink", "post", "journal", "effect", "work", "call", "ForEach"} {
		t.Run(kind, func(t *testing.T) {
			raw, _ := json.Marshal(root)
			var bad construction.Object
			_ = json.Unmarshal(raw, &bad)
			bad.Actions[0].Transaction.Steps[2].Kind = kind
			if err := b.checkTransactions(bad); err == nil {
				t.Fatal("unsupported kind accepted")
			}
		})
	}
	for _, which := range []string{"duplicate write", "forward reference", "legacy mutations", "foreign type", "conflicting sequence"} {
		t.Run(which, func(t *testing.T) {
			raw, _ := json.Marshal(root)
			var bad construction.Object
			_ = json.Unmarshal(raw, &bad)
			txn := bad.Actions[0].Transaction
			switch which {
			case "duplicate write":
				step := txn.Steps[1]
				step.Name = "again"
				txn.Steps = append(txn.Steps, step)
			case "forward reference":
				txn.Steps[2], txn.Steps[3] = txn.Steps[3], txn.Steps[2]
			case "legacy mutations":
				bad.Actions[0].Sets = []construction.Set{{Field: "number", From: "=old"}}
			case "foreign type":
				txn.Steps[2].Target.Type = "foreign.header"
			case "conflicting sequence":
				step := txn.Steps[0]
				step.Name = "other"
				step.Sequence = &platform.Sequence{Name: "documents", Pattern: "X{n}"}
				txn.Steps = append(txn.Steps, step)
			}
			if err := b.checkTransactions(bad); err == nil {
				t.Fatal("invalid publication accepted")
			}
		})
	}
	for _, raw := range []string{`{"subject":"build.request","steps":[],"guard":[]}`, `{"subject":"build.request","steps":[{"kind":"create","when":[]}]}`, `{"subject":"build.request","steps":[{"kind":"create","target":{"type":"build.header","script":"x"}}]}`} {
		var txn platform.BusinessTxn
		if err := json.Unmarshal([]byte(raw), &txn); err == nil {
			t.Fatalf("unsupported language was ignored: %v", err)
		}
	}
}
