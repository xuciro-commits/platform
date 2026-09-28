package platformserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

func TestJournalAcceptedResultAtomicRetryAndRecovery(t *testing.T) {
	url := os.Getenv("PLATFORM_TEST_DATABASE")
	if url == "" {
		t.Skip("PLATFORM_TEST_DATABASE not set")
	}
	ctx := context.Background()
	j, err := OpenJournal(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("accepted-%d", time.Now().UnixNano())
	defer func() {
		j.pool.Exec(ctx, `delete from journal where tenant=$1`, id)
		j.Close()
	}()
	build := func() *Tenant {
		seat := Seat{Subjects: []string{"ana"}, Member: platform.Member{ID: "ana", Roles: map[string]string{"stock": "clerk"}}}
		tn, err := NewTenant(id, NewConsole(id, seat), newStock(id))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	live := build()
	if entries, err := j.Entries(ctx, id, 0); err != nil || len(entries) != 0 {
		t.Fatalf("new journal: %v %v", entries, err)
	}
	live.AcceptResult = func(e Entry, key, hash string) ([]byte, error) {
		return j.AppendAccepted(ctx, id, e, key, hash)
	}
	m, _ := live.Member("ana")
	now := time.Date(2026, 9, 28, 14, 0, 0, 0, time.UTC)
	s := &pb.Submission{TenantId: id, PrincipalId: m.ID, Authority: "stock", IdempotencyKey: "k1",
		Target: &pb.EntityRef{Type: "stock.bin", Id: "B1"},
		Schema: &pb.SchemaRef{Name: "stock.bin.create", Version: 1}, Payload: []byte(`{"code":"A"}`)}
	if _, err := live.Submit(m, s, now); err != nil {
		t.Fatal(err)
	}
	entries, err := j.Entries(ctx, id, 0)
	if err != nil || len(entries) != 1 || entries[0].Kind != "accepted-result" {
		t.Fatalf("committed journal: %v %v", entries, err)
	}
	result, _, err := decodeAcceptedResult(entries[0].Body)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := j.AppendAccepted(ctx, id, entries[0], "k1", result.RequestHash); err != nil || j.Position(id) != 1 {
		t.Fatalf("duplicate advanced journal: %v", err)
	}
	if _, err := j.AppendAccepted(ctx, id, entries[0], "k1", "another request"); !errors.Is(err, errAcceptedConflict) {
		t.Fatalf("key reused for another request: %v", err)
	}
	reopened, err := OpenJournal(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	persisted, err := reopened.Entries(ctx, id, 0)
	if err != nil || len(persisted) != 1 {
		t.Fatalf("restart did not find result: %v %v", persisted, err)
	}
	CheckReplay(t, live, persisted, build)
}

func TestAcceptedSubmitCommitFailureRetryAndReplay(t *testing.T) {
	live := stockTenant(t)
	member, _ := live.app(PlatformApp).(*Console).Member("ana")
	now := time.Date(2026, 9, 28, 14, 0, 0, 0, time.UTC)
	sub := func(key, verb, body string) *pb.Submission {
		return &pb.Submission{TenantId: live.ID, PrincipalId: member.ID, Authority: "stock",
			IdempotencyKey: key, Target: &pb.EntityRef{Type: "stock.item", Id: "I1"},
			Schema: &pb.SchemaRef{Name: "stock.item." + verb, Version: 1}, Payload: []byte(body)}
	}
	var entries []Entry
	live.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
		if len(entries) == 0 {
			return nil, errors.New("injected append failure")
		}
		entries = append(entries, e)
		return e.Body, nil
	}
	create := sub("k1", "create", `{"name":"Bolt","qty":2,"line":"L1"}`)
	if _, err := live.Submit(member, create, now); err == nil ||
		len(live.app("stock").(*stock).ledger.Changes.Records(live.ID)) != 0 ||
		live.records.types["stock.item"].rows["I1"] != nil {
		t.Fatal("failed append exposed a draft")
	}
	entries = append(entries, Entry{}) // enable successful append
	if _, err := live.Submit(member, create, now); err != nil {
		t.Fatalf("retry after failed append: %v", err)
	}
	entries = entries[1:]
	if got, err := live.Submit(member, create, now); err != nil || got.GetRevision() != 1 || len(entries) != 1 {
		t.Fatalf("duplicate ran another decision: %+v %v", got, err)
	}
	if _, err := live.Submit(member, sub("k1", "create", `{"name":"Other"}`), now); err == nil || len(entries) != 1 {
		t.Fatal("conflicting key committed")
	}
	if _, err := live.Submit(member, sub("bad", "edit", `{"qty":"wrong"}`), now); err == nil || len(entries) != 1 {
		t.Fatal("rejected edit committed")
	}
	if _, err := live.Submit(member, sub("k2", "edit", `{"qty":7}`), now.Add(time.Second)); err != nil {
		t.Fatalf("edit: %v", err)
	}
	if len(entries) != 2 || entries[0].Kind != "accepted-result" || entries[1].Kind != "accepted-result" {
		t.Fatalf("expected two result entries: %+v", entries)
	}
	CheckReplay(t, live, entries, func() *Tenant { return stockTenant(t) })
}

func TestAcceptedResultCrashAfterAppendBeforeApply(t *testing.T) {
	source := stockTenant(t)
	member, _ := source.Member("ana")
	at := time.Date(2026, 9, 28, 14, 0, 0, 0, time.UTC)
	var durable Entry
	source.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
		durable = e // the one PostgreSQL insertion has committed
		panic("injected crash before application")
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("committed crash was swallowed")
			}
		}()
		source.Submit(member, &pb.Submission{TenantId: source.ID, PrincipalId: member.ID, Authority: "stock",
			IdempotencyKey: "crash", Target: &pb.EntityRef{Type: "stock.bin", Id: "B1"},
			Schema: &pb.SchemaRef{Name: "stock.bin.create", Version: 1}, Payload: []byte(`{"code":"A"}`)}, at)
	}()
	if durable.Kind != "accepted-result" || source.records.types["stock.bin"].rows["B1"] != nil ||
		len(source.app("stock").(*stock).ledger.Changes.Records(source.ID)) != 0 {
		t.Fatal("result was not durable first, or a draft escaped before application")
	}
	restarted := stockTenant(t)
	if err := restarted.Replay([]Entry{durable}); err != nil {
		t.Fatalf("restart must apply committed result: %v", err)
	}
	if restarted.records.types["stock.bin"].rows["B1"] == nil ||
		len(restarted.app("stock").(*stock).ledger.Changes.Records(source.ID)) != 1 {
		t.Fatal("durable result was not applied on restart")
	}
}

func TestAcceptedResultAppliesWithoutDecisionCode(t *testing.T) {
	source := stockTenant(t)
	app := source.app("stock").(*stock)
	member, _ := source.app(PlatformApp).(*Console).Member("ana")
	now := time.Date(2026, 9, 28, 14, 0, 0, 0, time.UTC)
	submit := func(key, verb, payload string) *pb.Submission {
		return &pb.Submission{TenantId: source.ID, PrincipalId: member.ID, Authority: "stock",
			IdempotencyKey: key, Target: &pb.EntityRef{Type: "stock.item", Id: "I1"},
			Schema: &pb.SchemaRef{Name: "stock.item." + verb, Version: 1}, Payload: []byte(payload)}
	}
	stage := func(s *pb.Submission, at time.Time) []byte {
		t.Helper()
		draft := source.newStagedDecision()
		c := platform.NewCaller(draft, member, "stock", false, false)
		receipt, err := app.Submit(c, s, at)
		if err != nil {
			t.Fatal(err)
		}
		raw, encodeErr := draft.result("stock", receipt)
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		return raw
	}
	create := submit("k1", "create", `{"name":"Bolt","qty":2,"line":"L1"}`)
	savedCreate := stage(create, now)
	if len(app.ledger.Changes.Records(source.ID)) != 0 || source.records.types["stock.item"].rows["I1"] != nil {
		t.Fatal("building a result exposed an uncommitted decision")
	}
	// Simulate loss of all in-memory draft state after the durable result: a
	// fresh tenant applies only the saved bytes, not App.Submit or its rules.
	restored := stockTenant(t)
	ledger := restored.app("stock").(*stock).ledger
	if applied, err := restored.applyAcceptedResult(ledger, savedCreate); err != nil || !applied {
		t.Fatalf("recover create: %v, applied=%t", err, applied)
	}
	c := platform.NewCaller(runtime{restored}, member, "stock", false, false)
	got, _ := restored.records.get(c, reflect.TypeFor[Item](), "I1")
	if got.(Item).Qty != 2 || len(restored.records.types["stock.item"].rows["I1"].history) != 1 ||
		len(ledger.Changes.Records(restored.ID)) != 1 || len(restored.events) != 0 {
		t.Fatalf("saved create was not applied purely: %+v", got)
	}
	if applied, err := restored.applyAcceptedResult(ledger, savedCreate); err != nil || applied {
		t.Fatalf("duplicate result reapplied: %v, applied=%t", err, applied)
	}
	// An already restored row can fill a missing kernel receipt after a
	// partially applied in-memory result, without rewriting that row.
	unappliedLedger := newStock(restored.ID).ledger
	if applied, err := restored.applyAcceptedResult(unappliedLedger, savedCreate); err != nil || !applied {
		t.Fatalf("repair missing receipt for unchanged row: %v, applied=%t", err, applied)
	}
	other, err := NewTenant("t-2", newStock("t-2"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.applyAcceptedResult(other.app("stock").(*stock).ledger, savedCreate); err == nil {
		t.Fatal("result crossed a tenant boundary")
	}
	// The second result is an edit after a committed first input.
	if _, err := source.Submit(member, create, now); err != nil {
		t.Fatal(err)
	}
	savedEdit := stage(submit("k2", "edit", `{"qty":7}`), now.Add(time.Second))
	missing := stockTenant(t)
	if _, err := missing.applyAcceptedResult(missing.app("stock").(*stock).ledger, savedEdit); err == nil ||
		len(missing.app("stock").(*stock).ledger.Changes.Records(missing.ID)) != 0 {
		t.Fatal("edit without a predecessor advanced a fresh tenant")
	}
	if applied, err := restored.applyAcceptedResult(ledger, savedEdit); err != nil || !applied {
		t.Fatalf("recover edit: %v, applied=%t", err, applied)
	}
	got, _ = restored.records.get(c, reflect.TypeFor[Item](), "I1")
	if got.(Item).Qty != 7 || len(ledger.Changes.Records(restored.ID)) != 2 ||
		len(restored.records.types["stock.item"].rows["I1"].history) != 2 {
		t.Fatalf("saved edit was not applied purely: %+v", got)
	}
	if applied, err := restored.applyAcceptedResult(ledger, savedEdit); err != nil || applied {
		t.Fatalf("duplicate edit reapplied: %v, applied=%t", err, applied)
	}
}

func TestAcceptedResultRejectsMalformedOrIncompatibleBytes(t *testing.T) {
	source := stockTenant(t)
	app := source.app("stock").(*stock)
	member, _ := source.app(PlatformApp).(*Console).Member("ana")
	draft := source.newStagedDecision()
	c := platform.NewCaller(draft, member, "stock", false, false)
	sub := &pb.Submission{TenantId: source.ID, PrincipalId: member.ID, Authority: "stock", IdempotencyKey: "k1",
		Target: &pb.EntityRef{Type: "stock.item", Id: "I1"}, Schema: &pb.SchemaRef{Name: "stock.item.create", Version: 1},
		Payload: []byte(`{"name":"Bolt","qty":2}`)}
	receipt, err := app.Submit(c, sub, time.Date(2026, 9, 28, 14, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	raw, encodeErr := draft.result("stock", receipt)
	if encodeErr != nil {
		t.Fatal(encodeErr)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var asJSON any
	if err := decoder.Decode(&asJSON); err != nil {
		t.Fatal(err)
	}
	reordered, _ := json.Marshal(asJSON)
	if _, _, err := decodeAcceptedResult(reordered); err != nil {
		t.Fatalf("JSONB-style reordered keys changed the result digest: %v", err)
	}
	for name, mutate := range map[string]func(*acceptedResult){
		"version":        func(r *acceptedResult) { r.Version++ },
		"request digest": func(r *acceptedResult) { r.RequestHash = "other" },
		"record type":    func(r *acceptedResult) { r.Row.Type = "other" },
		"record image":   func(r *acceptedResult) { r.Row.Value = []byte(`{"id":"I2"}`) },
		"changed value": func(r *acceptedResult) {
			r.Row.Value = bytes.Replace(r.Row.Value, []byte(`"qty":2`), []byte(`"qty":99`), 1)
		},
		"history": func(r *acceptedResult) { r.Row.History[len(r.Row.History)-1].Change = "other" },
	} {
		t.Run(name, func(t *testing.T) {
			var changed acceptedResult
			if err := json.Unmarshal(raw, &changed); err != nil {
				t.Fatal(err)
			}
			mutate(&changed)
			tampered, _ := json.Marshal(changed)
			recovered := stockTenant(t)
			ledger := recovered.app("stock").(*stock).ledger
			if _, err := recovered.applyAcceptedResult(ledger, tampered); err == nil ||
				len(ledger.Changes.Records(recovered.ID)) != 0 || recovered.records.types["stock.item"].rows["I1"] != nil {
				t.Fatalf("invalid bytes modified recovered tenant: %v", err)
			}
		})
	}
	if _, _, err := decodeAcceptedResult(bytes.Repeat([]byte(" "), maxAcceptedResultBytes+1)); err == nil {
		t.Fatal("oversized result accepted")
	}
}
