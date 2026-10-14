package platformserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/platform"
)

var txnAt = time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)

func txnTenant(t *testing.T) *Tenant {
	return composeTenant(t, "business-txn", []Seat{seatOf("builder", "build:builder"), seatOf("operator", "build:operator"), seatOf("limited", "build:limited")}, build.New("business-txn"))
}
func txnDefinition() *platform.BusinessTxn {
	return &platform.BusinessTxn{Subject: "build.request", Steps: []platform.TxnStep{
		{Name: "number", Kind: "sequence", Sequence: &platform.Sequence{Name: "documents", Pattern: "D-{n:5}"}},
		{Name: "input", Kind: "update", Target: platform.TxnRef{Type: "build.request", ID: "$subject"}, Changes: []platform.TxnSet{{Field: "consumed", From: "=true"}, {Field: "number", From: "step.number"}, {Field: "note", From: "label"}}},
		{Name: "head", Kind: "create", Target: platform.TxnRef{Type: "build.header"}, Changes: []platform.TxnSet{{Field: "number", From: "step.number"}, {Field: "owner", From: "$me"}, {Field: "label", From: "label"}}},
		{Name: "first", Kind: "create", Target: platform.TxnRef{Type: "build.detail"}, Changes: []platform.TxnSet{{Field: "header", From: "step.head"}, {Field: "note", From: "label"}}},
		{Name: "second", Kind: "create", Target: platform.TxnRef{Type: "build.detail"}, Changes: []platform.TxnSet{{Field: "header", From: "step.head"}, {Field: "note", From: "label"}}},
	}}
}
func txnSetup(t *testing.T, tn *Tenant) {
	publishObject(t, tn, "builder", "HEADER", map[string]any{"name": "header", "title": "Header", "plural": "Headers", "fields": []build.Field{{Name: "label", Title: "Label", Type: "longtext", Required: true}, {Name: "number", Title: "Number", Type: "text", Required: true}, {Name: "owner", Title: "Owner", Type: "text", Required: true}}, "scope": build.ObjectScope{Owner: "owner"}, "access": []build.Access{{Role: "operator", Read: "own", Create: true}, {Role: "limited", Read: "own", Create: true}}}, txnAt)
	publishObject(t, tn, "builder", "DETAIL", map[string]any{"name": "detail", "title": "Detail", "plural": "Details", "fields": []build.Field{{Name: "header", Title: "Header", Type: "reference", Ref: "build.header", Required: true}, {Name: "note", Title: "Note", Type: "longtext", Required: true}}, "access": []build.Access{{Role: "operator", Read: "own", Create: true}, {Role: "limited", Read: "own"}}}, txnAt)
	publishObject(t, tn, "builder", "REQUEST", map[string]any{"name": "request", "title": "Request", "plural": "Requests", "fields": []build.Field{{Name: "consumed", Title: "Consumed", Type: "boolean"}, {Name: "number", Title: "Number", Type: "text"}, {Name: "note", Title: "Note", Type: "longtext"}}, "states": []build.State{{Name: "draft", Title: "Draft"}}, "access": []build.Access{{Role: "operator", Read: "own", Create: true, Edit: true}, {Role: "limited", Read: "own", Create: true, Edit: true}}, "actions": []build.Action{{Name: "apply", Title: "Apply transaction", From: []string{"draft"}, Roles: []string{"operator", "limited"}, Inputs: []build.Input{{Name: "label", Title: "Label", Type: "longtext"}}, Transaction: txnDefinition()}}}, txnAt)
	for _, who := range []string{"operator", "limited"} {
		decide(t, tn, who, build.ID, "build.request.create", "build.request", who, map[string]any{"note": "unconsumed input"}, txnAt)
	}
}
func txnRequest(t *testing.T, tn *Tenant, who, label string) *pb.Submission {
	return submission(tn, memberOf(t, tn, who), build.ID, "build.request.apply", "build.request", who, map[string]any{"label": label})
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
				var kind struct {
					Kind string `json:"kind"`
				}
				_ = json.Unmarshal(raw, &kind)
				if kind.Kind == "record-batch" {
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
		t.Fatalf("step authorization not enforced: %v", deny)
	}
	txnUnchanged(t, tn, before)
	draft := tn.newStagedDecision()
	auto := platform.NewCaller(draft, memberOf(t, tn, "limited"), build.ID, false, true)
	if _, err := draft.Decide(auto, tn.app(build.ID), txnRequest(t, tn, "limited", "automation"), txnAt); err == nil || err.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
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
	batch, answer, err := decodeAcceptedBatch(entries[0].Body)
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
	if len(batch.Rows) != 4 || len(types) != 3 || len(batch.Decisions) != 1 || len(batch.Sequences) != 1 || len(batch.Intents) != 0 || len(batch.Observations) != 0 || !proto.Equal(receipt, answer) {
		t.Fatalf("unexpected batch: rows=%d types=%d decisions=%d sequences=%d", len(batch.Rows), len(types), len(batch.Decisions), len(batch.Sequences))
	}
	for _, row := range batch.Rows {
		var value map[string]any
		_ = json.Unmarshal(row.Value, &value)
		if row.Type == "build.header" && value["number"] != "D-00001" {
			t.Fatal("failed transaction occupied number")
		}
	}
	t.Logf("proof 1 / budget: 3 types, 4 rows, 1 decision, 1 sequence; %d / %d bytes", len(entries[0].Body), maxAcceptedResultBytes)
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
		tampered.Rows = append([]acceptedBatchRow(nil), batch.Rows...)
		for i := range tampered.Rows {
			if tampered.Rows[i].Type == typ {
				tampered.Rows[i].Before = "not-the-predecessor"
				break
			}
		}
		tampered.Digest, _ = digestAcceptedBatch(tampered)
		raw, _ := json.Marshal(tampered)
		if _, err := predecessor.applyAcceptedBatch(predecessor.app(build.ID).AcceptedLedger(), raw); err == nil {
			t.Fatal("different-type predecessor accepted")
		}
		if txnState(predecessor) != prefix {
			t.Fatal("late predecessor failure partially applied")
		}
	}
	t.Log("predecessor: absent and existing rows across three types validated; changed first/last Before rejects entire result")
	cross := txnDefinition()
	cross.Steps[2].Target.Type = "foreign.header"
	definition := map[string]any{"actions": []build.Action{{Name: "apply", Title: "Apply transaction", From: []string{"draft"}, Inputs: []build.Input{{Name: "label", Title: "Label", Type: "longtext"}}, Transaction: cross}}}
	if result := refuse(t, tn, "builder", build.ID, "build.object.edit", build.ObjectType, "REQUEST", definition, txnAt); !strings.Contains(result, "another application") {
		t.Fatalf("cross authority not explicitly refused: %s", result)
	}
	t.Log("proof 5: cross-application definition explicitly refused before saving")
	// ADR-0101 refuses an oversized rule operand before it can inflate the
	// accepted result. No truncation, number allocation or partial application.
	large := txnRequest(t, tn, "operator", strings.Repeat("x", 200000))
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
	if err := restarted.Replay(persisted); err != nil {
		t.Fatal(err)
	}
	saved, savedReply, err := decodeAcceptedBatch(frozen)
	if err != nil {
		t.Fatal(err)
	}
	// Recovery must not consult current caller grants or execute transaction rules.
	restarted.app(build.ID).Manifest().Actions.Disable("request")
	restored, restoreErr := restarted.Submit(memberOf(t, restarted, "operator"), savedReply.Submission, txnAt.Add(time.Hour))
	if restoreErr != nil || !proto.Equal(restored, savedReply) {
		t.Fatal("recovery reran current authorization")
	}
	for _, row := range saved.Rows {
		r := restarted.records.types[row.Type].rows.get(row.ID)
		image, err := r.image()
		if err != nil || !bytes.Equal(image, row.Value) {
			t.Fatalf("recovered image differs for %s: %v", row.Type, err)
		}
	}
	t.Logf("proof 4: process exit 86 after durable append; reopened %d bytes identical, all rows restored without rules/reauthorization", len(frozen))
}
