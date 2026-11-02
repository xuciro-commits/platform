package platformserver

import (
	"encoding/json"
	"reflect"
	"testing"

	"google.golang.org/protobuf/proto"
	"platformserver/platform"
	"platformserver/studio/build"
)

func TestInvariantK1K2DatasetTransactionReplayAndIdempotency(t *testing.T) {
	live := newInvariantTenant(t, "dataset-transaction-invariants")
	live.accept(t, "build.dataset.create", build.DatasetType, "DS", map[string]any{"name": "transactions", "title": "Transactions", "key": "id", "keep": 10})
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
		submission := live.submission(build.SchemaDatasetLoad, build.DatasetType, "DS", map[string]any{"type": item.kind, "rows": item.rows})
		accepted, refusal := live.Submit(live.member(t), submission, invariantClock)
		if refusal != nil {
			t.Fatalf("%s refused: %v", item.kind, refusal)
		}
		rows, _, err := build.DatasetRows(live.automation(build.ID, false), "DS", 0)
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
	first, _, err := build.DatasetRows(live.automation(build.ID, false), "DS", 1)
	if err != nil || !reflect.DeepEqual(first, cases[0].want) {
		t.Fatalf("historical rows changed: %v %v", first, err)
	}
	for _, payload := range []any{
		map[string]any{"type": "append", "rows": []map[string]any{{"id": "A"}}},
		map[string]any{"type": "update", "rows": []map[string]any{{"note": "no key"}}},
		map[string]any{"type": "snapshot", "rows": []map[string]any{{"id": "A"}, {"id": "A"}}},
	} {
		if _, refusal := live.Submit(live.member(t), live.submission(build.SchemaDatasetLoad, build.DatasetType, "DS", payload), invariantClock); refusal == nil {
			t.Fatalf("invalid key transaction accepted: %v", payload)
		}
	}
	live.accept(t, "build.dataset.create", build.DatasetType, "UNKEYED", map[string]any{"name": "unkeyed", "title": "Unkeyed"})
	live.accept(t, build.SchemaDatasetLoad, build.DatasetType, "UNKEYED", map[string]any{"rows": []map[string]any{{"id": "A"}, {"id": "A"}}})
	if _, refusal := live.Submit(live.member(t), live.submission("build.dataset.edit", build.DatasetType, "UNKEYED", map[string]any{"key": "id"}), invariantClock); refusal == nil {
		t.Fatal("a key was configured over duplicate existing values")
	}
	for _, kind := range []string{"update", "delete"} {
		if _, refusal := live.Submit(live.member(t), live.submission(build.SchemaDatasetLoad, build.DatasetType, "UNKEYED", map[string]any{"type": kind, "rows": []map[string]any{{"id": "A"}}}), invariantClock); refusal == nil {
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
	version, _ := platform.Get[build.DatasetVersion](live.automation(build.ID, false), build.VersionID("DS", 4))
	var retained []map[string]any
	if json.Unmarshal(version.Data, &retained) != nil || !reflect.DeepEqual(retained, cases[3].want) {
		t.Fatal("accepted final snapshot differs")
	}
}
