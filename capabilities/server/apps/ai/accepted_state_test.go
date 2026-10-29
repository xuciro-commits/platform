package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"platformserver/internal/aistate"
	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
	"strings"
	"testing"
	"time"
)

func TestAcceptedConfigurationPreservesOriginalMeterReservations(t *testing.T) {
	at := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	live := New("tenant")
	live.providers = []Provider{{ID: "local", Kind: "local", BaseURL: "http://localhost:1234", Wire: "openai"}}
	live.models = []Model{{Provider: "local", Model: "model", Access: "everyone"}}
	live.reservations["call"] = platform.AIReservation{Call: "call", At: at, Member: "member", Model: "local/model", Input: 10, Output: 20, State: "pending"}
	before, _ := live.AcceptedState()
	app, err := live.ForkAcceptedState()
	if err != nil {
		t.Fatal(err)
	}
	private := app.(*AI)
	private.providers[0].Secret = "next-key"
	r := private.reservations["call"]
	r.State = "unknown"
	private.reservations["call"] = r
	unchanged, _ := live.AcceptedState()
	if !bytes.Equal(before, unchanged) {
		t.Fatal("staged reservation changed the live meter")
	}
	image, _ := private.AcceptedState()
	if err := live.ApplyAcceptedState(image); err != nil {
		t.Fatal(err)
	}
	if live.providers[0].Secret != "next-key" || live.reservations["call"].State != "unknown" || live.AcceptedLedger() != private.AcceptedLedger() {
		t.Fatal("accepted image lost the original reservation or ledger")
	}
	saved, _ := live.Snapshot()
	restarted := New("tenant")
	if err := restarted.Restore(saved); err != nil {
		t.Fatal(err)
	}
	restored, _ := restarted.AcceptedState()
	if !bytes.Equal(image, restored) {
		t.Fatal("restoring the owner silently forgot unknown liability")
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

// Rebuilding candidate state must choose the same preceding refusal rather
// than whichever map entry happens to be visited first.
func TestTrainingPreferenceUsesLatestComparablePrecedingRefusal(t *testing.T) {
	at := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name     string
		refusals []platform.AITrainingCandidate
		want     string
	}{
		{name: "latest preceding refusal", refusals: []platform.AITrainingCandidate{
			{ID: "older", At: at.Add(-2 * time.Minute), Rejected: map[string]json.RawMessage{"reply": json.RawMessage(`"older"`)}},
			{ID: "latest", At: at.Add(-time.Minute), Rejected: map[string]json.RawMessage{"reply": json.RawMessage(`"latest"`)}},
			{ID: "future", At: at.Add(time.Minute), Rejected: map[string]json.RawMessage{"reply": json.RawMessage(`"future"`)}},
		}, want: `"latest"`},
		{name: "future refusal cannot explain prior acceptance", refusals: []platform.AITrainingCandidate{
			{ID: "future", At: at.Add(time.Minute), Rejected: map[string]json.RawMessage{"reply": json.RawMessage(`"future"`)}},
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			owner := New("tenant")
			owner.trainingPolicies["reviewed"] = platform.AITrainingPolicy{ID: "reviewed", Revision: 1, Enabled: true, TrainingAllowed: true, ProviderAllowed: true, Fields: []string{"reply"}, RetainDays: 30, EffectiveAt: at.Add(-time.Hour)}
			for _, refusal := range test.refusals {
				refusal.Intent, refusal.Action, refusal.Definition, refusal.Authorization = "intent", "desk.answer", "definition", "same-grants"
				refusal.Outcome, refusal.Policy, refusal.PolicyVersion = "rejected", "reviewed", 1
				owner.trainingCandidates[refusal.ID] = refusal
			}
			owner.trainingHost = trainingEvidenceFixture{platform.AITrainingEvidence{Candidates: []platform.AITrainingCandidate{{ID: "accepted", At: at, Intent: "intent", Action: "desk.answer", Definition: "definition", Authorization: "same-grants", Outcome: "accepted", Chosen: map[string]json.RawMessage{"reply": json.RawMessage(`"correct"`)}}}}}
			if refusal := owner.Listen(platform.Caller{}, platform.Event{}, nil, at); refusal != nil {
				t.Fatal(refusal)
			}
			if got := string(owner.trainingCandidates["accepted"].Rejected["reply"]); got != test.want {
				t.Fatalf("preference pair used %s; want %s", got, test.want)
			}
		})
	}
}

type trainingEvidenceFixture struct{ evidence platform.AITrainingEvidence }

func (f trainingEvidenceFixture) TrainingEvidence(platform.Event) platform.AITrainingEvidence {
	return f.evidence
}
func (trainingEvidenceFixture) TrainingReadable(platform.Member, platform.AITrainingCandidate, time.Time) string {
	return ""
}

func TestTrainingCandidateCapacityKeepsRecentEvidenceAndBoundsMetadata(t *testing.T) {
	at := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name  string
		fill  func(*AI)
		check func(*testing.T, *AI)
	}{
		{name: "unconfigured metadata count", fill: func(owner *AI) {
			for i := range trainingCandidatesKept + 1 {
				id := fmt.Sprint(i)
				owner.trainingCandidates[id] = platform.AITrainingCandidate{ID: id, At: at.Add(time.Duration(i) * time.Second), Evidence: "original/" + id}
			}
		}, check: func(t *testing.T, owner *AI) {
			if len(owner.trainingCandidates) != trainingCandidatesKept || owner.trainingCandidates["0"].ID != "" || owner.trainingCandidates[fmt.Sprint(trainingCandidatesKept)].Evidence == "" {
				t.Fatal("candidate capacity lost its newest facts or retained the oldest metadata")
			}
		}},
		{name: "total content bytes", fill: func(owner *AI) {
			payload, _ := json.Marshal(strings.Repeat("x", trainingCandidatesBytes/5))
			for i := range 9 {
				id := fmt.Sprint(i)
				owner.trainingCandidates[id] = platform.AITrainingCandidate{ID: id, At: at.Add(time.Duration(i) * time.Second), Chosen: map[string]json.RawMessage{"reply": payload}}
			}
		}, check: func(t *testing.T, owner *AI) {
			if len(owner.trainingCandidates) == 0 || len(owner.trainingCandidates) == 9 || owner.trainingCandidates["8"].ID == "" || owner.trainingCandidates["0"].ID != "" {
				t.Fatal("byte capacity did not retain a bounded window of newest facts")
			}
		}},
		{name: "oversized content preserves metadata", fill: func(owner *AI) {
			payload, _ := json.Marshal(strings.Repeat("x", trainingCandidatesBytes))
			owner.trainingCandidates["large"] = platform.AITrainingCandidate{ID: "large", At: at, Evidence: "original/change", Approved: true, QualityEvidence: "review", Chosen: map[string]json.RawMessage{"reply": payload}}
		}, check: func(t *testing.T, owner *AI) {
			candidate := owner.trainingCandidates["large"]
			if candidate.Evidence != "original/change" || len(candidate.Chosen) > 0 || candidate.Approved {
				t.Fatal("oversized content was retained or lost its original evidence reference")
			}
		}},
		{name: "oversized source metadata does not block derivation", fill: func(owner *AI) {
			owner.trainingCandidates["large"] = platform.AITrainingCandidate{ID: "large", At: at, Sources: map[string]uint32{strings.Repeat("x", trainingCandidatesBytes): 1}}
		}, check: func(t *testing.T, owner *AI) {
			if len(owner.trainingCandidates) != 0 {
				t.Fatal("oversized metadata entered the candidate collection")
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			owner := New("tenant")
			owner.trainingHost = trainingEvidenceFixture{}
			test.fill(owner)
			if refusal := owner.Listen(platform.Caller{}, platform.Event{}, nil, at); refusal != nil {
				t.Fatal("candidate capacity changed business acceptance", refusal)
			}
			raw, err := json.Marshal(owner.trainingCandidates)
			if err != nil || len(raw) > trainingCandidatesBytes {
				t.Fatal("candidate JSON exceeds its byte capacity", len(raw), err)
			}
			test.check(t, owner)
			image, err := owner.AcceptedState()
			if err != nil {
				t.Fatal(err)
			}
			result := acceptedformat.Input{Version: 2, Kind: "input-result", Tenant: "tenant", App: ID, Name: "candidate-capacity", Member: "app:" + ID, At: at, Answer: json.RawMessage(`{}`), States: []acceptedformat.State{{App: ID, Image: image}}}
			result.RequestHash, err = acceptedformat.InputHash(result.Tenant, result.App, result.Name, result.Member, result.Body)
			if err != nil {
				t.Fatal(err)
			}
			result.Key = "input:" + result.RequestHash
			result.Digest, err = acceptedformat.DigestInput(result)
			if err != nil {
				t.Fatal(err)
			}
			raw, err = json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := acceptedformat.DecodeInput(raw); err != nil {
				t.Fatal("bounded candidate state cannot fit the original accepted-result envelope", err)
			}
		})
	}
}
