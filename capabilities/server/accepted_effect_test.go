package platformserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/ai"
	"platformserver/platform"
)

type answerStock struct{ *stock }

func (a *answerStock) Answer(c platform.Caller, _ platform.Effect, outcome platform.Outcome, at time.Time) *kernel.Error {
	item, ok := platform.Get[Item](c, "I1")
	if !ok {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	item.Note = outcome.Detail
	return c.PutAt(at, "Effect answer", item)
}

type mixedAnswerStock struct{ *answerStock }

func (a *mixedAnswerStock) Answer(c platform.Caller, e platform.Effect, outcome platform.Outcome, at time.Time) *kernel.Error {
	if err := a.answerStock.Answer(c, e, outcome, at); err != nil {
		return err
	}
	_, err := platform.Decide(c, a, &pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID,
		Authority: "stock", IdempotencyKey: "effect-child", Target: &pb.EntityRef{Type: "stock.bin", Id: "B2"},
		Schema: &pb.SchemaRef{Name: "stock.bin.create", Version: 1}, Payload: []byte(`{"code":"B"}`)}, at)
	return err
}

func TestAcceptedEffectKeepsDirectObservationAndChildDecisionTogether(t *testing.T) {
	id := "effect-mixed"
	at := time.Date(2026, 9, 28, 23, 3, 0, 0, time.UTC)
	build := func() *Tenant {
		seat := Seat{Subjects: []string{"ana"}, Member: platform.Member{ID: "ana", Roles: map[string]string{"stock": "clerk"}}}
		tn, err := NewTenant(id, NewConsole(id, seat), &mixedAnswerStock{&answerStock{newStock(id)}})
		if err != nil {
			t.Fatal(err)
		}
		member, _ := tn.Member("ana")
		_, refusal := tn.Submit(member, &pb.Submission{TenantId: id, PrincipalId: member.ID, Authority: "stock",
			IdempotencyKey: "create", Target: &pb.EntityRef{Type: "stock.item", Id: "I1"},
			Schema:  &pb.SchemaRef{Name: "stock.item.create", Version: 1},
			Payload: []byte(`{"name":"Bolt","qty":2,"line":"L1"}`)}, at)
		if refusal != nil {
			t.Fatal(refusal)
		}
		tn.outbound = append(tn.outbound, &effect{Effect: platform.Effect{ID: "mixed-1", App: "stock",
			Endpoint: "sink", Event: "stock/reply", State: "pending", At: at, Due: at}})
		return tn
	}
	live := build()
	var saved Entry
	live.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { saved = e; return e.Body, nil }
	live.settle("mixed-1", platform.Outcome{Effect: "mixed-1", Result: "delivered", Detail: "accepted"}, at.Add(time.Second))
	result, err := decodeAcceptedEffect(saved.Body)
	if err != nil || live.quarantined() || len(result.Rows) != 1 || len(result.Changes) == 0 ||
		live.records.types["stock.item"].rows["I1"].value.Interface().(Item).Note != "accepted" ||
		live.records.types["stock.bin"].rows["B2"] == nil {
		t.Fatalf("mixed callback did not commit as one result: %v fault=%+v result=%+v", err, live.fault.Load(), result)
	}
	recovered := build()
	if err := recovered.Replay([]Entry{saved}); err != nil ||
		recovered.records.types["stock.item"].rows["I1"].value.Interface().(Item).Note != "accepted" ||
		recovered.records.types["stock.bin"].rows["B2"] == nil {
		t.Fatalf("mixed callback recovery lost a change: %v", err)
	}
	// A sound outer digest must not let a damaged child decision expose the
	// direct observation before the child predecessor has been checked.
	var damagedChild map[string]any
	if err := json.Unmarshal(result.Changes, &damagedChild); err != nil {
		t.Fatal(err)
	}
	damagedChild["digest"] = "wrong"
	result.Changes, _ = json.Marshal(damagedChild)
	result.Digest, err = digestAcceptedEffect(result)
	if err != nil {
		t.Fatal(err)
	}
	damaged, _ := json.Marshal(result)
	isolated := build()
	if _, applied, err := isolated.applyAcceptedEffect(damaged); err == nil || applied ||
		isolated.records.types["stock.item"].rows["I1"].value.Interface().(Item).Note != "" ||
		isolated.records.types["stock.bin"].rows["B2"] != nil {
		t.Fatalf("invalid child exposed an observation or a decision: %v (applied=%t)", err, applied)
	}
}

func TestJournalAcceptedEffectCrashBeforeApplication(t *testing.T) {
	dsn := os.Getenv("PLATFORM_TEST_DATABASE")
	if dsn == "" {
		t.Skip("PLATFORM_TEST_DATABASE is not set")
	}
	ctx := context.Background()
	journal, err := OpenJournal(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	id := fmt.Sprintf("effect-crash-%d", time.Now().UnixNano())
	defer journal.pool.Exec(ctx, `delete from journal where tenant=$1`, id)
	if _, err := journal.Entries(ctx, id, 0); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 9, 28, 23, 10, 0, 0, time.UTC)
	build := func() *Tenant {
		seat := Seat{Subjects: []string{"ana"}, Member: platform.Member{ID: "ana", Roles: map[string]string{"stock": "clerk"}}}
		tn, err := NewTenant(id, NewConsole(id, seat), &answerStock{newStock(id)})
		if err != nil {
			t.Fatal(err)
		}
		member, _ := tn.Member("ana")
		_, refusal := tn.Submit(member, &pb.Submission{TenantId: id, PrincipalId: member.ID, Authority: "stock",
			IdempotencyKey: "create", Target: &pb.EntityRef{Type: "stock.item", Id: "I1"},
			Schema:  &pb.SchemaRef{Name: "stock.item.create", Version: 1},
			Payload: []byte(`{"name":"Bolt","qty":2,"line":"L1"}`)}, at)
		if refusal != nil {
			t.Fatal(refusal)
		}
		tn.outbound = append(tn.outbound, &effect{Effect: platform.Effect{
			ID: "effect-1", App: "stock", Event: "stock/reply", Endpoint: "sink",
			State: "pending", At: at, Due: at}})
		return tn
	}
	live := build()
	live.AcceptResult = func(e Entry, key, hash string) ([]byte, error) {
		committed, err := journal.AppendAccepted(ctx, id, e, key, hash)
		if err != nil || len(committed) == 0 {
			t.Fatalf("append effect outcome: %v", err)
		}
		panic("crash after append")
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("post-commit crash was not injected")
			}
		}()
		live.settle("effect-1", platform.Outcome{Effect: "effect-1", Result: "delivered", Detail: "accepted"}, at.Add(time.Second))
	}()
	if live.outbound[0].State != "pending" ||
		live.records.types["stock.item"].rows["I1"].value.Interface().(Item).Note != "" {
		t.Fatal("outcome or callback escaped before application")
	}
	reopened, err := OpenJournal(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	entries, err := reopened.Entries(ctx, id, 0)
	if err != nil || len(entries) != 1 {
		t.Fatalf("effect result was not durable: %v, %d entries", err, len(entries))
	}
	restored := build()
	if err := restored.Replay(entries); err != nil || restored.outbound[0].State != "delivered" ||
		restored.records.types["stock.item"].rows["I1"].value.Interface().(Item).Note != "accepted" {
		t.Fatalf("recovery did not apply the saved outcome and callback: %v", err)
	}
	result, err := decodeAcceptedEffect(entries[0].Body)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.AppendAccepted(ctx, id, entries[0], result.Key, result.RequestHash); err != nil ||
		reopened.Position(id) != 1 {
		t.Fatalf("effect retry duplicated a committed outcome: %v", err)
	}
	if _, err := reopened.AppendAccepted(ctx, id, entries[0], result.Key, "other"); !errors.Is(err, errAcceptedConflict) {
		t.Fatalf("effect key accepted a different answer: %v", err)
	}
}

func TestAcceptedEffectCommitsOutcomeAndCallbackTogether(t *testing.T) {
	const tenantID = "effect-atomic"
	at := time.Date(2026, 9, 28, 23, 0, 0, 0, time.UTC)
	build := func() *Tenant {
		seat := Seat{Subjects: []string{"ana"}, Member: platform.Member{ID: "ana", Roles: map[string]string{"stock": "clerk"}}}
		tn, err := NewTenant(tenantID, NewConsole(tenantID, seat), &answerStock{newStock(tenantID)})
		if err != nil {
			t.Fatal(err)
		}
		m, _ := tn.Member("ana")
		_, refusal := tn.Submit(m, &pb.Submission{TenantId: tenantID, PrincipalId: m.ID, Authority: "stock",
			IdempotencyKey: "create", Target: &pb.EntityRef{Type: "stock.item", Id: "I1"},
			Schema:  &pb.SchemaRef{Name: "stock.item.create", Version: 1},
			Payload: []byte(`{"name":"Bolt","qty":2,"line":"L1"}`)}, at)
		if refusal != nil {
			t.Fatal(refusal)
		}
		tn.outbound = append(tn.outbound, &effect{Effect: platform.Effect{
			ID: "effect-1", App: "stock", Event: "stock/reply", Endpoint: "sink",
			State: "pending", At: at, Due: at}})
		return tn
	}
	live := build()
	live.AcceptResult = func(Entry, string, string) ([]byte, error) { return nil, errors.New("append failed") }
	outcome := platform.Outcome{Effect: "effect-1", Result: "delivered", Detail: "accepted"}
	live.settle(outcome.Effect, outcome, at.Add(time.Second))
	if live.quarantined() || live.outbound[0].State != "pending" ||
		live.records.types["stock.item"].rows["I1"].value.Interface().(Item).Note != "" {
		t.Fatal("failed append leaked the callback or effect outcome")
	}
	var saved Entry
	live.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
		saved = e
		return e.Body, nil
	}
	live.settle(outcome.Effect, outcome, at.Add(time.Second))
	if live.quarantined() || live.outbound[0].State != "delivered" ||
		live.records.types["stock.item"].rows["I1"].value.Interface().(Item).Note != "accepted" {
		t.Fatalf("committed callback missing: fault=%+v effect=%+v", live.fault.Load(), live.outbound[0])
	}
	result, err := decodeAcceptedEffect(saved.Body)
	if err != nil || len(result.Rows) != 1 || result.Changes != nil {
		t.Fatalf("direct callback was not in the effect result: %+v, %v", result, err)
	}
	recovered := build()
	if err := recovered.Replay([]Entry{saved}); err != nil {
		t.Fatalf("pure replay re-ran or lost the callback: %v", err)
	}
	if recovered.outbound[0].State != "delivered" ||
		recovered.records.types["stock.item"].rows["I1"].value.Interface().(Item).Note != "accepted" {
		t.Fatal("replay did not restore the same outcome and record")
	}
	var tampered map[string]any
	if err := json.Unmarshal(saved.Body, &tampered); err != nil {
		t.Fatal(err)
	}
	tampered["digest"] = "wrong"
	damaged, _ := json.Marshal(tampered)
	another := build()
	saved.Body = damaged
	if err := another.recoverEntries([]Entry{saved}); err == nil || !another.quarantined() {
		t.Fatal("tampered outcome revived a tenant")
	}
}

func TestAcceptedModelEffectKeepsAnswerInSameResult(t *testing.T) {
	compose := func() *Tenant {
		source := newRequestsTenant(t, false)
		tn, err := NewTenant("t", NewConsole("t"), &modelResultCart{resultCart: resultCart{source.app("cart").(*cart)}, model: "local/chosen"})
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	live := compose()
	at := time.Date(2026, 9, 28, 23, 5, 0, 0, time.UTC)
	member := platform.Member{ID: "buyer", Tenant: "t", Roles: map[string]string{"cart": "buyer"}}
	sub := &pb.Submission{TenantId: "t", PrincipalId: member.ID, Authority: "cart", IdempotencyKey: "ask",
		Target: &pb.EntityRef{Type: "cart.line", Id: "L1"},
		Schema: &pb.SchemaRef{Name: "cart.line.ask", Version: 1}, Payload: []byte(`{"item":"pen"}`)}
	var entries []Entry
	live.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
		entries = append(entries, e)
		return e.Body, nil
	}
	if _, err := live.Submit(member, sub, at); err != nil || len(live.outbound) == 0 {
		t.Fatalf("model intent was not accepted: %v", err)
	}
	outcome := platform.Outcome{Effect: live.outbound[0].ID, Result: "delivered", Answer: json.RawMessage(`"yes"`)}
	live.settle(outcome.Effect, outcome, at.Add(time.Second))
	if live.quarantined() || len(entries) != 2 {
		t.Fatalf("model effect did not commit with its answer: fault=%+v entries=%d", live.fault.Load(), len(entries))
	}
	result, err := decodeAcceptedEffect(entries[1].Body)
	if err != nil || len(result.Changes) == 0 {
		t.Fatalf("model answer is outside the accepted result: %v", err)
	}
	restored := compose()
	if err := restored.Replay(entries); err != nil {
		t.Fatalf("recovery could not apply the model answer: %v", err)
	}
	if restored.outbound[0].State != "delivered" ||
		restored.records.types["cart.line"].rows["L1"].value.Interface().(Cart).Status !=
			live.records.types["cart.line"].rows["L1"].value.Interface().(Cart).Status {
		t.Fatal("model answer and effect state diverged after recovery")
	}
}

func TestAcceptedModelUsageSharesEffectResultAndReplaysWithoutMeteringAgain(t *testing.T) {
	const id = "model-usage-result"
	at := time.Date(2026, 9, 28, 23, 15, 0, 0, time.UTC)
	build := func() *Tenant {
		tn, err := NewTenant(id, NewConsole(id), ai.New(id), newStock(id))
		if err != nil {
			t.Fatal(err)
		}
		tn.outbound = append(tn.outbound, &effect{Effect: platform.Effect{
			ID: "model-1", App: "stock", Endpoint: modelEndpoint, State: "pending", At: at, Due: at}})
		return tn
	}
	live := build()
	outcome := platform.Outcome{Effect: "model-1", Result: "retry", Detail: "provider unavailable"}
	usage := &ai.Usage{Member: "app:stock", Model: "model-1", Input: 7, Output: 2, At: at.Add(time.Second)}
	live.AcceptResult = func(Entry, string, string) ([]byte, error) {
		return nil, errors.New("append failed")
	}
	live.settleWithUsage(outcome.Effect, outcome, usage, usage.At)
	if live.quarantined() || live.outbound[0].State != "pending" || len(live.ai.Usage()) != 0 {
		t.Fatal("failed append leaked the model usage or effect result")
	}
	var entries []Entry
	live.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
		entries = append(entries, e)
		return e.Body, nil
	}
	live.settleWithUsage(outcome.Effect, outcome, usage, usage.At)
	if live.quarantined() || len(entries) != 1 || len(live.ai.Usage()) != 1 {
		t.Fatalf("usage must share one effect-result row: fault=%+v entries=%d usage=%d",
			live.fault.Load(), len(entries), len(live.ai.Usage()))
	}
	result, err := decodeAcceptedEffect(entries[0].Body)
	if err != nil || result.Usage == nil || result.UsageBefore == "" {
		t.Fatalf("model usage was not recorded with the outcome: %v, %+v", err, result)
	}
	restored := build()
	if err := restored.Replay(entries); err != nil || len(restored.ai.Usage()) != 1 ||
		restored.outbound[0].State != live.outbound[0].State {
		t.Fatalf("replay lost or double-metered the accepted result: %v", err)
	}
	if _, applied, err := restored.applyAcceptedEffect(entries[0].Body); err == nil || applied {
		t.Fatalf("duplicate result was reapplied without checking its predecessor: %v", err)
	}
	if len(restored.ai.Usage()) != 1 {
		t.Fatal("duplicate result changed model usage")
	}
}

func TestJournalAcceptedModelUsageCrashBeforeApplication(t *testing.T) {
	dsn := os.Getenv("PLATFORM_TEST_DATABASE")
	if dsn == "" {
		t.Skip("PLATFORM_TEST_DATABASE is not set")
	}
	ctx := context.Background()
	journal, err := OpenJournal(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	id := fmt.Sprintf("model-usage-crash-%d", time.Now().UnixNano())
	defer journal.pool.Exec(ctx, `delete from journal where tenant=$1`, id)
	if _, err := journal.Entries(ctx, id, 0); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 9, 28, 23, 16, 0, 0, time.UTC)
	build := func() *Tenant {
		tn, err := NewTenant(id, NewConsole(id), ai.New(id), newStock(id))
		if err != nil {
			t.Fatal(err)
		}
		tn.outbound = append(tn.outbound, &effect{Effect: platform.Effect{
			ID: "model-crash", App: "stock", Endpoint: modelEndpoint, State: "pending", At: at, Due: at}})
		return tn
	}
	live := build()
	live.AcceptResult = func(e Entry, key, hash string) ([]byte, error) {
		if _, err := journal.AppendAccepted(ctx, id, e, key, hash); err != nil {
			t.Fatal(err)
		}
		panic("crash after model outcome append")
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("post-commit crash was not injected")
			}
		}()
		live.settleWithUsage("model-crash",
			platform.Outcome{Effect: "model-crash", Result: "retry", Detail: "provider unavailable"},
			&ai.Usage{Member: "app:stock", Model: "model-crash", Input: 7, At: at.Add(time.Second)},
			at.Add(time.Second))
	}()
	if live.outbound[0].State != "pending" || len(live.ai.Usage()) != 0 {
		t.Fatal("usage or effect escaped before application")
	}
	entries, err := journal.Entries(ctx, id, 0)
	if err != nil || len(entries) != 1 {
		t.Fatalf("model outcome was not one durable result: %v entries=%d", err, len(entries))
	}
	restored := build()
	if err := restored.Replay(entries); err != nil || len(restored.ai.Usage()) != 1 ||
		restored.outbound[0].State != "retrying" || restored.outbound[0].Attempts != 1 {
		t.Fatalf("model outcome was not recovered atomically: %v", err)
	}
}
