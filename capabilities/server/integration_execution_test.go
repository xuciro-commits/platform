package platformserver

import resources "platformserver/engine/resources"

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	"platformserver/platform"
	"platformserver/studio/build"
)

func TestIntegrationSourceAcceptsReturningValuesAndRetainsRefusals(t *testing.T) {
	tn := builderTenant(t, "source-events")
	at := time.Date(2026, 10, 19, 20, 0, 0, 0, time.UTC)
	typ := publishObject(t, tn, "builder", "OBJECT", map[string]any{
		"name": "intake", "title": "Intake", "fields": []map[string]any{{"name": "note", "title": "Note", "type": "text", "required": true}},
	}, at)
	decide(t, tn, "builder", build.ID, build.SourceType+".create", build.SourceType, "SOURCE", map[string]any{
		"name": "feed", "title": "Feed", "url": "https://feed.example/rows", "object": typ, "key": "id", "since": "seq", "mapping": []build.SourceField{{From: "value", To: "note"}},
	}, at)
	decide(t, tn, "builder", build.ID, build.SourceType+".publish", build.SourceType, "SOURCE", map[string]any{}, at)
	var body string
	tn.Outbound = func(*http.Request, bool) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	}
	var previous string
	for n, value := range []string{"A", "B", "A"} {
		decide(t, tn, "builder", build.ID, build.SourceType+".pull", build.SourceType, "SOURCE", map[string]any{}, at)
		body = fmt.Sprintf(`[{"id":"R1","value":%q,"seq":%d}]`, value, n+1)
		tn.PullSources(at.Add(time.Duration(n) * time.Minute))
		source, _ := platform.Get[build.Source](tn.automation(build.ID, false), "SOURCE")
		if source.Last == nil || source.Last.Execution == nil || source.Last.Execution.Pending || source.Last.Applied != 1 || source.Last.Failed != 0 {
			t.Fatalf("input %d did not complete: %+v", n, source.Last)
		}
		if source.Last.Execution.ID == previous {
			t.Fatal("a new occurrence reused an earlier content identity")
		}
		previous = source.Last.Execution.ID
		row := tn.records.types[typ].rows.Get("R1")
		if row == nil || len(row.RecordHistory()) != n+1 {
			t.Fatalf("returning value was lost: history after %q", value)
		}
		var current map[string]any
		if err := json.Unmarshal(platform.Raw(row.Value.Interface()), &current); err != nil || current["note"] != value {
			t.Fatalf("current value after %q: %+v %v", value, current, err)
		}
	}
	// A durable business refusal finishes the attempt, but not the success cursor.
	decide(t, tn, "builder", build.ID, build.SourceType+".pull", build.SourceType, "SOURCE", map[string]any{}, at)
	body = `[{"id":"R1","value":"","seq":4}]`
	tn.PullSources(at.Add(4 * time.Minute))
	source, _ := platform.Get[build.Source](tn.automation(build.ID, false), "SOURCE")
	if source.Cursor != "3" || source.Last.Failed != 1 || source.Last.Execution.Pending {
		t.Fatalf("refused row advanced the cursor or stayed indefinitely pending: %+v", source)
	}
}

func TestIntegrationSourceResumesFrozenInputAfterRestart(t *testing.T) {
	for _, backend := range journalsUnderTest(t) {
		t.Run(backend.name, func(t *testing.T) {
			ctx := context.Background()
			id := fmt.Sprintf("source-resume-%d", time.Now().UnixNano())
			t.Cleanup(func() { backend.cleanup(t, id) })
			files, err := resources.NewLocalFiles(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			compose := func() *Tenant {
				tenant := builderTenant(t, id)
				tenant.Files = files
				return tenant
			}
			store := backend.open(t, id)
			live := compose()
			if _, err := store.Entries(ctx, id, 0); err != nil {
				t.Fatal(err)
			}
			live.attachJournal(ctx, store)
			at := time.Date(2026, 10, 19, 21, 0, 0, 0, time.UTC)
			typ := publishObject(t, live, "builder", "OBJECT", map[string]any{
				"name": "intake", "title": "Intake", "fields": []map[string]any{{"name": "note", "title": "Note", "type": "text"}},
			}, at)
			decide(t, live, "builder", build.ID, build.SourceType+".create", build.SourceType, "SOURCE", map[string]any{
				"name": "feed", "title": "Feed", "url": "https://feed.example/rows", "object": typ, "key": "id", "since": "seq", "mapping": []build.SourceField{{From: "value", To: "note"}},
			}, at)
			decide(t, live, "builder", build.ID, build.SourceType+".publish", build.SourceType, "SOURCE", map[string]any{}, at)
			fetches := 0
			live.Outbound = func(*http.Request, bool) (*http.Response, error) {
				fetches++
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`[{"id":"R1","value":"first","seq":1},{"id":"R2","value":"second","seq":2}]`))}, nil
			}
			persist := live.AcceptResult
			live.AcceptResult = func(e Entry, key, hash string) ([]byte, error) {
				if strings.HasPrefix(key, "integration:") && strings.HasSuffix(key, ":row:1") {
					return nil, errors.New("second row could not be appended")
				}
				return persist(e, key, hash)
			}
			decide(t, live, "builder", build.ID, build.SourceType+".pull", build.SourceType, "SOURCE", map[string]any{}, at)
			live.PullSources(at)
			pending, _ := platform.Get[build.Source](live.automation(build.ID, false), "SOURCE")
			if pending.Last == nil || pending.Last.Execution == nil || !pending.Last.Execution.Pending || pending.Cursor != "" || live.records.types[typ].rows.Len() != 1 {
				t.Fatalf("partial attempt did not retain its input: %+v", pending.Last)
			}
			execution := pending.Last.Execution.ID
			store.Close()
			store = backend.reopen(t, id)
			defer store.Close()
			restored := compose()
			deployment := Deployment{}
			if err := deployment.restoreTenants(ctx, store, "test-build", []*Tenant{restored}, map[string]int64{}, map[string]bool{}); err != nil || restored.quarantined() {
				t.Fatalf("restart: %v %+v", err, restored.fault.Load())
			}
			restored.Outbound = func(*http.Request, bool) (*http.Response, error) {
				t.Fatal("resuming a saved input fetched a different external response")
				return nil, errors.New("unexpected fetch")
			}
			restored.PullSources(at.Add(time.Hour))
			done, _ := platform.Get[build.Source](restored.automation(build.ID, false), "SOURCE")
			if done.Last.Execution.ID != execution || done.Last.Execution.Pending || done.Last.Applied != 2 || done.Cursor != "2" || fetches != 1 {
				t.Fatalf("restart did not finish the original input: %+v", done.Last)
			}
			for _, rowID := range []string{"R1", "R2"} {
				if row := restored.records.types[typ].rows.Get(rowID); row == nil || len(row.RecordHistory()) != 1 {
					t.Fatalf("row %s was lost or applied twice", rowID)
				}
			}
		})
	}
}

func TestIntegrationPipelineRunsWhenLookupVersionChanges(t *testing.T) {
	tn := builderTenant(t, "pipeline-inputs")
	at := time.Date(2026, 10, 19, 22, 0, 0, 0, time.UTC)
	for _, id := range []string{"raw", "labels", "out"} {
		decide(t, tn, "builder", build.ID, build.DatasetType+".create", build.DatasetType, id, map[string]any{"name": id, "title": id}, at)
	}
	load := func(id, note string) {
		t.Helper()
		rows := []map[string]any{{"id": "one", "ref": "label", "note": note}}
		if id == "labels" {
			rows = []map[string]any{{"id": "label", "note": note}}
		}
		decide(t, tn, "builder", build.ID, build.SchemaDatasetLoad, build.DatasetType, id, map[string]any{"rows": rows}, at)
	}
	load("raw", "original")
	load("labels", "A")
	decide(t, tn, "builder", build.ID, build.PipelineType+".create", build.PipelineType, "PIPE", map[string]any{
		"name": "enrich", "title": "Enrich", "input": "raw", "outputDataset": "out",
		"steps": []platform.TransformStep{{Kind: "lookup", Dataset: "labels", Column: "ref", Match: "id", Columns: []string{"note"}}},
	}, at)
	decide(t, tn, "builder", build.ID, build.PipelineType+".publish", build.PipelineType, "PIPE", map[string]any{}, at)
	decide(t, tn, "builder", build.ID, build.PipelineType+".run", build.PipelineType, "PIPE", map[string]any{}, at)
	previous := ""
	for n, note := range []string{"A", "B", "A"} {
		if n > 0 {
			load("labels", note)
		}
		tn.RunPipelines(at.Add(time.Duration(n) * time.Minute))
		pipeline, _ := platform.Get[build.Pipeline](tn.automation(build.ID, false), "PIPE")
		rows, version, err := build.DatasetRows(tn.automation(build.ID, false), "out", 0)
		if err != nil || version != n+1 || len(rows) != 1 || rows[0]["note"] != note || pipeline.Last.Input != 1 || pipeline.Last.Output != version || pipeline.Last.Execution.Pending || pipeline.Last.Execution.Inputs["labels"] != n+1 {
			t.Fatalf("lookup input %d did not produce its own output: %+v %+v %v", n, pipeline.Last, rows, err)
		}
		if pipeline.Last.Execution.ID == previous {
			t.Fatal("lookup-only change reused a prior execution")
		}
		previous = pipeline.Last.Execution.ID
		tn.RunPipelines(at.Add(time.Duration(n)*time.Minute + time.Second))
		_, unchanged, _ := build.DatasetRows(tn.automation(build.ID, false), "out", 0)
		if unchanged != version {
			t.Fatal("an unchanged input set started another execution")
		}
	}
}

func TestIntegrationPipelineRecoversItsOriginalOutputVersion(t *testing.T) {
	ctx := context.Background()
	id := "pipeline-resume"
	files, err := resources.NewLocalFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	store, err := OpenFileJournal(directory)
	if err != nil {
		t.Fatal(err)
	}
	compose := func() *Tenant {
		tenant := builderTenant(t, id)
		tenant.Files = files
		return tenant
	}
	tn := compose()
	if _, err := store.Entries(ctx, id, 0); err != nil {
		t.Fatal(err)
	}
	tn.attachJournal(ctx, store)
	at := time.Date(2026, 10, 19, 23, 0, 0, 0, time.UTC)
	for _, dataset := range []string{"raw", "out"} {
		decide(t, tn, "builder", build.ID, build.DatasetType+".create", build.DatasetType, dataset, map[string]any{"name": dataset, "title": dataset, "keep": 1}, at)
	}
	load := func(tenant *Tenant, dataset, note string) {
		t.Helper()
		decide(t, tenant, "builder", build.ID, build.SchemaDatasetLoad, build.DatasetType, dataset, map[string]any{"rows": []map[string]any{{"note": note}}}, at)
	}
	load(tn, "raw", "original")
	decide(t, tn, "builder", build.ID, build.PipelineType+".create", build.PipelineType, "PIPE", map[string]any{
		"name": "transfer", "title": "Transfer", "input": "raw", "outputDataset": "out",
	}, at)
	decide(t, tn, "builder", build.ID, build.PipelineType+".publish", build.PipelineType, "PIPE", map[string]any{}, at)
	persist := tn.AcceptResult
	tn.AcceptResult = func(e Entry, key, hash string) ([]byte, error) {
		if strings.HasPrefix(key, "pipeline:") && strings.HasSuffix(key, ":finished") {
			return nil, errors.New("summary append failed after output was accepted")
		}
		return persist(e, key, hash)
	}
	decide(t, tn, "builder", build.ID, build.PipelineType+".run", build.PipelineType, "PIPE", map[string]any{}, at)
	tn.RunPipelines(at)
	pending, _ := platform.Get[build.Pipeline](tn.automation(build.ID, false), "PIPE")
	if pending.Last == nil || pending.Last.Execution == nil || !pending.Last.Execution.Pending {
		t.Fatalf("output without a final summary lost its execution: %+v", pending.Last)
	}
	execution := pending.Last.Execution.ID
	load(tn, "out", "another producer")
	load(tn, "raw", "new input") // even retention of the original input cannot change this attempt
	decide(t, tn, "builder", build.ID, build.PipelineType+".edit", build.PipelineType, "PIPE", map[string]any{
		"steps": []platform.TransformStep{{Kind: "compute", To: "tag", Formula: "1"}},
	}, at)
	decide(t, tn, "builder", build.ID, build.PipelineType+".publish", build.PipelineType, "PIPE", map[string]any{}, at)
	store.Close()
	store, err = OpenFileJournal(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	restored := compose()
	if err := (&Deployment{}).restoreTenants(ctx, store, "test-build", []*Tenant{restored}, map[string]int64{}, map[string]bool{}); err != nil || restored.quarantined() {
		t.Fatalf("restart: %v %+v", err, restored.fault.Load())
	}
	restored.RunPipelines(at.Add(time.Hour))
	done, _ := platform.Get[build.Pipeline](restored.automation(build.ID, false), "PIPE")
	rows, version, err := build.DatasetRows(restored.automation(build.ID, false), "out", 0)
	if err != nil || version != 2 || rows[0]["note"] != "another producer" || done.Last.Execution.ID != execution || done.Last.Execution.Pending || done.Last.Input != 1 || done.Last.Output != 1 {
		t.Fatalf("resumed attempt reloaded inputs, rewrote output, or reported a later version: %+v %+v %d %v", done.Last, rows, version, err)
	}
	restored.RunPipelines(at.Add(2 * time.Hour))
	rows, version, err = build.DatasetRows(restored.automation(build.ID, false), "out", 0)
	if err != nil || version != 3 || rows[0]["note"] != "new input" || rows[0]["tag"] != float64(1) {
		t.Fatalf("the next input version did not run independently: %+v %d %v", rows, version, err)
	}
}

// Legacy submission replay reproduces its original record image. New accepted
// results retain the request key used to resume an integration after a restart.
func TestIntegrationDatasetLegacyReplayKeepsOriginalImage(t *testing.T) {
	compose := func() *Tenant { return builderTenant(t, "dataset-legacy-request") }
	live := compose()
	member := memberOf(t, live, "builder")
	at := time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)
	var entries []Entry
	for _, step := range []struct {
		schema  string
		payload any
	}{
		{build.DatasetType + ".create", map[string]any{"name": "historical", "title": "Historical rows"}},
		{build.SchemaDatasetLoad, map[string]any{"rows": []map[string]any{{"note": "original"}}, "producer": "legacy"}},
	} {
		request := submission(live, member, build.ID, step.schema, build.DatasetType, "DATASET", step.payload)
		raw, err := protojson.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, Entry{App: build.ID, Kind: "submission", Principal: platform.Raw(member), Body: raw, At: at})
	}
	if err := live.Replay(entries); err != nil {
		t.Fatal(err)
	}
	original, ok := live.Held(build.DatasetVersionType + "/DATASET@1")
	if !ok {
		t.Fatal("legacy dataset version was not restored")
	}
	originalImage := platform.Raw(original)
	if original.(build.DatasetVersion).Request != "" || bytes.Contains(originalImage, []byte(`"requestKey"`)) {
		t.Fatalf("legacy replay added request metadata to the original image: %s", originalImage)
	}
	live.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) {
		entries = append(entries, entry)
		return entry.Body, nil
	}
	request := submission(live, member, build.ID, build.SchemaDatasetLoad, build.DatasetType, "DATASET", map[string]any{"rows": []map[string]any{{"note": "new"}}, "producer": "current"})
	if _, refusal := live.Submit(member, request, at.Add(time.Minute)); refusal != nil {
		t.Fatal(refusal)
	}
	current, ok := live.Held(build.DatasetVersionType + "/DATASET@2")
	if !ok || current.(build.DatasetVersion).Request != request.GetIdempotencyKey() {
		t.Fatal("new accepted version did not retain its request identity")
	}
	CheckReplay(t, live, entries, compose)
	restored := compose()
	if err := restored.Replay(entries); err != nil {
		t.Fatal(err)
	}
	old, ok := restored.Held(build.DatasetVersionType + "/DATASET@1")
	if !ok || !bytes.Equal(platform.Raw(old), originalImage) {
		t.Fatal("mixed replay changed the original legacy image")
	}
	kept, ok := restored.Held(build.DatasetVersionType + "/DATASET@2")
	if !ok || kept.(build.DatasetVersion).Request != request.GetIdempotencyKey() {
		t.Fatal("pure result application lost the saved request identity")
	}
}

func TestPostgresIntegrationPreparationUsesCurrentReadScope(t *testing.T) {
	dsn := os.Getenv("PLATFORM_TEST_DATABASE")
	if dsn == "" {
		t.Skip("PLATFORM_TEST_DATABASE not set")
	}
	ctx := context.Background()
	id := fmt.Sprintf("integration-prepare-%d", time.Now().UnixNano())
	j, err := OpenJournal(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer forgetJournal(t, j, id)
	live := builderTenant(t, id)
	if _, err = j.Entries(ctx, id, 0); err != nil {
		t.Fatal(err)
	}
	live.attachJournal(ctx, j)
	typ := publishObject(t, live, "builder", "OBJECT", map[string]any{"name": "intake", "title": "Intake", "fields": []map[string]any{{"name": "note", "title": "Note", "type": "text"}}}, txnAt)
	decide(t, live, "builder", build.ID, typ+".create", typ, "R1", map[string]any{"note": "Existing"}, txnAt)
	if err = live.migrateRuntimeState(ctx, j); err != nil {
		t.Fatal(err)
	}
	decide(t, live, "builder", build.ID, "build.source.create", build.SourceType, "SOURCE", map[string]any{"name": "feed", "title": "Feed", "url": "https://feed.example/rows", "object": typ, "key": "id", "mapping": []build.SourceField{{From: "value", To: "note"}}}, txnAt)
	decide(t, live, "builder", build.ID, "build.source.publish", build.SourceType, "SOURCE", map[string]any{}, txnAt)
	live.Outbound = func(*http.Request, bool) (*http.Response, error) {
		if live.records.current.View() != nil {
			t.Fatal("Source I/O retained its preparation read view")
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`[{"id":"R1","value":"Updated"}]`))}, nil
	}
	decide(t, live, "builder", build.ID, build.SourceType+".pull", build.SourceType, "SOURCE", map[string]any{}, txnAt)
	live.PullSources(txnAt)
	view, problem := live.RecordOf(memberOf(t, live, "builder"), build.SourceType, "SOURCE", txnAt)
	if problem != nil {
		t.Fatal(problem)
	}
	source := view.Record.(build.Source)
	if source.Last == nil || source.Last.Applied != 1 || source.Last.Error != "" {
		t.Fatalf("SQL Source did not execute: %+v", source.Last)
	}
	rows := []build.SourceRow{{ID: "R1"}, {ID: "R2"}}
	merged, refusal := live.prepareRows(memberOf(t, live, "builder"), typ, "feed", rows, txnAt)
	if refusal != nil || merged != 0 || rows[0].Verb != ".edit" || rows[1].Verb != ".create" {
		t.Fatal("preparation did not read current SQL rows", rows, merged, refusal)
	}
	if live.records.current.View() != nil || live.records.types[typ].rows.Stored() != 0 {
		t.Fatal("preparation retained a SQL view or current row")
	}
}
