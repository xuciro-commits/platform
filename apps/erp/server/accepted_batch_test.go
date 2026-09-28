package erp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver"
)

func TestAcceptedPostingBatchCommitsRecordsAndNumberTogether(t *testing.T) {
	b := newBooks(t)
	fail := false
	b.tn.AcceptResult = func(e platformserver.Entry, _, _ string) ([]byte, error) {
		if fail {
			return nil, errors.New("injected append failure")
		}
		b.journal = append(b.journal, e)
		return e.Body, nil
	}
	for _, account := range Chart() {
		if account.ID != "1002" && account.ID != "4001" {
			continue
		}
		b.expect("account", b.do("cy", AccountType+".create", AccountType, account.ID,
			map[string]string{"name": account.Name, "kind": account.Kind}), "ok")
	}
	b.expect("period", b.do("cy", SchemaPeriodOpen, PeriodType, "2026-10", map[string]any{}), "ok")
	b.expect("draft", b.do("ada", EntryType+".create", EntryType, "E-1", map[string]any{
		"journal": "general", "date": "2026-10-02", "reference": "capital",
		"lines": []map[string]any{line("1002", 10000, 0), line("4001", 0, 10000)},
	}), "ok")
	member, _ := b.tn.Member("ada")
	now := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	submission := &pb.Submission{TenantId: "t", PrincipalId: member.ID, Authority: ID, IdempotencyKey: "post-e1",
		Target: &pb.EntityRef{Type: EntryType, Id: "E-1"},
		Schema: &pb.SchemaRef{Name: EntryType + ".post", Version: 1}, Payload: []byte(`{}`)}
	before := len(b.journal)
	fail = true
	if _, refusal := b.tn.Submit(member, submission, now); refusal == nil || len(b.journal) != before {
		t.Fatalf("failed journal append exposed a posting: %v, entries=%d", refusal, len(b.journal))
	}
	if got := b.entry("E-1"); got.State != "draft" || got.Number != "" {
		t.Fatalf("failed append posted an entry or allocated a number: %+v", got)
	}
	fail = false
	posted, refusal := b.tn.Submit(member, submission, now)
	if refusal != nil || posted.GetRevision() != 2 || len(b.journal) != before+1 {
		t.Fatalf("posting did not commit as one batch: %+v, %+v, entries=%d, replay=%v",
			posted, refusal, len(b.journal)-before, build(t).Replay(b.journal))
	}
	var envelope struct {
		Kind      string         `json:"kind"`
		Rows      []any          `json:"rows"`
		Sequences map[string]int `json:"sequences"`
	}
	if err := json.Unmarshal(b.journal[len(b.journal)-1].Body, &envelope); err != nil ||
		envelope.Kind != "record-batch" || len(envelope.Rows) != 3 ||
		envelope.Sequences["erp/entry.general/2026"] != 1 {
		t.Fatalf("posting omitted records or the sequence: %+v, %v", envelope, err)
	}
	if got := b.entry("E-1"); got.State != "posted" || got.Number != "GJ/2026/00001" {
		t.Fatalf("committed posting is incomplete: %+v", got)
	}
	if got, refusal := b.tn.Submit(member, submission, now.Add(time.Hour)); refusal != nil ||
		got.GetChangeId() != posted.GetChangeId() || len(b.journal) != before+1 {
		t.Fatalf("retry did not reuse the receipt: %+v, %v", got, refusal)
	}
	b.expect("reverse", b.do("ada", EntryType+".reverse", EntryType, "E-1",
		map[string]string{"date": "2026-10-03"}), "ok")
	if got := b.entry("E-1-R"); got.Number != "GJ/2026/00002" || got.State != "posted" {
		t.Fatalf("reversal did not allocate the next number: %+v", got)
	}
	if len(b.journal) != before+2 {
		t.Fatalf("reversal did not commit one result: %d entries", len(b.journal)-before)
	}
	platformserver.CheckReplay(t, b.tn, b.journal, func() *platformserver.Tenant { return build(t) })
}

func TestJournalAcceptedPostingBatchCrashAndRestart(t *testing.T) {
	url := os.Getenv("PLATFORM_TEST_DATABASE")
	if url == "" {
		t.Skip("PLATFORM_TEST_DATABASE not set")
	}
	ctx := context.Background()
	j, err := platformserver.OpenJournal(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	id := fmt.Sprintf("posting-%d", time.Now().UnixNano())
	defer func() {
		pool, err := pgxpool.New(ctx, url)
		if err != nil {
			t.Error(err)
			return
		}
		defer pool.Close()
		if _, err := pool.Exec(ctx, `delete from journal where tenant=$1`, id); err != nil {
			t.Error(err)
		}
	}()
	compose := func() *platformserver.Tenant { return buildTenant(t, id) }
	if _, err := j.Entries(ctx, id, 0); err != nil {
		t.Fatal(err)
	}
	live := compose()
	live.Record = func(e platformserver.Entry) {
		if err := j.Append(ctx, id, e); err != nil {
			panic(err)
		}
	}
	crash := false
	live.AcceptResult = func(e platformserver.Entry, key, hash string) ([]byte, error) {
		committed, err := j.AppendAccepted(ctx, id, e, key, hash)
		if err == nil && crash {
			panic("injected crash between commit and application")
		}
		return committed, err
	}
	accountant, _ := live.Member("ada")
	at := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	submit := func(memberID string, key, schema, typ, target, payload string) *pb.ChangeRecord {
		t.Helper()
		member, _ := live.Member(memberID)
		receipt, refused := live.Submit(member, &pb.Submission{TenantId: id, PrincipalId: member.ID,
			Authority: ID, IdempotencyKey: key, Target: &pb.EntityRef{Type: typ, Id: target},
			Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: []byte(payload)}, at)
		if refused != nil {
			t.Fatalf("%s: %v", schema, refused)
		}
		return receipt
	}
	for _, account := range Chart() {
		if account.ID != "1002" && account.ID != "4001" {
			continue
		}
		body, _ := json.Marshal(map[string]string{"name": account.Name, "kind": account.Kind})
		submit("cy", "account-"+account.ID, AccountType+".create", AccountType, account.ID, string(body))
	}
	submit("cy", "period", SchemaPeriodOpen, PeriodType, "2026-10", `{}`)
	submit("ada", "entry", EntryType+".create", EntryType, "E-1",
		`{"journal":"general","date":"2026-10-02","reference":"capital","lines":[{"account":"1002","debit":{"amount":10000},"credit":{"amount":0}},{"account":"4001","debit":{"amount":0},"credit":{"amount":10000}}]}`)
	preCommit := j.Position(id)
	post := &pb.Submission{TenantId: id, PrincipalId: accountant.ID, Authority: ID, IdempotencyKey: "post",
		Target: &pb.EntityRef{Type: EntryType, Id: "E-1"},
		Schema: &pb.SchemaRef{Name: EntryType + ".post", Version: 1}, Payload: []byte(`{}`)}
	crash = true
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("injected crash was swallowed")
			}
		}()
		live.Submit(accountant, post, at)
	}()
	if j.Position(id) != preCommit+1 {
		t.Fatal("posting did not commit one result before crashing")
	}
	reopened, err := platformserver.OpenJournal(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	entries, err := reopened.Entries(ctx, id, 0)
	if err != nil || int64(len(entries)) != preCommit+1 {
		t.Fatalf("the committed posting is not recoverable: %d entries, %v", len(entries), err)
	}
	restored := compose()
	if err := restored.Replay(entries); err != nil {
		t.Fatal(err)
	}
	record, refused := restored.RecordOf(accountant, EntryType, "E-1", at)
	if refused != nil || record.Record.(Entry).State != "posted" ||
		record.Record.(Entry).Number != "GJ/2026/00001" {
		t.Fatalf("the posting was not recovered: %+v, %v", record, refused)
	}
	restored.AcceptResult = func(e platformserver.Entry, key, hash string) ([]byte, error) {
		return reopened.AppendAccepted(ctx, id, e, key, hash)
	}
	if receipt, err := restored.Submit(accountant, post, at.Add(time.Hour)); err != nil ||
		receipt.GetRevision() != 2 || reopened.Position(id) != preCommit+1 {
		t.Fatalf("retry changed the posting or journal: %+v, %v", receipt, err)
	}
	platformserver.CheckReplay(t, restored, entries, compose)
}
