package platformserver

import "platformserver/internal/construction"

import (
	"context"
	"encoding/json"
	resultformat "platformserver/internal/acceptance/result"
	"strings"
	"testing"
	"time"

	"platformserver/platform"
)

func businessVersionSetup(t *testing.T, tn *Tenant, policy string) {
	t.Helper()
	access := []construction.Access{{Role: construction.User, Read: "all", Create: true, Edit: true, Archive: true}}
	publishObject(t, tn, "builder", "ROUTE", map[string]any{"name": "routing", "title": "Routing", "fields": []construction.Field{
		{Name: "code", Title: "Code", Type: "text", Required: true}, {Name: "version", Title: "Business version", Type: "integer", Required: true},
		{Name: "from", Title: "Valid from", Type: "date", Required: true}, {Name: "until", Title: "Valid until", Type: "date"}, {Name: "name", Title: "Name", Type: "text"},
	}, "access": access, "versioning": platform.Versioning{Key: "code", Version: "version", ValidFrom: "from", ValidUntil: "until", Pin: policy}}, txnAt)
	publishObject(t, tn, "builder", "OPERATION", map[string]any{"name": "operation", "title": "Operation", "fields": []construction.Field{{Name: "routing", Title: "Routing", Type: "reference", Ref: "build.routing", Inverse: "operations", Required: true}, {Name: "ordinal", Title: "Ordinal", Type: "integer", Required: true}}, "access": access}, txnAt)
	decide(t, tn, "builder", construction.ID, "build.object.edit", construction.ObjectType, "ROUTE", map[string]any{"sets": []platform.RecordSet{{Name: "operations", From: "operations", Direction: "inverse", Object: "build.operation", Order: []platform.RecordSetSort{{Expr: platform.ExprReference("integer", "ordinal")}}, Limit: 100}}}, txnAt)
	decide(t, tn, "builder", construction.ID, "build.object.publish", construction.ObjectType, "ROUTE", nil, txnAt)
	publishObject(t, tn, "builder", "ORDER", map[string]any{"name": "order", "title": "Order", "fields": []construction.Field{{Name: "routing", Title: "Routing", Type: "reference", Ref: "build.routing", Required: true}, {Name: "routingversion", Title: "Pinned routing version", Type: "integer"}, {Name: "note", Title: "Note", Type: "text"}}, "states": []construction.State{{Name: "draft", Title: "Draft"}, {Name: "released", Title: "Released"}}, "access": access,
		"versionPins": []platform.VersionPinBinding{{Reference: "routing", Version: "routingversion"}},
		"constraints": []platform.Constraint{{Name: "fixedrouting", Kind: "immutable", Fields: []string{"routing", "routingversion"}, Message: "Released routing stays fixed", Translations: map[string]string{"zh-CN": "已下达工单的工艺必须固定"}}},
		"actions":     []construction.Action{{Name: "release", Title: "Release", From: []string{"draft"}, To: "released", Roles: []string{construction.User}}},
	}, txnAt)
}

func TestBusinessVersionsPinTimeAndPureRecovery(t *testing.T) {
	tn := builderTenant(t, "business-versions")
	dir := t.TempDir()
	journal, journalErr := OpenFileJournal(dir)
	if journalErr != nil {
		t.Fatal(journalErr)
	}
	defer journal.Close()
	if _, err := journal.Entries(context.Background(), tn.ID, 0); err != nil {
		t.Fatal(err)
	}
	var entries []Entry
	tn.AcceptResult = func(entry Entry, key, hash string) ([]byte, error) {
		raw, err := journal.AppendAccepted(context.Background(), tn.ID, entry, key, hash)
		if err == nil {
			entry.Body = raw
			entries = append(entries, entry)
		}
		return raw, err
	}
	businessVersionSetup(t, tn, "on_state:released")
	before, _, err := tn.Snapshot(func() int64 { return 0 })
	if err != nil {
		t.Fatal(err)
	}
	start := len(entries)
	createRoute := func(id string, version int, from, until string, at time.Time) {
		decide(t, tn, "user", construction.ID, "build.routing.create", "build.routing", id, map[string]any{"code": "R", "version": version, "from": from, "until": until, "name": "Route"}, at)
	}
	createRoute("v1", 1, "2025-01-01", "2025-06-01", txnAt)
	createRoute("v2", 2, "2025-06-01", "", txnAt)
	decide(t, tn, "user", construction.ID, "build.operation.create", "build.operation", "op2", map[string]any{"routing": "v2", "ordinal": 1}, txnAt)
	decide(t, tn, "user", construction.ID, "build.order.create", "build.order", "first", map[string]any{"routing": "v2"}, txnAt)
	decide(t, tn, "user", construction.ID, "build.order.release", "build.order", "first", nil, txnAt)
	read := func(id string) map[string]any {
		view, err := tn.RecordOf(memberOf(t, tn, "user"), "build.order", id, txnAt)
		if err != nil {
			t.Fatal(err)
		}
		var raw map[string]any
		_ = json.Unmarshal(platform.Raw(view.Record), &raw)
		return raw
	}
	first := read("first")
	if first["routingversion"] != float64(2) || len(first["versionPins"].([]any)) != 1 {
		t.Fatalf("version was not automatically pinned: %v", first)
	}
	createRoute("v3", 3, "2025-07-01", "", txnAt)
	decide(t, tn, "user", construction.ID, "build.order.create", "build.order", "second", map[string]any{"routing": "v3"}, txnAt)
	decide(t, tn, "user", construction.ID, "build.order.release", "build.order", "second", nil, txnAt)
	if read("first")["routingversion"] != float64(2) || read("second")["routingversion"] != float64(3) {
		t.Fatal("new version changed an in-flight order")
	}
	for _, test := range []struct {
		schema, typ, id string
		payload         map[string]any
		reason          string
	}{
		{"build.order.edit", "build.order", "first", map[string]any{"routing": "v3"}, "pinned"},
		{"build.routing.edit", "build.routing", "v2", map[string]any{"name": "Changed"}, "Pinned business version content"},
		{"build.operation.edit", "build.operation", "op2", map[string]any{"ordinal": 2}, "declared records"},
		{"build.routing.create", "build.routing", "duplicate", map[string]any{"code": "R", "version": 2, "from": "2025-06-01"}, "business-version-unique"},
		{"build.routing.create", "build.routing", "invalid", map[string]any{"code": "X", "version": 1, "from": "2025-06-01", "until": "2025-05-01"}, "valid-until"},
		{"build.order.create", "build.order", "expired", map[string]any{"routing": "v1"}, "currently effective"},
		{"build.routing.archive", "build.routing", "v2", nil, "cannot be archived"},
	} {
		state := txnState(tn)
		if got := refuse(t, tn, "user", construction.ID, test.schema, test.typ, test.id, test.payload, txnAt); !strings.Contains(got, test.reason) {
			t.Fatalf("%s: %s", test.schema, got)
		}
		if txnState(tn) != state {
			t.Fatal("refusal changed business records")
		}
	}
	decide(t, tn, "user", construction.ID, "build.routing.edit", "build.routing", "v2", map[string]any{"until": "2025-07-01"}, txnAt)
	decide(t, tn, "user", construction.ID, "build.order.edit", "build.order", "first", map[string]any{"note": "Continue old order"}, txnAt)
	page, refusal := tn.Records(memberOf(t, tn, "user"), "build.routing", platform.Query{}, txnAt)
	if refusal != nil || page.Total != 1 {
		t.Fatalf("current effective versions: %+v %v", page, refusal)
	}
	past, refusal := tn.Records(memberOf(t, tn, "user"), "build.routing", platform.Query{BusinessTime: &platform.BusinessTime{At: "2025-03-01"}}, txnAt)
	if refusal != nil || past.Total != 1 {
		t.Fatalf("past effective versions: %+v %v", past, refusal)
	}
	all, refusal := tn.Records(memberOf(t, tn, "user"), "build.routing", platform.Query{BusinessTime: &platform.BusinessTime{All: true}}, txnAt)
	if refusal != nil || all.Total != 3 {
		t.Fatalf("retained versions: %+v %v", all, refusal)
	}
	caller := tn.caller(memberOf(t, tn, "user"), tn.app(construction.ID), false)
	set, setErr := caller.ReadRecordSet("build.routing", "v2", "operations", txnAt)
	if setErr != nil || len(set.Records) != 1 {
		t.Fatalf("expired pinned operations unreadable: %v", setErr)
	}
	expected := snapshot(tn)
	journal.Close()
	reopenedJournal, err := OpenFileJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopenedJournal.Close()
	persisted, err := reopenedJournal.Entries(context.Background(), tn.ID, 0)
	if err != nil || len(persisted) != len(entries) {
		t.Fatalf("journal reopen: %v", err)
	}
	reopened := builderTenant(t, "business-versions")
	if err := reopened.Restore(before); err != nil {
		t.Fatal(err)
	}
	for _, entry := range persisted[start:] {
		if err := reopened.Replay([]Entry{entry}); err != nil {
			t.Fatal(err)
		}
	}
	if snapshot(reopened) != expected {
		t.Fatal("business version recovery changed saved bytes")
	}
}

func TestPinnedBusinessVersionPreservesSourceReadPermissions(t *testing.T) {
	tn := builderTenant(t, "version-permissions")
	businessVersionSetup(t, tn, "on_create")
	decide(t, tn, "builder", construction.ID, "build.routing.create", "build.routing", "v1", map[string]any{"code": "R", "version": 1, "from": "2025-01-01"}, txnAt)
	decide(t, tn, "builder", construction.ID, "build.order.create", "build.order", "first", map[string]any{"routing": "v1"}, txnAt)
	// Retain the same source shape while revoking its version field from users.
	draft, _ := tn.RecordOf(memberOf(t, tn, "builder"), construction.ObjectType, "ROUTE", txnAt)
	var source construction.Object
	_ = json.Unmarshal(platform.Raw(draft.Record), &source)
	for i := range source.Fields {
		if source.Fields[i].Name == "version" {
			source.Fields[i].Read = []string{construction.Builder}
		}
	}
	decide(t, tn, "builder", construction.ID, "build.object.edit", construction.ObjectType, "ROUTE", map[string]any{"fields": source.Fields}, txnAt)
	decide(t, tn, "builder", construction.ID, "build.object.publish", construction.ObjectType, "ROUTE", nil, txnAt)
	view, err := tn.RecordOf(memberOf(t, tn, "user"), "build.order", "first", txnAt)
	var values map[string]any
	_ = json.Unmarshal(platform.Raw(view.Record), &values)
	if err != nil || values["routingversion"] != nil || values["versionPins"] != nil {
		t.Fatalf("pin leaked revoked source value: %v %v", values, err)
	}
	state := txnState(tn)
	if got := refuse(t, tn, "user", construction.ID, "build.order.create", "build.order", "unreadable", map[string]any{"routing": "v1"}, txnAt); !strings.Contains(got, "not readable") {
		t.Fatal(got)
	}
	if txnState(tn) != state {
		t.Fatal("unreadable version pin changed business state")
	}
}

func TestBusinessVersionOnCreateAndPublicationBoundaries(t *testing.T) {
	tn := builderTenant(t, "creation-versions")
	businessVersionSetup(t, tn, "on_create")
	decide(t, tn, "user", construction.ID, "build.routing.create", "build.routing", "v1", map[string]any{"code": "R", "version": 1, "from": "2025-01-01"}, txnAt)
	decide(t, tn, "user", construction.ID, "build.order.create", "build.order", "first", map[string]any{"routing": "v1"}, txnAt)
	view, _ := tn.RecordOf(memberOf(t, tn, "user"), "build.order", "first", txnAt)
	var values map[string]any
	_ = json.Unmarshal(platform.Raw(view.Record), &values)
	if values["routingversion"] != float64(1) {
		t.Fatal("on-create did not pin")
	}
	decide(t, tn, "builder", construction.ID, "build.object.edit", construction.ObjectType, "ROUTE", map[string]any{"versioning": nil}, txnAt)
	if got := refuse(t, tn, "builder", construction.ID, "build.object.publish", construction.ObjectType, "ROUTE", nil, txnAt); !strings.Contains(got, "retained") {
		t.Fatal(got)
	}
}

func TestBusinessVersionPinAndDocumentLinesShareOneAcceptedResult(t *testing.T) {
	tn := builderTenant(t, "version-transaction")
	businessVersionSetup(t, tn, "on_create")
	decide(t, tn, "user", construction.ID, "build.routing.create", "build.routing", "v1", map[string]any{"code": "R", "version": 1, "from": "2025-01-01"}, txnAt)
	publishObject(t, tn, "builder", "LINE", map[string]any{"name": "orderline", "title": "Order line", "fields": []construction.Field{{Name: "order", Title: "Order", Type: "reference", Ref: "build.order", Required: true}, {Name: "ordinal", Title: "Ordinal", Type: "integer", Required: true}}}, txnAt)
	txn := &platform.BusinessTxn{Subject: "build.request", Steps: []platform.TxnStep{
		{Name: "order", Kind: "create", Target: platform.TxnRef{Type: "build.order"}, Changes: []platform.TxnSet{{Field: "routing", From: "=v1"}}},
		{Name: "line", Kind: "create", Target: platform.TxnRef{Type: "build.orderline"}, Changes: []platform.TxnSet{{Field: "order", From: "step.order"}, {Field: "ordinal", From: "=1"}}},
		{Name: "request", Kind: "update", Target: platform.TxnRef{Type: "build.request", ID: "$subject"}},
	}}
	publishObject(t, tn, "builder", "REQUEST", map[string]any{"name": "request", "title": "Release request", "fields": []construction.Field{{Name: "note", Title: "Note", Type: "text"}}, "states": []construction.State{{Name: "draft", Title: "Draft"}}, "actions": []construction.Action{{Name: "release", Title: "Release", From: []string{"draft"}, Roles: []string{construction.User}, Transaction: txn}}}, txnAt)
	decide(t, tn, "user", construction.ID, "build.request.create", "build.request", "request", nil, txnAt)
	var result []byte
	tn.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) { result = entry.Body; return entry.Body, nil }
	decision := decide(t, tn, "user", construction.ID, "build.request.release", "build.request", "request", nil, txnAt)
	batch, _, err := resultformat.DecodeBatch(result)
	if err != nil || len(batch.Rows) != 3 {
		t.Fatalf("document and line did not share accepted rows: %v", err)
	}
	view, refusal := tn.RecordOf(memberOf(t, tn, "user"), "build.order", decision.GetChangeId()+"-order", txnAt)
	var values map[string]any
	_ = json.Unmarshal(platform.Raw(view.Record), &values)
	if refusal != nil || values["routingversion"] != float64(1) || len(values["versionPins"].([]any)) != 1 {
		t.Fatal("transaction lost its automatic pin")
	}
}
