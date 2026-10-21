package ai

import (
	"bytes"
	"encoding/json"
	"platformserver/internal/aistate"
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

func TestApplyUsageStateMatchesMeterAcrossDaysAndRetention(t *testing.T) {
	at := time.Date(2026, 10, 11, 23, 59, 0, 0, time.FixedZone("offset", -2*60*60))
	owner := New("tenant")
	owner.providers = []Provider{{ID: "local", Kind: "local", BaseURL: "http://localhost:1234", Wire: "openai"}}
	owner.models = []Model{{Provider: "local", Model: "model", Access: "everyone"}}
	owner.limits["member"] = Limit{Member: "member", DailyTokens: 123456, PerMinute: 5}
	// A retained history may already be at its limit. Old daily totals cannot be
	// reconstructed from that truncated history and must be carried forward.
	owner.daily["2026-10-09|member"] = 91
	owner.daily["2026-10-10|member"] = 92
	owner.daily["2026-10-11|member"] = 93
	for i := range aistate.UsageKept {
		owner.usage = append(owner.usage, Usage{At: at.Add(-time.Duration(aistate.UsageKept-i) * time.Second), Member: "member", Model: "local/model", Input: i})
	}
	image, err := owner.AcceptedState()
	if err != nil {
		t.Fatal(err)
	}
	readings := []Usage{
		{At: at, Member: "member", Model: "local/model", Input: 7, Output: 3},
		{At: at.Add(24 * time.Hour), Member: "other", Model: "local/model", Input: 11, Output: 5},
	}
	for _, usage := range readings {
		prior := bytes.Clone(image)
		next, err := aistate.ApplyUsageState(image, usage)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(image, prior) {
			t.Fatal("meter projection modified its input bytes")
		}
		owner.Meter(usage)
		expected, err := owner.AcceptedState()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(next, expected) {
			t.Fatal("meter projection differs from the ordinary AI owner")
		}
		image = next
	}
	var state aistate.State
	if err := json.Unmarshal(image, &state); err != nil {
		t.Fatal(err)
	}
	if len(state.Usage) != aistate.UsageKept || state.Usage[0].Input != 2 || state.Usage[len(state.Usage)-1].Member != readings[1].Member || !state.Usage[len(state.Usage)-1].At.Equal(readings[1].At) {
		t.Fatal("meter projection changed retained usage order or limit")
	}
	// at is October 12 in UTC; the next reading is October 13. Only that day
	// and its predecessor remain, using the same UTC rule as ordinary metering.
	if len(state.Daily) != 4 || state.Daily["2026-10-12|member"] != 10 || state.Daily["2026-10-12|model:local/model"] != 10 || state.Daily["2026-10-13|other"] != 16 || state.Daily["2026-10-13|model:local/model"] != 16 {
		t.Fatal("meter projection changed cross-day totals", state.Daily)
	}
	if state.Limits["member"].DailyTokens != 123456 || len(state.Providers) != 1 || len(state.Models) != 1 {
		t.Fatal("meter projection changed owner configuration")
	}
}
