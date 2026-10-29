package platformserver

import "platformserver/studio/build"

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"platformserver/platform"
)

func observationTenant(t *testing.T) (*Tenant, func() *Tenant, platform.AssetRef, time.Time) {
	t.Helper()
	compose := func() *Tenant {
		return composeTenant(t, "observations", []platform.Seat{seatOf("builder", "build:builder"), seatOf("operator", "build:user"), seatOf("outsider", "build:observer")}, build.New("observations"))
	}
	tn := compose()
	at := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	publishObject(t, tn, "builder", "DEVICE", map[string]any{
		"name": "device", "title": "Device", "fields": []build.Field{{Name: "device", Title: "Device", Type: "text"}, {Name: "limit", Title: "Limit", Type: "decimal"}, {Name: "latest", Title: "Latest", Type: "decimal"}, {Name: "trend", Title: "Trend", Type: "decimal"}, {Name: "samples", Title: "Sample count", Type: "integer"}},
		"states":  []build.State{{Name: "normal", Title: "Normal"}, {Name: "warning", Title: "Warning"}},
		"access":  []build.Access{{Role: "user", Read: "all", Create: true, Edit: true}, {Role: "observer", Read: "all"}},
		"actions": []build.Action{{Name: "warn", Title: "Record warning", From: []string{"normal"}, To: "warning", Roles: []string{"user"}, Inputs: []build.Input{{Name: "latest", Title: "Latest", Type: "decimal", Required: true}, {Name: "trend", Title: "Trend", Type: "decimal", Required: true}, {Name: "samples", Title: "Sample count", Type: "integer", Required: true}}, Sets: []build.Set{{Field: "latest", From: "latest"}, {Field: "trend", From: "trend"}, {Field: "samples", From: "samples"}}}},
	}, at)
	decide(t, tn, "operator", build.ID, "build.device.create", "build.device", "D1", map[string]any{"device": "boiler-1", "limit": 35}, at)
	decide(t, tn, "builder", build.ID, "build.connection.create", build.ConnectionType, "CONNECTION", map[string]any{"name": "thermalstore", "title": "External thermal store", "kind": "http", "address": "http://observation.test", "allowPrivate": true}, at)
	decide(t, tn, "builder", build.ID, "build.connection.check", build.ConnectionType, "CONNECTION", map[string]any{}, at)
	tn.Outbound = func(*http.Request, bool) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
	}
	tn.CheckConnections(at)

	ref := platform.AssetRef{App: build.ID, Kind: platform.AssetObservation, Name: "temperature"}
	decision := &platform.ObservationDecision{Action: platform.AssetRef{App: build.ID, Kind: platform.AssetAction, Name: "build.device.warn"}, When: platform.Expr{Op: "and", Kind: "boolean", Args: []platform.Expr{
		{Op: "=", Kind: "boolean", Args: []platform.Expr{platform.ExprReference("choice", "record.state"), platform.ExprLiteralValue("choice", "normal")}},
		{Op: ">", Kind: "boolean", Args: []platform.Expr{platform.ExprReference("decimal", "observation.latest"), platform.ExprReference("decimal", "record.limit")}},
		{Op: ">", Kind: "boolean", Args: []platform.Expr{{Op: "rate", Kind: "decimal", Window: &platform.ExprWindowReference{Source: "observation"}}, platform.ExprLiteralValue("decimal", "0.1")}},
	}}, Inputs: map[string]platform.Expr{"latest": platform.ExprReference("decimal", "observation.latest"), "trend": {Op: "delta", Kind: "decimal", Window: &platform.ExprWindowReference{Source: "observation"}}, "samples": {Op: "over-window", Kind: "integer", Window: &platform.ExprWindowReference{Source: "observation", Aggregate: "count"}}}}
	decide(t, tn, "builder", build.ID, "build.observation.create", build.ObservationType, "OBSERVATION", map[string]any{"name": ref.Name, "title": "Building temperature", "connection": "CONNECTION", "object": "build.device", "deviceField": "device", "signal": "temperature", "unit": "C", "path": "/observations", "windowSeconds": 120, "expectedRetentionSeconds": 86400, "expectedGranularitySeconds": 1, "roles": []string{"user", "builder"}, "decision": decision}, at)
	decide(t, tn, "builder", build.ID, build.SchemaObservation, build.ObservationType, "OBSERVATION", map[string]any{}, at)
	return tn, compose, ref, at
}

func observationSender(t *testing.T, samples *int64, calls *int, during func()) func(*http.Request, bool) (*http.Response, error) {
	t.Helper()
	return func(request *http.Request, _ bool) (*http.Response, error) {
		*calls++
		var input struct {
			Device, Signal, Mode string
			Since, Until         time.Time
			Limit                int
		}
		if request.Method != http.MethodPost || json.NewDecoder(request.Body).Decode(&input) != nil || input.Device != "boiler-1" || input.Signal != "temperature" {
			t.Fatal("storage request lost its original device/signal")
		}
		if during != nil {
			during()
		}
		window := platform.ObservationWindow{Since: input.Since, Until: input.Until, Count: *samples, FirstAt: input.Since.Add(time.Second), LastAt: input.Until.Add(-time.Second), First: "10", Last: "50", Min: "10", Max: "50", Mean: "30", Sum: fmt.Sprint(*samples * 30), Unit: "C"}
		answer := map[string]any{"window": window}
		if input.Mode == "points" {
			answer["points"] = []platform.ObservationPoint{{At: window.FirstAt, Value: "10"}, {At: window.LastAt, Value: "50"}}
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(platform.Raw(answer))), Header: make(http.Header)}, nil
	}
}

func TestObservationExternalReadResultOnlyAcceptanceAndRecovery(t *testing.T) {
	tn, compose, ref, at := observationTenant(t)
	entries := []Entry{}
	tn.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) {
		entries = append(entries, entry)
		return entry.Body, nil
	}
	count, calls := int64(1000), 0
	tn.Outbound = observationSender(t, &count, &calls, nil)
	member := memberOf(t, tn, "operator")
	window := ObservationRead{Record: "D1", Since: at.Add(-120 * time.Second), Until: at, Mode: "points", Limit: 10}
	before := snapshot(tn)
	checkpoint, _, checkpointErr := tn.Snapshot(func() int64 { return 0 })
	if checkpointErr != nil {
		t.Fatal(checkpointErr)
	}
	for _, size := range []int64{1000, 10_000_000} {
		count = size
		result, refusal := tn.ReadObservation(context.Background(), member, ref, window, at)
		if refusal != nil || len(result.Points) != 2 || result.Window.Count != size {
			t.Fatalf("external read: %+v %v", result, refusal)
		}
	}
	if len(entries) != 0 || snapshot(tn) != before {
		t.Fatal("reading millions of external observations wrote tenant state")
	}
	answer, refusal := tn.EvaluateObservation(context.Background(), member, ref, ObservationEvaluate{Record: "D1", Until: at, Key: "warning"}, at)
	if refusal != nil || !answer.Matched || answer.Receipt == nil || len(entries) != 1 {
		t.Fatalf("result decision: %+v %v entries=%d", answer, refusal, len(entries))
	}
	if bytes.Contains(entries[0].Body, []byte(`"points"`)) || len(entries[0].Body) > 100000 {
		t.Fatal("accepted result copied raw points or grew with sample count")
	}
	if !bytes.Contains(entries[0].Body, []byte(`"observation"`)) || !bytes.Contains(entries[0].Body, []byte(`"count":10000000`)) {
		t.Fatal("accepted result lost compact original window evidence")
	}
	after := snapshot(tn)
	readCalls := calls
	answer, refusal = tn.EvaluateObservation(context.Background(), member, ref, ObservationEvaluate{Record: "D1", Until: at, Key: "warning"}, at)
	if refusal != nil || !answer.Reused || calls != readCalls || snapshot(tn) != after {
		t.Fatal("accepted retry reread or changed business state")
	}
	if _, refusal = tn.EvaluateObservation(context.Background(), member, ref, ObservationEvaluate{Record: "D1", Until: at.Add(time.Second), Key: "warning"}, at); refusal == nil {
		t.Fatal("same key accepted a different window")
	}
	if _, refusal = tn.ReadObservation(context.Background(), memberOf(t, tn, "outsider"), ref, window, at); refusal == nil {
		t.Fatal("source role restriction was bypassed")
	}
	fresh := compose()
	if err := fresh.Restore(checkpoint); err != nil {
		t.Fatal(err)
	}
	fresh.Outbound = func(*http.Request, bool) (*http.Response, error) {
		t.Fatal("recovery requested external points")
		return nil, nil
	}
	if err := fresh.Replay(entries); err != nil {
		t.Fatal(err)
	}
	if snapshot(fresh) != after {
		t.Fatal("accepted observation result did not recover exactly")
	}
	got, _ := fresh.RecordOf(memberOf(t, fresh, "operator"), "build.device", "D1", at)
	if !strings.Contains(string(platform.Raw(got.Record)), `"state":"warning"`) {
		t.Fatal("warning did not reach original record")
	}
}

func TestObservationReadRevalidatesOriginalRecordAfterExternalIO(t *testing.T) {
	tn, _, ref, at := observationTenant(t)
	count, calls := int64(1000), 0
	tn.Outbound = observationSender(t, &count, &calls, func() {
		decide(t, tn, "operator", build.ID, "build.device.edit", "build.device", "D1", map[string]any{"limit": 99}, at)
	})
	_, refusal := tn.ReadObservation(context.Background(), memberOf(t, tn, "operator"), ref, ObservationRead{Record: "D1", Since: at.Add(-120 * time.Second), Until: at, Mode: "summary"}, at)
	if refusal == nil || !strings.Contains(refusal.Message, "changed") || calls != 1 {
		t.Fatalf("changed source record was accepted: %+v", refusal)
	}
}

func TestScheduledObservationSharesSourceLoopWithoutPointWrites(t *testing.T) {
	tn, _, ref, at := observationTenant(t)
	decide(t, tn, "builder", build.ID, "build.observation.edit", build.ObservationType, "OBSERVATION", map[string]any{"scheduled": true}, at)
	decide(t, tn, "builder", build.ID, build.SchemaObservation, build.ObservationType, "OBSERVATION", map[string]any{}, at)
	count, calls := int64(60000), 0
	tn.Outbound = observationSender(t, &count, &calls, nil)
	entries := []Entry{}
	tn.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) {
		entries = append(entries, entry)
		return entry.Body, nil
	}
	tn.EvaluateObservations(at)
	if calls != 1 || len(entries) != 1 {
		t.Fatalf("scheduled source did not submit one original action: calls=%d entries=%d", calls, len(entries))
	}
	tn.EvaluateObservations(at.Add(time.Second))
	if calls != 1 || len(entries) != 1 {
		t.Fatal("same completed window was scheduled twice")
	}
	tn.EvaluateObservations(at.Add(120 * time.Second))
	if calls != 2 || len(entries) != 1 {
		t.Fatal("state suppression wrote an unmatched observation window")
	}
	delete(tn.observationWindows, ref.String()+"/D1") // scheduling memory is disposable at restart
	tn.EvaluateObservations(at.Add(120 * time.Second))
	if len(entries) != 1 {
		t.Fatal("restarting the outside schedule duplicated business state")
	}
	decide(t, tn, "builder", build.ID, "build.observation.retire", build.ObservationType, "OBSERVATION", map[string]any{}, at)
	beforeCalls := calls
	_, refusal := tn.ReadObservation(context.Background(), memberOf(t, tn, "operator"), ref, ObservationRead{Record: "D1", Since: at.Add(-120 * time.Second), Until: at, Mode: "summary"}, at)
	if refusal == nil || calls != beforeCalls {
		t.Fatal("retired source kept reading external points")
	}
}

func TestObservationStorageReplacementOnlyChangesTheEnvironmentBinding(t *testing.T) {
	tn, _, ref, at := observationTenant(t)
	decide(t, tn, "builder", build.ID, "build.app.create", build.AppType, "APPLICATION", map[string]any{"name": "heating", "title": "Heating", "resources": []platform.AssetRef{ref}}, at)
	decide(t, tn, "builder", build.ID, build.SchemaHandOver, build.AppType, "APPLICATION", map[string]any{}, at)
	application, refusal := tn.ApplicationRuntimeOf(memberOf(t, tn, "builder"), platform.AssetRef{App: build.ID, Kind: platform.AssetApp, Name: "heating"}, at)
	if refusal != nil || !slices.ContainsFunc(application.Artifact.Integrations, func(binding platform.AssetBinding) bool { return binding.Ref == ref }) {
		t.Fatalf("application omitted observation integration: %+v %v", application.Artifact.Integrations, refusal)
	}
	storage := func(count int64) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte(`{}`))
				return
			}
			var q struct{ Since, Until time.Time }
			if json.NewDecoder(r.Body).Decode(&q) != nil {
				w.WriteHeader(400)
				return
			}
			window := platform.ObservationWindow{Since: q.Since, Until: q.Until, Count: count, FirstAt: q.Since.Add(time.Second), LastAt: q.Until.Add(-time.Second), First: "10", Last: "50", Min: "10", Max: "50", Mean: "30", Sum: fmt.Sprint(count * 30), Unit: "C"}
			_ = json.NewEncoder(w).Encode(map[string]any{"window": window})
		}))
	}
	first, second := storage(1000), storage(10_000_000)
	defer first.Close()
	defer second.Close()
	original, _ := platform.Get[build.Observation](tn.automation(build.ID, false), "OBSERVATION")
	tn.Outbound = nil // use real guarded HTTP against the two independent external adapters
	for index, test := range []struct {
		address string
		count   int64
	}{{first.URL, 1000}, {second.URL, 10_000_000}} {
		decide(t, tn, "builder", build.ID, "build.connection.edit", build.ConnectionType, "CONNECTION", map[string]any{"address": test.address}, at)
		decide(t, tn, "builder", build.ID, "build.connection.check", build.ConnectionType, "CONNECTION", map[string]any{}, at)
		tn.CheckConnections(at.Add(time.Duration(index+1) * time.Second))
		result, refusal := tn.ReadObservation(context.Background(), memberOf(t, tn, "operator"), ref, ObservationRead{Record: "D1", Since: at.Add(-120 * time.Second), Until: at, Mode: "summary"}, at)
		if refusal != nil || result.Window.Count != test.count {
			t.Fatalf("environment source replacement: %+v %v", result, refusal)
		}
		current, _ := platform.Get[build.Observation](tn.automation(build.ID, false), "OBSERVATION")
		if current.Published != original.Published || current.DefinitionVersion != original.DefinitionVersion {
			t.Fatal("storage replacement changed the application definition")
		}
	}
}
