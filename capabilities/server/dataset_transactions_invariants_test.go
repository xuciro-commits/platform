package platformserver

import (
	"testing"

	"google.golang.org/protobuf/proto"
	"platformserver/studio/build"
)

func TestInvariantK1K2DatasetTransactionReplayAndIdempotency(t *testing.T) {
	live := newInvariantTenant(t, "dataset-transaction-invariants")
	live.accept(t, "build.dataset.create", build.DatasetType, "DS", map[string]any{"name": "transactions", "title": "Transactions", "keep": 3})
	for _, typ := range []build.DatasetTransactionType{
		"", build.DatasetTransactionSnapshot, build.DatasetTransactionAppend,
		build.DatasetTransactionUpdate, build.DatasetTransactionDelete,
	} {
		payload := map[string]any{"rows": []map[string]any{{"id": "A", "note": "retained"}}}
		if typ != "" {
			payload["type"] = typ
		}
		submission := live.submission(build.SchemaDatasetLoad, build.DatasetType, "DS", payload)
		accepted, refusal := live.Submit(live.member(t), submission, invariantClock)
		if refusal != nil {
			t.Fatalf("transaction %q refused: %v", typ, refusal)
		}
		entries := len(live.entries)
		image := live.recordImage(t)
		retried, refusal := live.Submit(live.member(t), submission, invariantClock)
		if refusal != nil || !proto.Equal(accepted, retried) || len(live.entries) != entries {
			t.Fatalf("transaction %q retry changed the answer or accepted log: refusal=%v entries=%d; want %d", typ, refusal, len(live.entries), entries)
		}
		if differing := differingTypes(image, live.recordImage(t)); len(differing) != 0 {
			t.Fatalf("transaction %q retry changed record bytes in %v", typ, differing)
		}
	}
	want := live.recordImage(t)
	for replay := 1; replay <= 2; replay++ {
		restored := live.restore(t, live.entries)
		if differing := differingTypes(want, restored.recordImage(t)); len(differing) != 0 {
			t.Fatalf("dataset transaction replay %d differs from the live record store in %v", replay, differing)
		}
	}
}
