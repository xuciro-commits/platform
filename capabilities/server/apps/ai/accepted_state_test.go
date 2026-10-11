package ai

import (
	"bytes"
	"testing"
	"time"
)

func TestAcceptedConfigurationPreservesConcurrentModelReservations(t *testing.T) {
	at := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	live := New("tenant")
	live.providers = []Provider{{ID: "local", Kind: "local", BaseURL: "http://localhost:1234", Wire: "openai"}}
	live.models = []Model{{Provider: "local", Model: "model", Access: "everyone"}}
	live.limits["member"] = Limit{Member: "member", PerMinute: 5}
	live.usage = []Usage{{At: at, Member: "member", Model: "local/model", Input: 2}}
	live.daily["2026-10-11|member"] = 2
	live.recent["member"] = []time.Time{at}
	live.inflight["member"] = []time.Time{at}
	before, _ := live.AcceptedState()
	app, err := live.ForkAcceptedState()
	if err != nil {
		t.Fatal(err)
	}
	private := app.(*AI)
	private.providers[0].Secret = "next-key"
	private.models[0].DailyTokens = 100
	private.limits["member"] = Limit{Member: "member", PerMinute: 10}
	private.recent["member"][0] = at.Add(time.Second)
	private.inflight["member"][0] = at.Add(time.Second)
	unchanged, _ := live.AcceptedState()
	if !bytes.Equal(before, unchanged) || !live.recent["member"][0].Equal(at) || !live.inflight["member"][0].Equal(at) {
		t.Fatal("private configuration or execution bookkeeping changed the live owner")
	}
	// A model call can reserve outside the tenant lock while the configuration
	// is awaiting durable acceptance. Applying it must retain both live calls.
	live.inflight["member"] = append(live.inflight["member"], at.Add(2*time.Second))
	image, _ := private.AcceptedState()
	if err := live.ApplyAcceptedState(image); err != nil {
		t.Fatal(err)
	}
	if live.providers[0].Secret != "next-key" || live.models[0].DailyTokens != 100 || live.limits["member"].PerMinute != 10 ||
		len(live.inflight["member"]) != 2 || !live.recent["member"][0].Equal(at) || live.AcceptedLedger() != private.AcceptedLedger() {
		t.Fatal("committed configuration lost live model reservations or ledger ownership")
	}
	prior, _ := live.AcceptedState()
	if err := live.ApplyAcceptedState([]byte(`{"providers":[{"id":"same"},{"id":"same"}]}`)); err == nil {
		t.Fatal("duplicate provider image accepted")
	}
	after, _ := live.AcceptedState()
	if !bytes.Equal(prior, after) {
		t.Fatal("invalid image changed live state")
	}
}
