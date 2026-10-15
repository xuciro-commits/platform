package simulator

import (
	"context"
	"reflect"
	"testing"
	"time"

	"platformserver/platform"
)

func simulation(scenario string) platform.DriverOpen {
	return platform.DriverOpen{Session: scenario, Connection: "sim", Revision: "r1", Generation: "g1", Profile: Profile,
		Configuration: platform.DriverConfiguration{Scenario: scenario, Seed: 42, Connection: map[string]any{"variation": "0.1"}, Devices: []platform.DriverDevice{{ID: "device", Title: "Simulated device"}},
			Points: []platform.DriverPoint{{ID: "value", Device: "device", Address: "10", Type: "decimal", Unit: "unit", Scale: "2", IntervalMS: 1000}}, Commands: []platform.DriverCommand{{ID: "setvalue", Point: "value", Min: "0", Max: "100"}}}}
}

func TestDeterministicWindowAndLostCommandReply(t *testing.T) {
	at := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)
	h := New()
	h.Now = func() time.Time { return at.Add(30 * time.Second) }
	in := simulation("unknown")
	if _, err := h.Handle(context.Background(), "open", platform.Raw(in)); err != nil {
		t.Fatal(err)
	}
	read := platform.DriverRead{Session: in.Session, Device: "device", Signals: []string{"value"}, Since: at, Until: at.Add(time.Minute), Mode: "points", Limit: 100}
	before, err := h.Handle(context.Background(), "read", platform.Raw(read))
	if err != nil {
		t.Fatal(err)
	}
	again, err := h.Handle(context.Background(), "read", platform.Raw(read))
	if err != nil || !reflect.DeepEqual(before, again) {
		t.Fatal("fixed times and seed did not reproduce the same window")
	}
	command := platform.DriverExecute{Session: in.Session, Dispatch: platform.DriverDispatchIdentity{RunID: "run", AttemptID: "attempt-1", BusinessKey: "device", BindingRevision: "r1", Deadline: at.Add(time.Hour)}, Command: platform.DriverCommandValue{Name: "setvalue", Value: "60"}}
	answer, err := h.Handle(context.Background(), "execute", platform.Raw(command))
	if err != nil || answer.(platform.DriverExecution).Sending != "possible" || answer.(platform.DriverExecution).State != "unknown" {
		t.Fatalf("lost reply became success or a safe refusal: %+v %v", answer, err)
	}
	after, err := h.Handle(context.Background(), "read", platform.Raw(read))
	if err != nil {
		t.Fatal(err)
	}
	oldPoints, newPoints := before.(platform.DriverReadResult).Points, after.(platform.DriverReadResult).Points
	if !reflect.DeepEqual(oldPoints[:30], newPoints[:30]) || newPoints[30].Value != "60" || newPoints[len(newPoints)-1].Value != "60" {
		t.Fatal("absolute set changed earlier samples or was scaled twice")
	}
	read.Since, read.Until, read.Mode = at.Add(31*time.Second), at.Add(40*time.Second), "summary"
	latest, err := h.Handle(context.Background(), "read", platform.Raw(read))
	if err != nil || latest.(platform.DriverReadResult).Window.Last != "60" || !latest.(platform.DriverReadResult).Window.LastAt.After(at.Add(30*time.Second)) {
		t.Fatalf("readback did not expose a fresh physical value after the lost reply: %+v %v", latest, err)
	}
}

func TestSimulationQualityDoesNotBecomeZero(t *testing.T) {
	at := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)
	for _, scenario := range []string{"offline", "stale"} {
		t.Run(scenario, func(t *testing.T) {
			h := New()
			h.Now = func() time.Time { return at }
			in := simulation(scenario)
			if _, err := h.Handle(context.Background(), "open", platform.Raw(in)); err != nil {
				t.Fatal(err)
			}
			answer, err := h.Handle(context.Background(), "read", platform.Raw(platform.DriverRead{Session: in.Session}))
			if err != nil {
				t.Fatal(err)
			}
			sample := answer.(platform.DriverReadResult).Samples[0]
			if sample.Quality == "good" || scenario == "offline" && sample.Value != nil || scenario == "stale" && !sample.At.Before(at) {
				t.Fatalf("bad sample was reported as current usable data: %+v", sample)
			}
			answer, err = h.Handle(context.Background(), "read", platform.Raw(platform.DriverRead{Session: in.Session, Since: at.Add(-time.Minute), Until: at, Mode: "summary"}))
			if err != nil {
				t.Fatal(err)
			}
			result := answer.(platform.DriverReadResult)
			if result.Window.Count != 0 || result.Window.Last != "" || result.Coverage.Good != 0 || !result.Coverage.CompleteUntil.Before(at) {
				t.Fatalf("unavailable samples became a completed numeric window: %+v", result)
			}
		})
	}
}
