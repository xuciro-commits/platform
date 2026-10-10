package platformserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

func TestAcceptedRefusalIsDurableAndImmutableOnRetry(t *testing.T) {
	live := stockTenant(t)
	member, _ := live.Member("ana")
	at := time.Date(2026, 9, 28, 16, 0, 0, 0, time.UTC)
	submission := &pb.Submission{TenantId: live.ID, PrincipalId: member.ID, Authority: "stock",
		IdempotencyKey: "bad-create", Target: &pb.EntityRef{Type: "stock.item", Id: "I1"},
		Schema:  &pb.SchemaRef{Name: "stock.item.create", Version: 1},
		Payload: []byte(`{"name":"Bolt","qty":"wrong","line":"L1"}`)}
	var entries []Entry
	fail := true
	live.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
		if fail {
			return nil, errors.New("append refused")
		}
		entries = append(entries, e)
		return e.Body, nil
	}
	if _, err := live.Submit(member, submission, at); err == nil || len(entries) != 0 ||
		len(live.committed.refusals) != 0 {
		t.Fatalf("uncommitted refusal became visible: %v, %+v", err, entries)
	}
	fail = false
	_, first := live.Submit(member, submission, at)
	if first == nil || len(entries) != 1 {
		t.Fatalf("refusal not committed: %v, %+v", first, entries)
	}
	saved, sub, err := decodeRefusedResult(entries[0].Body)
	if err != nil || saved.Error != *first || sub.GetIdempotencyKey() != submission.IdempotencyKey {
		t.Fatalf("refusal bytes differ from the answer: %+v, %v", saved, err)
	}
	if _, second := live.Submit(member, submission, at.Add(time.Hour)); second == nil ||
		*second != *first || len(entries) != 1 {
		t.Fatalf("retry did not return the original refusal: %v, entries=%d", second, len(entries))
	}
	conflicting := &pb.Submission{TenantId: live.ID, PrincipalId: member.ID, Authority: "stock",
		IdempotencyKey: submission.IdempotencyKey, Target: submission.Target, Schema: submission.Schema,
		Payload: []byte(`{"name":"Other","line":"L1"}`)}
	if _, refused := live.Submit(member, conflicting, at); refused == nil ||
		refused.Code != pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT || len(entries) != 1 {
		t.Fatalf("reused refusal key accepted another input: %v", refused)
	}
	if live.records.types["stock.item"].rows["I1"] != nil {
		t.Fatal("refusal modified the record store")
	}
	CheckReplay(t, live, entries, func() *Tenant { return stockTenant(t) })

	t.Run("field issues survive retry and replay", func(t *testing.T) {
		tenant := stockTenant(t)
		member, _ := tenant.Member("ana")
		app := tenant.app("stock").(*stock)
		declared, _ := app.ledger.Catalog.Action("stock.item.create")
		declared.Payload = append([]platform.Field(nil), declared.Payload...)
		for i := range declared.Payload {
			if declared.Payload[i].Name == "qty" {
				min := 0.0
				declared.Payload[i].Constraints = &platform.InputConstraints{Min: &min}
			}
		}
		app.ledger.Catalog.Add(declared)
		var entries []Entry
		tenant.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
		request := proto.Clone(submission).(*pb.Submission)
		request.IdempotencyKey = "field-refusal"
		request.Payload = []byte(`{"name":"Bolt","qty":-1,"line":"L1"}`)
		_, refusal, issues := tenant.submitDiagnosed(member, request, at)
		if refusal == nil || len(issues) != 1 || issues[0].Path[0] != "qty" || issues[0].Code != "min" {
			t.Fatalf("missing input issue: %v %+v", refusal, issues)
		}
		saved, _, decodeErr := decodeRefusedResult(entries[0].Body)
		if decodeErr != nil || saved.Version != 2 || len(saved.Issues) != 1 {
			t.Fatalf("missing version 2 refusal: %+v %v", saved, decodeErr)
		}
		app.ledger.Catalog.Add(platform.Action{Schema: declared.Schema, Payload: []platform.Field{}})
		_, again, repeated := tenant.submitDiagnosed(member, request, at.Add(time.Hour))
		if again == nil || *again != *refusal || !reflect.DeepEqual(repeated, issues) || len(entries) != 1 {
			t.Fatalf("retry recast diagnostics: %v %+v", again, repeated)
		}
		recovered := stockTenant(t)
		if err := recovered.Replay(entries); err != nil {
			t.Fatal(err)
		}
		_, _, restored := recovered.submitDiagnosed(member, request, at)
		if !reflect.DeepEqual(restored, issues) {
			t.Fatalf("replay recast diagnostics: %+v", restored)
		}
		legacy, encodeErr := encodeRefusedResult(request, at, refusal)
		old, _, decodeErr := decodeRefusedResult(legacy)
		if encodeErr != nil || decodeErr != nil || old.Version != 1 || len(old.Issues) != 0 {
			t.Fatalf("version 1 changed: %v %v", encodeErr, decodeErr)
		}
		saved.Issues[0].Message = "tampered issue"
		damaged, _ := json.Marshal(saved)
		if _, _, err := decodeRefusedResult(damaged); err == nil {
			t.Fatal("issue tampering passed the digest")
		}
	})

	damaged := entries[0]
	var decoded refusedResult
	if err := json.Unmarshal(damaged.Body, &decoded); err != nil {
		t.Fatal(err)
	}
	decoded.Error.Message = "tampered"
	damaged.Body, _ = json.Marshal(decoded)
	if err := stockTenant(t).Replay([]Entry{damaged}); err == nil {
		t.Fatal("replay accepted a tampered refusal")
	}
	wrongClock := entries[0]
	wrongClock.At = at.Add(time.Hour)
	if err := stockTenant(t).Replay([]Entry{wrongClock}); err == nil {
		t.Fatal("replay accepted a refusal with the wrong journal clock")
	}
	snapshot, _, err := live.Snapshot(func() int64 { return 1 })
	if err != nil {
		t.Fatal(err)
	}
	var state tenantState
	if err := json.Unmarshal(snapshot, &state); err != nil {
		t.Fatal(err)
	}
	key := "stock/" + submission.IdempotencyKey
	refusal := state.Refusals[key]
	refusal.Error.Message = "tampered snapshot"
	state.Refusals[key] = refusal
	corrupt, _ := json.Marshal(state)
	if err := stockTenant(t).Restore(corrupt); err == nil {
		t.Fatal("restore accepted a corrupt cached refusal")
	}
}

func TestJournalAcceptedRefusalAfterCrashAndRestart(t *testing.T) {
	url := os.Getenv("PLATFORM_TEST_DATABASE")
	if url == "" {
		t.Skip("PLATFORM_TEST_DATABASE not set")
	}
	ctx := context.Background()
	j, err := OpenJournal(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	id := fmt.Sprintf("refused-%d", time.Now().UnixNano())
	defer func() { _, _ = j.Pool().Exec(ctx, `delete from journal where tenant=$1`, id) }()
	compose := func() *Tenant {
		seat := Seat{Subjects: []string{"ana"}, Member: platform.Member{ID: "ana", Roles: map[string]string{"stock": "clerk"}}}
		tn, err := NewTenant(id, NewConsole(id, seat), newStock(id))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	if _, err := j.Entries(ctx, id, 0); err != nil {
		t.Fatal(err)
	}
	live := compose()
	m, _ := live.Member("ana")
	at := time.Date(2026, 9, 28, 17, 0, 0, 0, time.UTC)
	bad := &pb.Submission{TenantId: id, PrincipalId: m.ID, Authority: "stock", IdempotencyKey: "same",
		Target: &pb.EntityRef{Type: "stock.bin", Id: "B1"},
		Schema: &pb.SchemaRef{Name: "stock.bin.create", Version: 1}, Payload: []byte(`{}`)}
	live.AcceptResult = func(e Entry, key, hash string) ([]byte, error) {
		if _, err := j.AppendAccepted(ctx, id, e, key, hash); err != nil {
			return nil, err
		}
		panic("crash after the durable refusal")
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("the injected crash did not interrupt application")
			}
		}()
		live.Submit(m, bad, at)
	}()
	if len(live.committed.refusals) != 0 {
		t.Fatal("unapplied refusal entered the live cache")
	}
	reopened, err := OpenJournal(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	entries, err := reopened.Entries(ctx, id, 0)
	if err != nil || len(entries) != 1 {
		t.Fatalf("durable refusal not recovered: %d entries, %v", len(entries), err)
	}
	recovered := compose()
	if err := recovered.Replay(entries); err != nil {
		t.Fatal(err)
	}
	recovered.AcceptResult = func(e Entry, key, hash string) ([]byte, error) {
		return reopened.AppendAccepted(ctx, id, e, key, hash)
	}
	_, refusal := recovered.Submit(m, bad, at.Add(time.Hour))
	if refusal == nil || reopened.Position(id) != 1 {
		t.Fatalf("retry changed the committed refusal: %v, position=%d", refusal, reopened.Position(id))
	}
	saved, _, err := decodeRefusedResult(entries[0].Body)
	if err != nil || *refusal != saved.Error {
		t.Fatalf("retry lost the original answer: %v, %+v, %v", refusal, saved, err)
	}
	// An accepted input cannot reuse the refusal's key, including after a
	// restart or a stale writer, even though the refusal has no kernel receipt.
	valid := proto.Clone(bad).(*pb.Submission)
	valid.Payload = []byte(`{"code":"A"}`)
	stale := compose()
	draft := stale.newStagedDecision()
	receipt, refused := decideAccepted(stale.app("stock").(platform.ResultApp), draft, m, valid, at)
	if refused != nil {
		t.Fatalf("the alternate submission should be valid: %v", refused)
	}
	accepted, err := draft.result("stock", receipt, at)
	if err != nil {
		t.Fatal(err)
	}
	otherHash, err := submissionHash(valid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.AppendAccepted(ctx, id, Entry{App: "stock", Kind: "accepted-result",
		Body: accepted, Principal: entries[0].Principal, At: at}, "same", otherHash); !errors.Is(err, errAcceptedConflict) {
		t.Fatalf("accepted result reused a refused key: %v", err)
	}
	if _, err := reopened.AppendAccepted(ctx, id, Entry{App: "stock", Kind: "accepted-result",
		Body: entries[0].Body, Principal: entries[0].Principal, At: at}, "same", "another hash"); !errors.Is(err, errAcceptedConflict) {
		t.Fatalf("cross-result idempotency key was not reserved: %v", err)
	}
	if _, err := recovered.Submit(m, valid, at); err == nil ||
		err.Code != pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT {
		t.Fatalf("the refused key admitted a new input: %v", err)
	}
	CheckReplay(t, recovered, entries, compose)
}
