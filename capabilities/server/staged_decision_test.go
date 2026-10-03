package platformserver

import (
	"reflect"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

func TestGeneratedDecisionStaysPrivate(t *testing.T) {
	tn := stockTenant(t)
	app := tn.app("stock").(*stock)
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	member, _ := tn.app(PlatformApp).(*Console).Member("ana")
	submission := func(key, schema, typ, id, payload string) *pb.Submission {
		return &pb.Submission{TenantId: tn.ID, PrincipalId: member.ID, Authority: "stock",
			IdempotencyKey: key, Target: &pb.EntityRef{Type: typ, Id: id},
			Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: []byte(payload)}
	}
	create := submission("k1", "stock.item.create", "stock.item", "I1", `{"name":"Bolt","qty":2,"line":"L1"}`)
	draft := tn.newStagedDecision()
	c := platform.NewCaller(draft, member, "stock", false, false)
	record, err := app.Submit(c, create, now)
	if err != nil || record == nil {
		t.Fatalf("staged create: %v", err)
	}
	if len(app.ledger.RecordsFor(tn.ID)) != 0 || tn.records.types["stock.item"].rows["I1"] != nil ||
		len(tn.events) != 0 || tn.acted != 0 {
		t.Fatal("decision changed the live ledger, record or event queue before acceptance")
	}
	if got, ok := draft.records.get(c, reflect.TypeFor[Item](), "I1"); !ok || got.(Item).Qty != 2 {
		t.Fatalf("staged record absent: %+v", got)
	}
	if got := draft.logs[app.ledger].Records(tn.ID); len(got) != 1 || got[0].GetChangeId() != record.GetChangeId() {
		t.Fatalf("staged receipt absent: %v", got)
	}
	if len(draft.events) != 1 || len(draft.events[0].Changed) != 1 || draft.events[0].Changed[0] != "stock.item/I1" {
		t.Fatalf("staged event missing its record reference: %+v", draft.events)
	}
	again, err := app.Submit(c, create, now)
	if err != nil || again.GetChangeId() != record.GetChangeId() || len(draft.events) != 1 {
		t.Fatalf("duplicate staged request was decided again: %v, %+v", err, draft.events)
	}
	conflict := submission("k1", "stock.item.create", "stock.item", "I1", `{"name":"Other"}`)
	if _, err := app.Submit(c, conflict, now); err == nil || len(draft.events) != 1 {
		t.Fatalf("changed reuse of staged key was accepted: %v", err)
	}
	refused := submission("bad", "stock.item.create", "stock.item", "I2", `{}`)
	if _, err := app.Submit(c, refused, now); err == nil || draft.records.types["stock.item"].rows["I2"] != nil ||
		len(draft.events) != 1 || len(draft.logs[app.ledger].Records(tn.ID)) != 1 {
		t.Fatalf("refusal left staged records, receipts or events: %v", err)
	}
	// Discarding a draft must not reserve its idempotency key or ID.
	live, err := tn.Submit(member, create, now)
	if err != nil || live.GetChangeId() != record.GetChangeId() {
		t.Fatalf("discarded draft affected subsequent live submission: %v", err)
	}
	edit := submission("k2", "stock.item.edit", "stock.item", "I1", `{"qty":7}`)
	draft = tn.newStagedDecision()
	c = platform.NewCaller(draft, member, "stock", false, false)
	edited, err := app.Submit(c, edit, now.Add(time.Second))
	if err != nil || edited.GetRevision() != 2 {
		t.Fatalf("staged edit: %v, %+v", err, edited)
	}
	current, _ := tn.records.get(c, reflect.TypeFor[Item](), "I1")
	proposed, _ := draft.records.get(c, reflect.TypeFor[Item](), "I1")
	if current.(Item).Qty != 2 || proposed.(Item).Qty != 7 ||
		len(app.ledger.RecordsFor(tn.ID)) != 1 || len(draft.logs[app.ledger].Records(tn.ID)) != 2 {
		t.Fatalf("staged edit leaked: live=%+v draft=%+v", current, proposed)
	}
}

func TestStagedDecisionRefusesOtherEffects(t *testing.T) {
	tn := stockTenant(t)
	draft := tn.newStagedDecision()
	defer func() {
		if recover() == nil {
			t.Fatal("unsupported effect escaped instead of failing closed")
		}
		if len(tn.events) != 0 || len(tn.notices) != 0 {
			t.Fatal("unsupported effect changed the live tenant")
		}
	}()
	draft.Deliver(platform.Caller{}, "", "", "", time.Now())
}
