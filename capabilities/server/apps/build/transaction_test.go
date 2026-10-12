package build

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"platformserver/platform"
)

func TestBusinessTxnPublicationBoundaries(t *testing.T) {
	b, _ := lifecycleBuild()
	header := Object{Name: "header", Title: "Header", Plural: "Headers", Fields: []Field{{Name: "number", Title: "Number", Type: "text"}}}
	detail := Object{Name: "detail", Title: "Detail", Plural: "Details", Fields: []Field{{Name: "header", Title: "Header", Type: "reference", Ref: "build.header"}}}
	b.installed = map[string]platform.Entity{"build.header": Entity(header), "build.detail": Entity(detail)}
	b.jointEntities = map[string]platform.EntityInfo{}
	for typ, entity := range b.installed {
		info, _ := platform.Describe(ID, entity, func(reflect.Type) string { return "" })
		b.jointEntities[typ] = info
	}
	root := Object{Name: "request", Title: "Request", Plural: "Requests", Fields: []Field{{Name: "number", Title: "Number", Type: "text"}}, States: []State{{Name: "draft", Title: "Draft"}}, Actions: []Action{{Name: "apply", Title: "Apply", From: []string{"draft"}, Transaction: &platform.BusinessTxn{Subject: "build.request", Steps: []platform.TxnStep{
		{Name: "number", Kind: "sequence", Sequence: &platform.Sequence{Name: "documents", Pattern: "{n}"}},
		{Name: "root", Kind: "update", Target: platform.TxnRef{Type: "build.request", ID: "$subject"}, Changes: []platform.TxnSet{{Field: "number", From: "step.number"}}},
		{Name: "head", Kind: "create", Target: platform.TxnRef{Type: "build.header"}, Changes: []platform.TxnSet{{Field: "number", From: "step.number"}}},
		{Name: "line", Kind: "create", Target: platform.TxnRef{Type: "build.detail"}, Changes: []platform.TxnSet{{Field: "header", From: "step.head"}}},
	}}}}}
	if err := b.checkTransactions(root); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"archive", "link", "unlink", "post", "journal", "effect", "work", "call", "ForEach"} {
		t.Run(kind, func(t *testing.T) {
			raw, _ := json.Marshal(root)
			var bad Object
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
			var bad Object
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
				bad.Actions[0].Sets = []Set{{Field: "number", From: "=old"}}
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
		if err := json.Unmarshal([]byte(raw), &txn); err == nil || !strings.Contains(err.Error(), "unknown field") {
			t.Fatalf("unsupported language was ignored: %v", err)
		}
	}
}
