package platformserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"google.golang.org/protobuf/proto"
	"os"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/build"
	"platformserver/apps/work"
	resultformat "platformserver/internal/acceptance/result"
	"platformserver/internal/construction"
	"platformserver/platform"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// accepted_refusal_test.go consolidated into this owner

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
	saved, sub, err := resultformat.DecodeRefusal(entries[0].Body)
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
	if live.records.types["stock.item"].rows.Get("I1") != nil {
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
		saved, _, decodeErr := resultformat.DecodeRefusal(entries[0].Body)
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
		legacy, encodeErr := resultformat.EncodeRefusal(request, at, refusal)
		old, _, decodeErr := resultformat.DecodeRefusal(legacy)
		if encodeErr != nil || decodeErr != nil || old.Version != 1 || len(old.Issues) != 0 {
			t.Fatalf("version 1 changed: %v %v", encodeErr, decodeErr)
		}
		saved.Issues[0].Message = "tampered issue"
		damaged, _ := json.Marshal(saved)
		if _, _, err := resultformat.DecodeRefusal(damaged); err == nil {
			t.Fatal("issue tampering passed the digest")
		}
	})

	damaged := entries[0]
	var decoded resultformat.Refusal
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
	saved, _, err := resultformat.DecodeRefusal(entries[0].Body)
	if err != nil || *refusal != saved.Error {
		t.Fatalf("retry lost the original answer: %v, %+v, %v", refusal, saved, err)
	}
	// An accepted input cannot reuse the refusal's key, including after a
	// restart or a stale writer, even though the refusal has no kernel receipt.
	valid := proto.Clone(bad).(*pb.Submission)
	valid.Payload = []byte(`{"code":"A"}`)
	stale := compose()
	draft := stale.newStagedDecision()
	receipt, refused := decideAccepted(stale.app("stock").(platform.App), draft, m, valid, at)
	if refused != nil {
		t.Fatalf("the alternate submission should be valid: %v", refused)
	}
	accepted, err := draft.result("stock", receipt, at)
	if err != nil {
		t.Fatal(err)
	}
	otherHash, err := resultformat.SubmissionHash(valid)
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

// business_refusal_escalation_test.go consolidated into this owner

func TestPostgresBusinessRefusalEscalationReadsSavedFacts(t *testing.T) {
	dsn := os.Getenv("PLATFORM_TEST_DATABASE")
	if dsn == "" {
		t.Skip("PLATFORM_TEST_DATABASE not set")
	}
	ctx := context.Background()
	j, err := OpenJournal(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	tn := businessRefusalEscalationTenant(t)
	defer forgetJournal(t, j, tn.ID)
	if _, err = j.Entries(ctx, tn.ID, 0); err != nil {
		t.Fatal(err)
	}
	tn.attachJournal(ctx, j)
	businessRefusalOwnerSetup(t, tn)
	setBusinessRefusalEscalation(t, tn, "flag", &platform.RefusalEscalation{Count: 2, WindowSeconds: 60, ReviewerRole: "manager"})
	member := memberOf(t, tn, "operator")
	for i := range 2 {
		if _, refusal := tn.Submit(member, txnRequest(t, tn, "operator", "flag"), txnAt.Add(time.Duration(i)*time.Second)); refusal != nil {
			t.Fatal(refusal)
		}
	}
	if err = tn.migrateRuntimeState(ctx, j); err != nil {
		t.Fatal(err)
	}
	var originalTask string
	check := func(tenant *Tenant) {
		t.Helper()
		tasks, refusal := recordRead(tenant, ctx, func() ([]work.WorkTask, *kernel.Error) {
			facts := storedValues(&tenant.committed, "answer", tenant.committed.answers, "business")
			if len(facts) != 2 {
				t.Fatalf("business selection returned ordinary replies: %d", len(facts))
			}
			rows, _, refusal := platform.Find[work.WorkTask](tenant.automation(work.ID, false), platform.Query{})
			return rows, refusal
		})
		if refusal != nil || len(tasks) != 1 || !slices.Equal(tasks[0].Candidates, []string{"manager"}) {
			t.Fatal("saved refusal facts lost their original Work task", tasks, refusal)
		}
		if originalTask != "" && tasks[0].ID != originalTask {
			t.Fatal("checkpoint changed escalation identity")
		}
		originalTask = tasks[0].ID
	}
	runJob(t, tn, work.ID, "refusals", txnAt.Add(3*time.Second))
	check(tn)
	if _, err = tn.checkpointRuntime(ctx, j, "refusal-history"); err != nil {
		t.Fatal(err)
	}
	restored := businessRefusalEscalationTenant(t)
	d := Deployment{SnapshotEvery: 1}
	if err = d.restoreTenants(ctx, j, "refusal-history", []*Tenant{restored}, map[string]int64{}, map[string]bool{}); err != nil || restored.quarantined() {
		t.Fatal(err, restored.fault.Load())
	}
	runJob(t, restored, work.ID, "refusals", txnAt.Add(4*time.Second))
	check(restored)
}

func businessRefusalEscalationTenant(t *testing.T) *Tenant {
	t.Helper()
	return composeTenant(t, "business-txn", []Seat{seatOf("builder", "build:builder"), seatOf("operator", "build:operator"), seatOf("limited", "build:limited"), seatOf("manager", "build:manager")}, work.New("business-txn"), build.New("business-txn"))
}

func setBusinessRefusalEscalation(t *testing.T, tn *Tenant, level string, policy *platform.RefusalEscalation) {
	t.Helper()
	view, err := tn.RecordOf(memberOf(t, tn, "builder"), construction.ObjectType, "REQUEST", txnAt)
	if err != nil {
		t.Fatal(err)
	}
	var object construction.Object
	if err := json.Unmarshal(platform.Raw(view.Record), &object); err != nil {
		t.Fatal(err)
	}
	for i := range object.Actions[0].Transaction.Steps {
		for j := range object.Actions[0].Transaction.Steps[i].Refusals {
			declaration := &object.Actions[0].Transaction.Steps[i].Refusals[j]
			if declaration.Level == level {
				declaration.Escalation = policy
			}
		}
	}
	decide(t, tn, "builder", construction.ID, construction.ObjectType+".edit", construction.ObjectType, "REQUEST", map[string]any{"actions": object.Actions}, txnAt)
	decide(t, tn, "builder", construction.ID, construction.ObjectType+".publish", construction.ObjectType, "REQUEST", map[string]any{}, txnAt)
}
func TestBusinessRefusalEscalationCannotBeForged(t *testing.T) {
	tn := businessRefusalEscalationTenant(t)
	businessRefusalOwnerSetup(t, tn)
	if got := refuse(t, tn, "operator", work.ID, work.TaskType+".refusal", work.TaskType, "forged", map[string]any{}, txnAt); !strings.Contains(got, "POLICY_DENIED") {
		t.Fatalf("member forged a refusal escalation: %s", got)
	}
}
