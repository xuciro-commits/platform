package platformserver

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"platformkernel/kernel"
	"platformserver/platform"
)

type neverAnswerStock struct{ *answerStock }

func (*neverAnswerStock) Answer(platform.Caller, platform.Effect, platform.Outcome, time.Time) *kernel.Error {
	panic("recovery ran an effect callback")
}

func TestAcceptedControlCallbackRowsAppendFailureCrashAndPureRecovery(t *testing.T) {
	const id = "control-callback"
	at := time.Date(2026, 10, 11, 18, 0, 0, 0, time.UTC)
	build := func(forbid bool) *Tenant {
		app := &answerStock{newStock(id)}
		var owner platform.App = app
		if forbid {
			owner = &neverAnswerStock{app}
		}
		return composeTenant(t, id, []Seat{seatOf("ana", "platform:admin", "stock:clerk")}, owner)
	}
	live := build(false)
	decide(t, live, "ana", "stock", "stock.item.create", "stock.item", "I1", map[string]any{"name": "Bolt", "qty": 2, "line": "L1"}, at)
	live.outbound = append(live.outbound, &effect{Effect: platform.Effect{ID: id + ":reply", App: "stock", Endpoint: "sink", Event: "stock/reply", Target: "stock.item/I1", State: "pending", At: at, Due: at}})
	image, _, err := live.Snapshot(func() int64 { return 0 })
	if err != nil {
		t.Fatal(err)
	}
	member := memberOf(t, live, "ana")
	request := submission(live, member, PlatformApp, SchemaEffectDiscard, EffectType, id+":reply", map[string]any{})
	value := func(tenant *Tenant) Item {
		item, _ := platform.Get[Item](tenant.automation("stock", false), "I1")
		return item
	}
	live.AcceptResult = func(Entry, string, string) ([]byte, error) { return nil, errors.New("append failed") }
	if _, refusal := live.Submit(member, request, at); refusal == nil || value(live).Note != "" || live.outbound[0].State != "pending" {
		t.Fatal("callback observation or control escaped failed append")
	}
	var entries []Entry
	live.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); panic("crash after append") }
	func() { defer func() { _ = recover() }(); live.Submit(member, request, at) }()
	if len(entries) != 1 || value(live).Note != "" || live.outbound[0].State != "pending" {
		t.Fatal("callback became visible before application")
	}
	batch, _, err := decodeAcceptedBatch(entries[0].Body)
	if err != nil || batch.Version != 4 || len(batch.DirectRows) != 1 || batch.Control == nil {
		t.Fatalf("callback not sealed with control: %v", err)
	}
	recovered := build(true)
	if err := recovered.Restore(image); err != nil {
		t.Fatal(err)
	}
	if err := recovered.Replay(entries); err != nil || value(recovered).Note != "discarded by ana" || recovered.outbound[0].State != "discarded" {
		t.Fatalf("callback/control pure recovery: %v note=%q", err, value(recovered).Note)
	}
	if _, refusal := recovered.Submit(member, request, at.Add(time.Second)); refusal != nil || value(recovered).Note != "discarded by ana" {
		t.Fatalf("retry re-executed callback: %v", refusal)
	}
	for _, damage := range []string{"source", "foreign source", "predecessor", "receipt", "duplicate"} {
		t.Run(damage, func(t *testing.T) {
			broken, _, err := decodeAcceptedBatch(entries[0].Body)
			if err != nil {
				t.Fatal(err)
			}
			switch damage {
			case "source":
				broken.DirectRows[0].Source.ID = "invented"
			case "foreign source":
				broken.DirectRows[0].Source.App = "foreign"
			case "predecessor":
				broken.DirectRows[0].Row.Before = "wrong"
			case "receipt":
				broken.DirectRows[0].Row.History[len(broken.DirectRows[0].Row.History)-1].Change = "invented-kernel-receipt"
			case "duplicate":
				broken.DirectRows = append(broken.DirectRows, broken.DirectRows[0])
			}
			broken.Digest, err = digestAcceptedBatch(broken)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(broken)
			if err != nil {
				t.Fatal(err)
			}
			isolated := build(true)
			if err := isolated.Restore(image); err != nil {
				t.Fatal(err)
			}
			if applied, err := isolated.applyAcceptedBatch(isolated.app(PlatformApp).AcceptedLedger(), raw); err == nil || applied || value(isolated).Note != "" || isolated.outbound[0].State != "pending" || isolated.app(PlatformApp).AcceptedLedger().AcceptedFor(id, request.GetIdempotencyKey()) != nil {
				t.Fatalf("damaged callback exposed part of the result: %v", err)
			}
		})
	}
}

func TestCallbackRowsDoNotAcceptOrdinaryUnpublishedWrites(t *testing.T) {
	const id = "callback-scope"
	at := time.Date(2026, 10, 11, 19, 0, 0, 0, time.UTC)
	live := composeTenant(t, id, []Seat{seatOf("ana", "stock:clerk")}, newStock(id))
	decide(t, live, "ana", "stock", "stock.item.create", "stock.item", "I1", map[string]any{"name": "Bolt", "qty": 2, "line": "L1"}, at)
	draft := live.newStagedDecision()
	draft.at = at
	c := platform.NewCaller(draft, platform.Member{ID: "app:stock", Tenant: id}, "stock", false, true)
	item, _ := platform.Get[Item](c, "I1")
	item.Note = "unpublished"
	if err := c.PutAt(at, "observation", item); err != nil {
		t.Fatal(err)
	}
	if _, err := draft.savedCallbackRows(map[string]bool{}); err == nil {
		t.Fatal("ordinary unpublished write borrowed callback authority")
	}
}
