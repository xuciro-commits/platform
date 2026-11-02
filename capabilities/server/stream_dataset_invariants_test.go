package platformserver

import (
	"platformserver/platform"
	"platformserver/studio/build"
	"testing"
	"time"
)

func TestInvariantK1K2StreamingDatasetAcceptedRecovery(t *testing.T) {
	live := newInvariantTenant(t, "stream-accepted-invariants")
	live.accept(t, "build.connection.create", build.ConnectionType, "CONN", map[string]any{"name": "events", "title": "Events", "kind": "postgres", "address": "postgres://fixture.invalid/events?sslmode=disable"})
	live.accept(t, "build.connection.check", build.ConnectionType, "CONN", map[string]any{})
	connection, _ := platform.Get[build.Connection](live.automation(build.ID, false), "CONN")
	caller := live.automation(build.ID, false)
	checked := live.submission(build.SchemaConnectionChecked, build.ConnectionType, "CONN", map[string]any{"revision": connection.Revision, "check": build.ConnectionCheck{At: invariantClock, OK: true}})
	checked.PrincipalId = caller.ID
	live.mu.Lock()
	_, refusal := live.submitAccepted(live.app(build.ID), caller.Member, checked, invariantClock, true)
	live.mu.Unlock()
	if refusal != nil {
		t.Fatal(refusal)
	}
	live.accept(t, "build.dataset.create", build.DatasetType, "DATA", map[string]any{"name": "eventsdata", "title": "Events", "key": "position", "stream": true, "keep": 10})
	live.accept(t, "build.source.create", build.SourceType, "SOURCE", map[string]any{"name": "eventsource", "title": "Events", "connection": "CONN", "profile": "table", "entity": "events", "since": "position", "stream": true, "dataset": "DATA"})
	live.accept(t, "build.source.publish", build.SourceType, "SOURCE", map[string]any{})
	prepare := func(rows []map[string]any, cursor string, at time.Time) *build.IntegrationExecution {
		t.Helper()
		source, _ := platform.Get[build.Source](live.automation(build.ID, false), "SOURCE")
		plan := integrationPreparation{Type: build.SourceType, ID: "SOURCE", Dataset: "DATA", Transaction: "append", Producer: "source:eventsource", Data: rows}
		execution, err := live.prepareIntegration(live.member(t), source.Revision, at, plan)
		if err != nil {
			t.Fatal(err)
		}
		pull := build.SourcePull{At: at, Rows: len(rows), Cursor: cursor, DefinitionVersion: source.ExecutionVersion(), Execution: execution}
		if refusal := live.sourceResult(live.member(t), source, pull, "prepared", &source.Revision, at); refusal != nil {
			t.Fatal(refusal)
		}
		return execution
	}
	prepare([]map[string]any{{"position": float64(1), "note": "first"}}, "1", invariantClock)
	live.PullSources(invariantClock)
	second := invariantClock.Add(time.Second)
	execution := prepare([]map[string]any{{"position": float64(2), "note": "second"}}, "2", second)
	key := "source:" + execution.ID + ":dataset"
	if refusal := live.loadDatasetKey(live.member(t), "DATA", "source:eventsource", "", []map[string]any{{"position": float64(2), "note": "second"}}, key, second, "append"); refusal != nil {
		t.Fatal(refusal)
	}
	interrupted := append([]Entry{}, live.entries...)
	for replay := 0; replay < 2; replay++ {
		restored := live.restore(t, interrupted)
		restored.Files = live.files()
		restored.PullSources(second.Add(time.Second))
		rows, version, err := build.DatasetRows(restored.automation(build.ID, false), "DATA", 0)
		source, _ := platform.Get[build.Source](restored.automation(build.ID, false), "SOURCE")
		if err != nil || len(rows) != 2 || version != 2 || source.Cursor != "2" || source.Last.Execution.Pending {
			t.Fatalf("prepared recovery rows=%v version=%d source=%+v: %v", rows, version, source, err)
		}
		before := restored.recordImage(t)
		entries := len(restored.entries)
		if refusal := restored.loadDatasetKey(restored.member(t), "DATA", "source:eventsource", "", []map[string]any{{"position": float64(2), "note": "second"}}, key, second, "append"); refusal != nil {
			t.Fatal(refusal)
		}
		if len(restored.entries) != entries || len(differingTypes(before, restored.recordImage(t))) != 0 {
			t.Fatal("stream retry appended or rewrote accepted rows")
		}
	}
}
