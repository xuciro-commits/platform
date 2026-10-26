package platformserver

import "platformserver/internal/construction"

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"platformserver/internal/acceptance"
	resultformat "platformserver/internal/acceptance/result"
	"reflect"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/apps/core"
	"platformserver/apps/work"
	"platformserver/platform"
)

var txnAt = time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)

func txnTenant(t *testing.T) *Tenant {
	return composeTenant(t, "business-txn", []Seat{seatOf("builder", "build:builder"), seatOf("operator", "build:operator"), seatOf("limited", "build:limited")}, core.New("business-txn"), work.New("business-txn"), build.New("business-txn"))
}
func txnDefinition() *platform.BusinessTxn {
	decimal := func(value string) *platform.Expr { x := platform.ExprLiteralValue("decimal", value); return &x }
	return &platform.BusinessTxn{Subject: "build.request", Steps: []platform.TxnStep{
		{Name: "number", Kind: "sequence", Sequence: &platform.Sequence{Name: "documents", Pattern: "D-{n:5}"}},
		{Name: "input", Kind: "update", Target: platform.TxnRef{Type: "build.request", ID: "$subject"}, Changes: []platform.TxnSet{{Field: "consumed", From: "=true"}, {Field: "number", From: "step.number"}, {Field: "note", From: "label"}}},
		{Name: "head", Kind: "create", Target: platform.TxnRef{Type: "build.header"}, Changes: []platform.TxnSet{{Field: "number", From: "step.number"}, {Field: "owner", From: "$me"}, {Field: "label", From: "label"}}},
		{Name: "before", Kind: "call", Call: &platform.TxnCall{Ref: platform.AssetRef{App: construction.ID, Kind: platform.AssetQuery, Name: "headers"}, Version: 1, ResultPath: []string{"total"}, ResultType: platform.ExprType{Kind: "integer"}}},
		{Name: "after", Kind: "call", Call: &platform.TxnCall{Ref: platform.AssetRef{App: construction.ID, Kind: platform.AssetQuery, Name: "headers"}, Version: 1, View: "current", ResultPath: []string{"total"}, ResultType: platform.ExprType{Kind: "integer"}}},
		{Name: "first", Kind: "create", Target: platform.TxnRef{Type: "build.detail"}, Changes: []platform.TxnSet{{Field: "header", From: "step.head"}, {Field: "note", From: "label"}, {Field: "before", From: "step.before"}, {Field: "after", From: "step.after"}}},
		{Name: "second", Kind: "create", Target: platform.TxnRef{Type: "build.detail"}, Changes: []platform.TxnSet{{Field: "header", From: "step.head"}, {Field: "note", From: "label"}}},
		{Name: "stock", Kind: "post", Target: platform.TxnRef{Type: "build.balance"}, Post: &platform.TxnPost{Match: []platform.TxnSet{{Field: "item", From: "=goods"}}, Field: "quantity", Amount: platform.Expr{Op: "+", Kind: "decimal", Args: []platform.Expr{*decimal("0.1"), *decimal("0.2")}}}},
		{Name: "export", Kind: "effect", Target: platform.TxnRef{Type: "build.header", ID: "step.head"}, Effect: &platform.TxnEffect{Writeback: "EXPORT", Payload: []platform.TxnSet{{Field: "number", From: "step.number"}}}},
		{Name: "books", Kind: "journal", Journal: &platform.TxnJournal{Lines: []platform.TxnJournalLine{{Account: platform.ExprLiteralValue("text", "1403"), Debit: decimal("0.3")}, {Account: platform.ExprLiteralValue("text", "2290"), Credit: decimal("0.3")}}}},
		{Name: "review", Kind: "work", Target: platform.TxnRef{Type: "build.header", ID: "step.head"}, Work: &platform.TxnWork{Title: "Review received goods", Role: "operator"}},
	}}
}
func txnSetup(t *testing.T, tn *Tenant) {
	publishObject(t, tn, "builder", "HEADER", map[string]any{"name": "header", "title": "Header", "plural": "Headers", "fields": []construction.Field{{Name: "label", Title: "Label", Type: "longtext", Required: true}, {Name: "number", Title: "Number", Type: "text", Required: true}, {Name: "owner", Title: "Owner", Type: "text", Required: true}}, "scope": construction.ObjectScope{Owner: "owner"}, "access": []construction.Access{{Role: "operator", Read: "own", Create: true}, {Role: "limited", Read: "own", Create: true}}}, txnAt)
	publishObject(t, tn, "builder", "DETAIL", map[string]any{"name": "detail", "title": "Detail", "plural": "Details", "fields": []construction.Field{{Name: "header", Title: "Header", Type: "reference", Ref: "build.header", Required: true}, {Name: "note", Title: "Note", Type: "longtext", Required: true}, {Name: "before", Title: "Snapshot head count", Type: "integer"}, {Name: "after", Title: "Current head count", Type: "integer"}}, "access": []construction.Access{{Role: "operator", Read: "own", Create: true}, {Role: "limited", Read: "own"}}}, txnAt)
	publishObject(t, tn, "builder", "BALANCE", map[string]any{"name": "balance", "title": "Balance", "fields": []construction.Field{{Name: "item", Title: "Item", Type: "text"}, {Name: "quantity", Title: "Quantity", Type: "decimal"}}, "access": []construction.Access{{Role: "operator", Read: "all", Create: true, Edit: true}}}, txnAt)
	decide(t, tn, "builder", construction.ID, "build.query.create", construction.QueryType, "HEADERS", map[string]any{"name": "headers", "title": "Headers", "description": "Visible receipt headers", "object": "build.header", "limit": 200, "sort": []string{"id"}}, txnAt)
	decide(t, tn, "builder", construction.ID, "build.query.publish", construction.QueryType, "HEADERS", map[string]any{}, txnAt)
	tn.Outbound = func(*http.Request, bool) (*http.Response, error) { return confirmationResponse(200, `{}`) }
	decide(t, tn, "builder", construction.ID, "build.connection.create", construction.ConnectionType, "DESTINATION", map[string]any{"name": "warehouse", "title": "Warehouse", "kind": "http", "address": "https://warehouse.example"}, txnAt)
	decide(t, tn, "builder", construction.ID, "build.connection.check", construction.ConnectionType, "DESTINATION", map[string]any{}, txnAt)
	tn.CheckConnections(txnAt)
	decide(t, tn, "builder", construction.ID, "build.writeback.create", construction.WritebackType, "EXPORT", map[string]any{"name": "receipt", "title": "Receipt export", "connection": "DESTINATION", "object": "build.header", "on": "transaction", "path": "/receipts"}, txnAt)
	decide(t, tn, "builder", construction.ID, "build.writeback.publish", construction.WritebackType, "EXPORT", map[string]any{}, txnAt)
	tn.Outbound = func(*http.Request, bool) (*http.Response, error) {
		t.Fatal("a transaction or recovery contacted an external service")
		return nil, io.EOF
	}
	publishObject(t, tn, "builder", "REQUEST", map[string]any{"name": "request", "title": "Request", "plural": "Requests", "fields": []construction.Field{{Name: "consumed", Title: "Consumed", Type: "boolean"}, {Name: "number", Title: "Number", Type: "text"}, {Name: "note", Title: "Note", Type: "longtext"}}, "states": []construction.State{{Name: "draft", Title: "Draft"}, {Name: "posted", Title: "Posted"}}, "access": []construction.Access{{Role: "operator", Read: "own", Create: true, Edit: true}, {Role: "limited", Read: "own", Create: true, Edit: true}}, "actions": []construction.Action{{Name: "apply", Title: "Apply transaction", From: []string{"draft"}, To: "posted", Roles: []string{"operator", "limited"}, Inputs: []construction.Input{{Name: "label", Title: "Label", Type: "longtext"}}, Transaction: txnDefinition()}}}, txnAt)
	for _, who := range []string{"operator", "limited"} {
		decide(t, tn, who, construction.ID, "build.request.create", "build.request", who, map[string]any{"note": "unconsumed input"}, txnAt)
	}
}
func txnRequest(t *testing.T, tn *Tenant, who, label string) *pb.Submission {
	return submission(tn, memberOf(t, tn, who), construction.ID, "build.request.apply", "build.request", who, map[string]any{"label": label})
}
func txnState(tn *Tenant) string {
	var parts []json.RawMessage
	if err := json.Unmarshal([]byte(snapshot(tn)), &parts); err != nil {
		panic(err)
	}
	// Business records, deliveries and effects: a durable refusal is expected to
	// add an immutable answer/audit, but may not change these business resources.
	raw, _ := json.Marshal([]json.RawMessage{parts[0], parts[9], parts[11]})
	return string(raw)
}
func txnUnchanged(t *testing.T, tn *Tenant, before string) {
	t.Helper()
	if txnState(tn) != before || len(tn.sequences.last) != 0 {
		t.Fatal("transaction left records or allocated a number")
	}
}

func TestBusinessTxnConditionalSubjectAndDynamicTargets(t *testing.T) {
	tn := txnTenant(t)
	directory := t.TempDir()
	journal, err := OpenFileJournal(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { journal.Close() }()
	if _, err := journal.Entries(context.Background(), tn.ID, 0); err != nil {
		t.Fatal(err)
	}
	var entries []Entry
	tn.AcceptResult = func(entry Entry, key, hash string) ([]byte, error) {
		raw, err := journal.AppendAccepted(context.Background(), tn.ID, entry, key, hash)
		if err == nil {
			entries = append(entries, entry)
		}
		return raw, err
	}
	access := []construction.Access{{Role: "operator", Read: "all", Create: true, Edit: true}, {Role: "limited", Read: "all"}}
	skip := platform.ExprLiteralValue("boolean", false)
	publishObject(t, tn, "builder", "BUCKET", map[string]any{
		"name": "bucket", "title": "Bucket", "plural": "Buckets",
		"fields": []construction.Field{{Name: "assigned", Title: "Assigned", Type: "boolean"}}, "access": access,
		"createTransaction": &platform.BusinessTxn{Subject: "build.bucket", Steps: []platform.TxnStep{{Name: "optional", Kind: "sequence", When: &skip, Sequence: &platform.Sequence{Name: "unused", Pattern: "{n}"}}}},
	}, txnAt)
	apply := platform.ExprReference("boolean", "input.apply")
	move := &platform.BusinessTxn{Subject: "build.resource", Steps: []platform.TxnStep{
		{Name: "ticket", Kind: "sequence", When: &apply, Sequence: &platform.Sequence{Name: "moves", Pattern: "M-{n}"}},
		{Name: "release", Kind: "update", When: &apply, Target: platform.TxnRef{Type: "build.bucket", Via: "bucket"}, Changes: []platform.TxnSet{{Field: "assigned", From: "=false"}}},
		{Name: "claim", Kind: "update", When: &apply, Target: platform.TxnRef{Type: "build.bucket", ID: "input.next"}, Changes: []platform.TxnSet{{Field: "assigned", From: "=true"}}},
		{Name: "subject", Kind: "update", When: &apply, Target: platform.TxnRef{Type: "build.resource", ID: "$subject"}, Changes: []platform.TxnSet{{Field: "bucket", From: "next"}, {Field: "ticket", From: "step.ticket"}}},
	}}
	first, second := platform.ExprReference("boolean", "input.first"), platform.ExprReference("boolean", "input.second")
	rename := &platform.BusinessTxn{Subject: "build.resource", Steps: []platform.TxnStep{
		{Name: "first", Kind: "update", When: &first, Target: platform.TxnRef{Type: "build.resource", ID: "$subject"}, Changes: []platform.TxnSet{{Field: "label", From: "=First"}}},
		{Name: "second", Kind: "update", When: &second, Target: platform.TxnRef{Type: "build.resource", ID: "$subject"}, Changes: []platform.TxnSet{{Field: "label", From: "=Second"}}},
	}}
	access[1].Create, access[1].Edit = true, true
	publishObject(t, tn, "builder", "RESOURCE", map[string]any{
		"name": "resource", "title": "Resource", "plural": "Resources",
		"fields": []construction.Field{{Name: "bucket", Title: "Bucket", Type: "reference", Ref: "build.bucket", Required: true}, {Name: "ticket", Title: "Ticket", Type: "text"}, {Name: "label", Title: "Label", Type: "text"}},
		"states": []construction.State{{Name: "ready", Title: "Ready"}}, "access": access,
		"actions": []construction.Action{
			{Name: "move", Title: "Move", From: []string{"ready"}, Roles: []string{"operator", "limited"}, Inputs: []construction.Input{{Name: "next", Title: "Next bucket", Type: "reference", Ref: "build.bucket"}, {Name: "apply", Title: "Apply", Type: "boolean"}}, Transaction: move},
			{Name: "rename", Title: "Rename", From: []string{"ready"}, Roles: []string{"operator"}, Inputs: []construction.Input{{Name: "first", Title: "First", Type: "boolean"}, {Name: "second", Title: "Second", Type: "boolean"}}, Transaction: rename},
		},
	}, txnAt)
	for _, id := range []string{"A", "B"} {
		decide(t, tn, "operator", construction.ID, "build.bucket.create", "build.bucket", id, map[string]any{"assigned": id == "A"}, txnAt)
	}
	decide(t, tn, "operator", construction.ID, "build.resource.create", "build.resource", "resource", map[string]any{"bucket": "A", "label": "Original"}, txnAt)
	read := func(typ, id string) map[string]any {
		t.Helper()
		view, err := tn.RecordOf(memberOf(t, tn, "operator"), typ, id, txnAt)
		if err != nil {
			t.Fatal(err)
		}
		var values map[string]any
		if err := json.Unmarshal(platform.Raw(view.Record), &values); err != nil {
			t.Fatal(err)
		}
		return values
	}
	beforeSubject, beforeA, beforeB := read("build.resource", "resource"), read("build.bucket", "A"), read("build.bucket", "B")
	if beforeA["revision"] != float64(1) || beforeB["revision"] != float64(1) {
		t.Fatal("a creation without a subject field step lost its original revision")
	}
	noop := decide(t, tn, "operator", construction.ID, "build.resource.move", "build.resource", "resource", map[string]any{"next": "B", "apply": false}, txnAt)
	current := read("build.resource", "resource")
	if current["bucket"] != "A" || current["label"] != "Original" || current["revision"] != beforeSubject["revision"].(float64)+1 || uint32(current["revision"].(float64)) != noop.GetRevision() {
		t.Fatalf("skipped subject fields lost their values or accepted revision: %v", current)
	}
	if !reflect.DeepEqual(beforeA, read("build.bucket", "A")) || !reflect.DeepEqual(beforeB, read("build.bucket", "B")) || len(tn.sequences.last) != 0 {
		t.Fatal("a false condition changed a target or allocated a number")
	}
	request := submission(tn, memberOf(t, tn, "operator"), construction.ID, "build.resource.move", "build.resource", "resource", map[string]any{"next": "B", "apply": true})
	accepted, failure := tn.Submit(memberOf(t, tn, "operator"), request, txnAt)
	if failure != nil {
		t.Fatalf("distinct dynamic targets were refused: %v", failure)
	}
	if read("build.bucket", "A")["assigned"] != false || read("build.bucket", "B")["assigned"] != true || read("build.resource", "resource")["bucket"] != "B" || read("build.resource", "resource")["ticket"] != "M-1" {
		t.Fatal("the move did not update its two original targets and subject together")
	}
	state, counters := txnState(tn), tn.sequences.clone()
	if reason := refuse(t, tn, "operator", construction.ID, "build.resource.move", "build.resource", "resource", map[string]any{"next": "B", "apply": true}, txnAt); !strings.Contains(reason, "more than once") {
		t.Fatalf("aliased dynamic targets were not refused: %s", reason)
	}
	if txnState(tn) != state || !reflect.DeepEqual(counters, tn.sequences.last) {
		t.Fatal("a target collision left its earlier row change or allocated a number")
	}
	if reason := refuse(t, tn, "limited", construction.ID, "build.resource.move", "build.resource", "resource", map[string]any{"next": "A", "apply": true}, txnAt); !strings.Contains(reason, "POLICY_DENIED") {
		t.Fatalf("the original target permission was bypassed: %s", reason)
	}
	if txnState(tn) != state || !reflect.DeepEqual(counters, tn.sequences.last) {
		t.Fatal("a denied step left business changes or allocated a number")
	}
	decide(t, tn, "operator", construction.ID, "build.resource.rename", "build.resource", "resource", map[string]any{"first": true, "second": false}, txnAt)
	if read("build.resource", "resource")["label"] != "First" {
		t.Fatal("a conditional subject branch did not apply")
	}
	state = txnState(tn)
	if reason := refuse(t, tn, "operator", construction.ID, "build.resource.rename", "build.resource", "resource", map[string]any{"first": true, "second": true}, txnAt); !strings.Contains(reason, "more than once") {
		t.Fatalf("overlapping subject writes were not refused: %s", reason)
	}
	if txnState(tn) != state {
		t.Fatal("overlapping branches retained their first subject write")
	}
	journal.Close()
	journal, err = OpenFileJournal(directory)
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := journal.Entries(context.Background(), tn.ID, 0)
	if err != nil || len(persisted) != len(entries) || !bytes.Equal(persisted[len(persisted)-1].Body, entries[len(entries)-1].Body) {
		t.Fatalf("reopening changed the accepted records: %v", err)
	}
	tn = txnTenant(t)
	if err := tn.Replay(persisted); err != nil {
		t.Fatal(err)
	}
	if txnState(tn) != state {
		t.Fatal("conditional decisions did not recover their accepted business state")
	}
	repeated, failure := tn.Submit(memberOf(t, tn, "operator"), request, txnAt)
	if failure != nil || !proto.Equal(accepted, repeated) || txnState(tn) != state {
		t.Fatal("retry after recovery executed a move again or changed its answer")
	}
}

// All five proofs and three assumptions belong to this one host scenario.
func TestBusinessTxnMinimalSlice(t *testing.T) {
	if dir := os.Getenv("PLATFORM_TXN_CRASH_DIR"); dir != "" {
		j, err := OpenFileJournal(dir)
		if err != nil {
			t.Fatal(err)
		}
		tn := txnTenant(t)
		if _, err := j.Entries(context.Background(), tn.ID, 0); err != nil {
			t.Fatal(err)
		}
		tn.AcceptResult = func(e Entry, key, hash string) ([]byte, error) {
			raw, err := j.AppendAccepted(context.Background(), tn.ID, e, key, hash)
			if err == nil {
				batch, answer, batchErr := resultformat.DecodeBatch(raw)
				if batchErr == nil && batch.Kind == "record-batch" && answer.GetSubmission().GetSchema().GetName() == "build.request.apply" {
					if err := os.WriteFile(filepath.Join(dir, "expected-result"), raw, 0600); err != nil {
						t.Fatal(err)
					}
					os.Exit(86) // fsynced result, before authoritative promotion
				}
			}
			return raw, err
		}
		txnSetup(t, tn)
		request := txnRequest(t, tn, "operator", "crash input")
		request.IdempotencyKey = "crash"
		if _, err := tn.Submit(memberOf(t, tn, "operator"), request, txnAt); err != nil {
			t.Fatal(err)
		}
		t.Fatal("crash point not reached")
	}
	j, err := OpenFileJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	tn := txnTenant(t)
	if _, err := j.Entries(context.Background(), tn.ID, 0); err != nil {
		t.Fatal(err)
	}
	tn.AcceptResult = func(e Entry, key, hash string) ([]byte, error) {
		return j.AppendAccepted(context.Background(), tn.ID, e, key, hash)
	}
	txnSetup(t, tn)
	before := txnState(tn)
	bad := txnRequest(t, tn, "operator", "")
	original := proto.Clone(bad).(*pb.Submission)
	_, refusal := tn.Submit(memberOf(t, tn, "operator"), bad, txnAt)
	if refusal == nil {
		t.Fatal("third step required field accepted")
	}
	txnUnchanged(t, tn, before)
	if !proto.Equal(bad, original) {
		t.Fatal("refusal consumed or rewrote input")
	}
	_, again := tn.Submit(memberOf(t, tn, "operator"), bad, txnAt.Add(time.Hour))
	if again == nil || *again != *refusal {
		t.Fatal("refused retry changed answer")
	}
	t.Logf("proof 2: third step refused (%s); zero row/sequence changes, input unconsumed", refusal.Code)
	denied := txnRequest(t, tn, "limited", "private parent")
	_, deny := tn.Submit(memberOf(t, tn, "limited"), denied, txnAt)
	if deny == nil || deny.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
		if deny == nil {
			t.Fatal("step authorization not enforced: transaction was accepted")
		}
		t.Fatalf("step authorization not enforced: %s: %s", deny.Code, deny.Message)
	}
	txnUnchanged(t, tn, before)
	draft := tn.newStagedDecision()
	auto := platform.NewCaller(draft, memberOf(t, tn, "limited"), construction.ID, false, true)
	if _, err := draft.Decide(auto, tn.app(construction.ID), txnRequest(t, tn, "limited", "automation"), txnAt); err == nil || err.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
		t.Fatal("automation bypassed applicant step authorization")
	}
	txnUnchanged(t, tn, before)
	t.Log("authorization: earlier-created header readable in private context; line create role denial aborts all steps")
	success := txnRequest(t, tn, "operator", "document input")
	position := j.Position(tn.ID)
	originalAccept := tn.AcceptResult
	tn.AcceptResult = func(e Entry, key, hash string) ([]byte, error) {
		var kind struct {
			Kind string `json:"kind"`
		}
		_ = json.Unmarshal(e.Body, &kind)
		if kind.Kind == "record-batch" {
			return nil, errors.New("injected append failure")
		}
		return originalAccept(e, key, hash)
	}
	if _, err := tn.Submit(memberOf(t, tn, "operator"), success, txnAt); err == nil {
		t.Fatal("append failure accepted")
	}
	txnUnchanged(t, tn, before)
	if j.Position(tn.ID) != position {
		t.Fatal("failed append advanced journal")
	}
	tn.AcceptResult = originalAccept
	receipt, errK := tn.Submit(memberOf(t, tn, "operator"), success, txnAt)
	if errK != nil {
		t.Fatal(errK)
	}
	entries, err := j.Entries(context.Background(), tn.ID, position)
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected one acceptance: %v %d", err, len(entries))
	}
	batch, answer, err := resultformat.DecodeBatch(entries[0].Body)
	if err != nil {
		t.Fatal(err)
	}
	types := map[string]bool{}
	for _, row := range batch.Rows {
		types[row.Type] = true
		if len(row.History) != 1 || row.History[0].Change != receipt.ChangeId {
			t.Fatal("row not attached to the same decision")
		}
		if row.Type == "build.request" && (row.Before == "" || row.HistoryBase != 1) {
			t.Fatal("updated subject lost predecessor")
		}
	}
	if len(batch.Rows) != 6 || len(types) != 5 || len(batch.Decisions) != 1 || len(batch.Sequences) != 1 || len(batch.Intents) != 2 || len(batch.Observations) != 0 || !proto.Equal(receipt, answer) {
		t.Fatalf("unexpected batch: rows=%d types=%d decisions=%d sequences=%d", len(batch.Rows), len(types), len(batch.Decisions), len(batch.Sequences))
	}
	for _, row := range batch.Rows {
		var value map[string]any
		_ = json.Unmarshal(row.Value, &value)
		if row.Type == "build.header" && value["number"] != "D-00001" {
			t.Fatal("failed transaction occupied number")
		}
		if row.Type == "build.balance" && string(platform.Raw(value["quantity"])) != `0.3` {
			t.Fatalf("posting lost exact decimal arithmetic: %s", row.Value)
		}
		if row.Type == "build.detail" && strings.HasSuffix(row.ID, "-first") && (value["before"] != float64(0) || value["after"] != float64(1)) {
			t.Fatalf("pure calls did not distinguish initial and preceding state: %s", row.Value)
		}
		if row.Type == "build.request" && (value["state"] != "posted" || value["consumed"] != true) {
			t.Fatalf("subject state did not join the document acceptance: %s", row.Value)
		}
		if row.Type == work.TaskType && (value["title"] != "Review received goods" || len(value["candidates"].([]any)) != 1) {
			t.Fatalf("owned task intent was not retained: %s", row.Value)
		}
	}
	for _, intent := range batch.Intents {
		if intent.Endpoint == construction.BooksEndpoint {
			var journal platform.Journal
			if json.Unmarshal([]byte(intent.Body), &journal) != nil || len(journal.Lines) != 2 || journal.Lines[0].Debit != 0.3 || journal.Lines[1].Credit != 0.3 {
				t.Fatalf("journal intent lost its balanced lines: %s", intent.Body)
			}
		}
	}
	if entries, _, _ := platform.Find[platform.Journal](tn.automation(core.ID, false), platform.Query{}); len(entries) != 0 {
		t.Fatal("the business transaction wrote into the books' independent authority")
	}
	t.Logf("complete document: header, two lines, subject state, exact balance, sequence, pure reads, effect, work and journal intent; %d / %d bytes", len(entries[0].Body), acceptance.MaxResultBytes)
	retry, retryErr := tn.Submit(memberOf(t, tn, "operator"), success, txnAt.Add(time.Hour))
	if retryErr != nil || !proto.Equal(retry, receipt) || j.Position(tn.ID) != position+1 {
		t.Fatal("same input did not return original receipt")
	}
	conflict := proto.Clone(success).(*pb.Submission)
	conflict.Payload = platform.Raw(map[string]any{"label": "changed"})
	if _, err := tn.Submit(memberOf(t, tn, "operator"), conflict, txnAt); err == nil || err.Code != pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT {
		t.Fatal("same key accepted different content")
	}
	t.Log("proof 3: same-key identical reply; different payload IDEMPOTENCY_CONFLICT")
	// Restore the prefix, then tamper a different object type's predecessor.
	all, _ := j.Entries(context.Background(), tn.ID, 0)
	predecessor := txnTenant(t)
	if err := predecessor.Replay(all[:len(all)-1]); err != nil {
		t.Fatal(err)
	}
	prefix := txnState(predecessor)
	for _, typ := range []string{"build.detail", "build.request"} {
		tampered := batch
		tampered.Rows = append([]resultformat.BatchRow(nil), batch.Rows...)
		for i := range tampered.Rows {
			if tampered.Rows[i].Type == typ {
				tampered.Rows[i].Before = "not-the-predecessor"
				break
			}
		}
		tampered.Digest, _ = resultformat.DigestBatch(tampered)
		raw, _ := json.Marshal(tampered)
		if _, err := predecessor.applyAcceptedBatch(predecessor.app(construction.ID).AcceptedLedger(), raw); err == nil {
			t.Fatal("different-type predecessor accepted")
		}
		if txnState(predecessor) != prefix {
			t.Fatal("late predecessor failure partially applied")
		}
	}
	t.Log("predecessor: absent and existing rows across three types validated; changed first/last Before rejects entire result")
	cross := txnDefinition()
	cross.Steps[2].Target.Type = "foreign.header"
	definition := map[string]any{"actions": []construction.Action{{Name: "apply", Title: "Apply transaction", From: []string{"draft"}, Inputs: []construction.Input{{Name: "label", Title: "Label", Type: "longtext"}}, Transaction: cross}}}
	if result := refuse(t, tn, "builder", construction.ID, "build.object.edit", construction.ObjectType, "REQUEST", definition, txnAt); !strings.Contains(result, "another application") {
		t.Fatalf("cross authority not explicitly refused: %s", result)
	}
	t.Log("proof 5: cross-application definition explicitly refused before saving")
	for _, kind := range []platform.AssetKind{platform.AssetAction, platform.AssetFunction, platform.AssetCompute} {
		candidate := txnDefinition()
		candidate.Steps[3].Call.Ref = platform.AssetRef{App: construction.ID, Kind: kind, Name: "headers"}
		definition := map[string]any{"actions": []construction.Action{{Name: "apply", Title: "Apply transaction", From: []string{"draft"}, Inputs: []construction.Input{{Name: "label", Title: "Label", Type: "longtext"}}, Transaction: candidate}}}
		decide(t, tn, "builder", construction.ID, "build.object.edit", construction.ObjectType, "REQUEST", definition, txnAt)
		if result := refuse(t, tn, "builder", construction.ID, construction.SchemaPublish, construction.ObjectType, "REQUEST", map[string]any{}, txnAt); result == "ok" {
			t.Fatalf("an authoritative, effectful or asynchronous %s capability was accepted as a transaction call", kind)
		}
	}
	// The shared rule engine refuses an oversized operand before it can inflate the
	// accepted result. No truncation, number allocation or partial application.
	decide(t, tn, "operator", construction.ID, "build.request.create", "build.request", "large", map[string]any{"note": "unconsumed oversized input"}, txnAt)
	large := txnRequest(t, tn, "operator", strings.Repeat("x", 200000))
	large.Target.Id = "large"
	state := txnState(tn)
	counters := tn.sequences.clone()
	_, over := tn.Submit(memberOf(t, tn, "operator"), large, txnAt)
	if over == nil || !strings.Contains(over.Message, "4096") {
		t.Fatalf("budget overflow did not identify its limit: %v", over)
	}
	if txnState(tn) != state || !reflect.DeepEqual(counters, tn.sequences.last) {
		t.Fatal("oversized transaction partially committed")
	}
	t.Logf("budget overflow explicitly refused: %s", over.Message)
	// A real subprocess terminates after fsync and before applying the result.
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestBusinessTxnMinimalSlice$")
	cmd.Env = append(os.Environ(), "PLATFORM_TXN_CRASH_DIR="+dir)
	output, runErr := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(runErr, &exit) || exit.ExitCode() != 86 {
		t.Fatalf("crash subprocess: %v %s", runErr, output)
	}
	reopened, err := OpenFileJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	persisted, err := reopened.Entries(context.Background(), tn.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := os.ReadFile(filepath.Join(dir, "expected-result"))
	if err != nil {
		t.Fatal(err)
	}
	if len(persisted) == 0 || !bytes.Equal(frozen, persisted[len(persisted)-1].Body) {
		t.Fatal("reopening changed accepted bytes")
	}
	restarted := txnTenant(t)
	restarted.Outbound = func(*http.Request, bool) (*http.Response, error) {
		t.Fatal("recovery contacted an external service")
		return nil, io.EOF
	}
	if err := restarted.Replay(persisted); err != nil {
		t.Fatal(err)
	}
	saved, savedReply, err := resultformat.DecodeBatch(frozen)
	if err != nil {
		t.Fatal(err)
	}
	// Recovery must not consult current caller grants or execute transaction rules.
	restarted.app(construction.ID).Manifest().Actions.Disable("request")
	restored, restoreErr := restarted.Submit(memberOf(t, restarted, "operator"), savedReply.Submission, txnAt.Add(time.Hour))
	if restoreErr != nil || !proto.Equal(restored, savedReply) {
		t.Fatal("recovery reran current authorization")
	}
	for _, row := range saved.Rows {
		r := restarted.records.types[row.Type].rows.Get(row.ID)
		image, err := r.Image()
		if err != nil || !bytes.Equal(image, row.Value) {
			t.Fatalf("recovered image differs for %s: %v", row.Type, err)
		}
	}
	t.Logf("proof 4: process exit 86 after durable append; reopened %d bytes identical, all rows restored without rules/reauthorization", len(frozen))
}

func TestBusinessTxnMigratedNumberRangeContinuesExistingDocuments(t *testing.T) {
	tn := txnTenant(t)
	publishObject(t, tn, "builder", "NUMBERED", map[string]any{"name": "numbered", "title": "Numbered document", "fields": []construction.Field{{Name: "number", Title: "Number", Type: "text", Required: true}, {Name: "label", Title: "Label", Type: "text", Required: true}}, "access": []construction.Access{{Role: "operator", Read: "all", Create: true}}}, txnAt)
	for id, number := range map[string]string{"earlier": "GR2026-0007", "last": "GR2026-0042", "oldyear": "GR2025-0999"} {
		decide(t, tn, "operator", construction.ID, "build.numbered.create", "build.numbered", id, map[string]any{"number": number, "label": "Existing document"}, txnAt)
	}
	decide(t, tn, "builder", construction.ID, "build.object.edit", construction.ObjectType, "NUMBERED", map[string]any{"numbering": &construction.Numbering{Field: "number", Prefix: "GR", Yearly: true, Width: 4}}, txnAt)
	decide(t, tn, "builder", construction.ID, construction.SchemaPublish, construction.ObjectType, "NUMBERED", map[string]any{}, txnAt)
	if refusal := refuse(t, tn, "operator", construction.ID, "build.numbered.create", "build.numbered", "invalid", map[string]any{}, txnAt); refusal == "ok" {
		t.Fatal("a rejected migrated document consumed the next number")
	}
	decide(t, tn, "operator", construction.ID, "build.numbered.create", "build.numbered", "next", map[string]any{"label": "Next document"}, txnAt)
	got, refusal := tn.RecordOf(memberOf(t, tn, "operator"), "build.numbered", "next", txnAt)
	if refusal != nil || !bytes.Contains(platform.Raw(got.Record), []byte(`"number":"GR2026-0043"`)) {
		t.Fatalf("migrating restarted or reused the existing number range: %s %v", platform.Raw(got.Record), refusal)
	}
	decide(t, tn, "operator", construction.ID, "build.numbered.create", "build.numbered", "following", map[string]any{"label": "Following document"}, txnAt)
	got, refusal = tn.RecordOf(memberOf(t, tn, "operator"), "build.numbered", "following", txnAt)
	if refusal != nil || !bytes.Contains(platform.Raw(got.Record), []byte(`"number":"GR2026-0044"`)) {
		t.Fatalf("the migrated named sequence did not continue: %s %v", platform.Raw(got.Record), refusal)
	}
}

func TestBusinessTxnPostingRefusalsLeaveNoPartialState(t *testing.T) {
	tn := builderTenant(t, "posting-refusals")
	for _, item := range []struct{ name, kind string }{{"whole", "integer"}, {"cash", "money"}} {
		publishObject(t, tn, "builder", strings.ToUpper(item.name), map[string]any{"name": item.name, "title": item.name, "fields": []construction.Field{{Name: "item", Title: "Item", Type: "text"}, {Name: "amount", Title: "Amount", Type: item.kind}}, "access": []construction.Access{{Role: "user", Read: "all", Create: true, Edit: true}}}, txnAt)
	}
	decide(t, tn, "user", construction.ID, "build.whole.create", "build.whole", build.BalanceID("build.whole", []string{"goods"}), map[string]any{"item": "goods", "amount": json.Number("9223372036854775807")}, txnAt)
	decide(t, tn, "user", construction.ID, "build.cash.create", "build.cash", build.BalanceID("build.cash", []string{"goods"}), map[string]any{"item": "goods", "amount": platform.Money{Amount: 100, Currency: "USD"}}, txnAt)
	action := func(name, object string, amount platform.Expr) construction.Action {
		return construction.Action{Name: name, Title: name, From: []string{"draft"}, Roles: []string{"user"}, Transaction: &platform.BusinessTxn{Subject: "build.posting", Steps: []platform.TxnStep{
			{Name: "mark", Kind: "update", Target: platform.TxnRef{Type: "build.posting", ID: "$subject"}, Changes: []platform.TxnSet{{Field: "consumed", From: "=true"}}},
			{Name: "post", Kind: "post", Target: platform.TxnRef{Type: object}, Post: &platform.TxnPost{Match: []platform.TxnSet{{Field: "item", From: "=goods"}}, Field: "amount", Amount: amount}},
		}}}
	}
	actions := []construction.Action{action("overflow", "build.whole", platform.ExprLiteralValue("integer", json.Number("1"))), action("currency", "build.cash", platform.ExprLiteralValue("money", platform.Money{Amount: 1, Currency: "EUR"}))}
	publishObject(t, tn, "builder", "POSTING", map[string]any{"name": "posting", "title": "Posting", "fields": []construction.Field{{Name: "consumed", Title: "Consumed", Type: "boolean"}}, "states": []construction.State{{Name: "draft", Title: "Draft"}}, "access": []construction.Access{{Role: "user", Read: "all", Create: true, Edit: true}}, "actions": actions}, txnAt)
	decide(t, tn, "user", construction.ID, "build.posting.create", "build.posting", "P1", map[string]any{}, txnAt)
	for _, name := range []string{"overflow", "currency"} {
		t.Run(name, func(t *testing.T) {
			before := txnState(tn)
			if result := refuse(t, tn, "user", construction.ID, "build.posting."+name, "build.posting", "P1", map[string]any{}, txnAt); result == "ok" {
				t.Fatalf("invalid %s posting was accepted", name)
			}
			if txnState(tn) != before {
				t.Fatal("a failed posting left its earlier subject write or balance change")
			}
		})
	}
	fraction := action("fraction", "build.whole", platform.ExprLiteralValue("decimal", "0.1"))
	if result := refuse(t, tn, "builder", construction.ID, "build.object.edit", construction.ObjectType, "POSTING", map[string]any{"actions": []construction.Action{fraction}}, txnAt); result == "ok" {
		if published := refuse(t, tn, "builder", construction.ID, construction.SchemaPublish, construction.ObjectType, "POSTING", map[string]any{}, txnAt); published == "ok" {
			t.Fatal("fractional posting into an integer balance was published")
		}
	}
}
