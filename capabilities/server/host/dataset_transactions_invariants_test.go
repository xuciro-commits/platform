package host

import (
	"encoding/json"
	"io"
	"net/http"
	dataowner "platformserver/services/data"
	"reflect"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"platformserver/platform"
	"platformserver/services/resources/authoring"
)

func TestInvariantK1K2DatasetTransactionReplayAndIdempotency(t *testing.T) {
	live := newInvariantTenant(t, "dataset-transaction-invariants")
	live.accept(t, "build.dataset.create", dataowner.DatasetType, "DS", map[string]any{"name": "transactions", "title": "Transactions", "key": "id", "keep": 10})
	cases := []struct {
		kind       string
		rows, want []map[string]any
	}{
		{"snapshot", []map[string]any{{"id": "A", "note": "retained", "count": float64(1)}}, []map[string]any{{"id": "A", "note": "retained", "count": float64(1)}}},
		{"append", []map[string]any{{"id": "B", "note": "second"}}, []map[string]any{{"id": "A", "note": "retained", "count": float64(1)}, {"id": "B", "note": "second"}}},
		{"update", []map[string]any{{"id": "A", "count": float64(2)}, {"id": "C", "note": "inserted"}}, []map[string]any{{"id": "A", "note": "retained", "count": float64(2)}, {"id": "B", "note": "second"}, {"id": "C", "note": "inserted"}}},
		{"delete", []map[string]any{{"id": "B"}}, []map[string]any{{"id": "A", "note": "retained", "count": float64(2)}, {"id": "C", "note": "inserted"}}},
	}
	for _, item := range cases {
		submission := live.submission(dataowner.SchemaDatasetLoad, dataowner.DatasetType, "DS", map[string]any{"type": item.kind, "rows": item.rows})
		accepted, refusal := live.Submit(live.member(t), submission, invariantClock)
		if refusal != nil {
			t.Fatalf("%s refused: %v", item.kind, refusal)
		}
		rows, _, err := dataowner.DatasetRows(live.automation(build.ID, false), "DS", 0)
		if err != nil || !reflect.DeepEqual(rows, item.want) {
			t.Fatalf("%s rows=%v, want=%v; err=%v", item.kind, rows, item.want, err)
		}
		entries := len(live.entries)
		image := live.recordImage(t)
		retried, refusal := live.Submit(live.member(t), submission, invariantClock)
		if refusal != nil || !proto.Equal(accepted, retried) || len(live.entries) != entries {
			t.Fatalf("%s retry changed accepted log: %v", item.kind, refusal)
		}
		if differing := differingTypes(image, live.recordImage(t)); len(differing) != 0 {
			t.Fatalf("retry changed %v", differing)
		}
	}
	first, _, err := dataowner.DatasetRows(live.automation(build.ID, false), "DS", 1)
	if err != nil || !reflect.DeepEqual(first, cases[0].want) {
		t.Fatalf("historical rows changed: %v %v", first, err)
	}
	for _, payload := range []any{
		map[string]any{"type": "append", "rows": []map[string]any{{"id": "A"}}},
		map[string]any{"type": "update", "rows": []map[string]any{{"note": "no key"}}},
		map[string]any{"type": "snapshot", "rows": []map[string]any{{"id": "A"}, {"id": "A"}}},
	} {
		if _, refusal := live.Submit(live.member(t), live.submission(dataowner.SchemaDatasetLoad, dataowner.DatasetType, "DS", payload), invariantClock); refusal == nil {
			t.Fatalf("invalid key transaction accepted: %v", payload)
		}
	}
	live.accept(t, "build.dataset.create", dataowner.DatasetType, "UNKEYED", map[string]any{"name": "unkeyed", "title": "Unkeyed"})
	live.accept(t, dataowner.SchemaDatasetLoad, dataowner.DatasetType, "UNKEYED", map[string]any{"rows": []map[string]any{{"id": "A"}, {"id": "A"}}})
	if _, refusal := live.Submit(live.member(t), live.submission("build.dataset.edit", dataowner.DatasetType, "UNKEYED", map[string]any{"key": "id"}), invariantClock); refusal == nil {
		t.Fatal("a key was configured over duplicate existing values")
	}
	for _, kind := range []string{"update", "delete"} {
		if _, refusal := live.Submit(live.member(t), live.submission(dataowner.SchemaDatasetLoad, dataowner.DatasetType, "UNKEYED", map[string]any{"type": kind, "rows": []map[string]any{{"id": "A"}}}), invariantClock); refusal == nil {
			t.Fatalf("%s without configured key accepted", kind)
		}
	}
	want := live.recordImage(t)
	for replay := 1; replay <= 2; replay++ {
		restored := live.restore(t, live.entries)
		if differing := differingTypes(want, restored.recordImage(t)); len(differing) != 0 {
			t.Fatalf("replay %d differs in %v", replay, differing)
		}
	}
	version, _ := platform.Get[dataowner.DatasetVersion](live.automation(build.ID, false), dataowner.VersionID("DS", 4))
	var retained []map[string]any
	if json.Unmarshal(version.Data, &retained) != nil || !reflect.DeepEqual(retained, cases[3].want) {
		t.Fatal("accepted final snapshot differs")
	}
}

func TestInvariantK1K2SourceSnapshotMergeAndDeletion(t *testing.T) {
	for _, mapping := range []string{"declared", "legacy"} {
		t.Run(mapping, func(t *testing.T) {
			live := newInvariantTenant(t, "source-snapshot-merge-"+mapping)
			live.accept(t, "build.dataset.create", dataowner.DatasetType, "INPUT", map[string]any{"name": "incoming", "title": "Incoming", "key": "id", "keep": 10})
			sourceSpec := map[string]any{"name": "external", "title": "External", "url": "https://upstream.example.test/rows", "object": "build.merged"}
			pipelineSpec := map[string]any{"name": "incomingpipe", "title": "Incoming pipeline", "input": "INPUT", "outputObject": "build.merged"}
			if mapping == "legacy" {
				sourceSpec["key"], sourceSpec["mapping"] = "id", []dataowner.SourceField{{From: "note", To: "note"}}
				pipelineSpec["key"] = "id"
			}
			live.accept(t, "build.source.create", dataowner.SourceType, "SOURCE", sourceSpec)
			live.accept(t, "build.pipeline.create", dataowner.PipelineType, "PIPE", pipelineSpec)
			object := invariantObject("merged", "Merged")
			object["datasources"] = []platform.DataSourceBinding{
				{Kind: "source", Source: "SOURCE", Key: "id", Columns: []platform.DataSourceColumn{{Column: "note", Attribute: "note"}}},
				{Kind: "pipeline", Source: "PIPE", Key: "id", Columns: []platform.DataSourceColumn{{Column: "note", Attribute: "note"}}},
			}
			if mapping == "legacy" {
				delete(object, "datasources")
			}
			live.accept(t, "build.object.create", build.ObjectType, "OBJECT", object)
			live.accept(t, "build.object.publish", build.ObjectType, "OBJECT", map[string]any{})
			live.accept(t, "build.source.publish", dataowner.SourceType, "SOURCE", map[string]any{})
			live.accept(t, "build.pipeline.publish", dataowner.PipelineType, "PIPE", map[string]any{})
			live.accept(t, "build.merged.create", "build.merged", "D", map[string]any{"note": "created by action"})
			rows := `[{"id":"A","note":"external A"},{"id":"B","note":"external B"},{"id":"C","note":"external C"},{"id":"D","note":"external D"}]`
			live.Outbound = func(*http.Request, bool) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(rows))}, nil
			}
			pull := func(at time.Time) dataowner.Source {
				t.Helper()
				live.accept(t, "build.source.pull", dataowner.SourceType, "SOURCE", map[string]any{})
				live.PullSources(at)
				source, _ := platform.Get[dataowner.Source](live.automation(build.ID, false), "SOURCE")
				if source.Last == nil || source.Last.Error != "" || source.Last.Failed != 0 || source.Last.Execution.Pending {
					t.Fatalf("source failed: %+v", source.Last)
				}
				return source
			}
			check := func(id, note string, archived bool) {
				t.Helper()
				view, refusal := live.RecordOf(t.Context(), live.member(t), "build.merged", id, invariantClock)
				if refusal != nil {
					t.Fatal(refusal)
				}
				var record struct {
					Note     string `json:"note"`
					Archived bool   `json:"archived"`
				}
				if err := json.Unmarshal(platform.Raw(view.Record), &record); err != nil || record.Note != note || record.Archived != archived {
					t.Fatalf("%s: %+v; want note=%s archived=%v: %v", id, record, note, archived, err)
				}
			}
			pull(invariantClock)
			live.accept(t, "build.merged.edit", "build.merged", "C", map[string]any{"note": "action edit"})
			live.accept(t, dataowner.SchemaDatasetLoad, dataowner.DatasetType, "INPUT", map[string]any{"rows": []map[string]any{{"id": "B", "note": "pipeline B"}}})
			live.accept(t, "build.pipeline.run", dataowner.PipelineType, "PIPE", map[string]any{})
			live.RunPipelines(invariantClock.Add(time.Second))
			firstPipeline, _ := platform.Get[dataowner.Pipeline](live.automation(build.ID, false), "PIPE")
			if firstPipeline.Last == nil || firstPipeline.Last.Error != "" || firstPipeline.Last.Failed != 0 || firstPipeline.Last.Written != 1 {
				t.Fatalf("first pipeline run failed: %+v", firstPipeline.Last)
			}
			check("B", "pipeline B", false)
			rows = `[{"id":"A","note":"external A"},{"id":"B","note":"latest external B"},{"id":"C","note":"new external C"},{"id":"D","note":"new external D"}]`
			pull(invariantClock.Add(2 * time.Second))
			check("B", "latest external B", false)
			rows = `[{"id":"C","note":"new external C"},{"id":"D","note":"new external D"}]`
			source := pull(invariantClock.Add(3 * time.Second))
			if source.Last.Deleted != 2 {
				t.Fatalf("source deleted=%d; want 2", source.Last.Deleted)
			}
			check("A", "external A", true)
			check("B", "pipeline B", false)
			check("C", "action edit", false)
			check("D", "created by action", false)
			rows = `[]`
			pull(invariantClock.Add(4 * time.Second))
			check("C", "action edit", true)
			check("D", "created by action", false)
			check("B", "pipeline B", false)
			live.accept(t, dataowner.SchemaDatasetLoad, dataowner.DatasetType, "INPUT", map[string]any{"rows": []map[string]any{}})
			live.RunPipelines(invariantClock.Add(5 * time.Second))
			pipeline, _ := platform.Get[dataowner.Pipeline](live.automation(build.ID, false), "PIPE")
			if pipeline.Last == nil || pipeline.Last.Error != "" || pipeline.Last.Failed != 0 || pipeline.Last.Deleted != 1 {
				t.Fatalf("pipeline deletion failed: %+v", pipeline.Last)
			}
			check("B", "pipeline B", true)
			rows = `[{"id":"A","note":"restored external A"}]`
			pull(invariantClock.Add(6 * time.Second))
			check("A", "restored external A", false)
			want := live.recordImage(t)
			for replay := 0; replay < 2; replay++ {
				restored := live.restore(t, live.entries)
				if differing := differingTypes(want, restored.recordImage(t)); len(differing) != 0 {
					t.Fatalf("source merge replay differs in %v", differing)
				}
			}
		})
	}
}
