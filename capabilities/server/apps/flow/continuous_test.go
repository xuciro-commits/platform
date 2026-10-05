package flow

import (
	"encoding/json"
	"testing"
	"time"

	"platformserver/platform"
)

// Continuous Flow: admission, watermark, dead letters and the batch frame
// (ADR-0047 §13, plan A).

func TestContinuousBatchAdmissionAndFolding(t *testing.T) {
	declared := platform.Continuous{Source: "k8.meters", Batch: 3, DeadLetter: true}
	frame := &BatchFrame{State: map[string]json.RawMessage{}}
	now := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)

	// The first batch is admitted, folded, and becomes the cursor.
	first := Batch{ID: "b1", Signals: []Signal{
		{Key: "hotel-a", At: now, Value: json.RawMessage(`4`)},
		{Key: "hotel-b", At: now.Add(time.Minute), Value: json.RawMessage(`6`)},
	}}
	if err := admitBatch(frame, declared, first); err != nil {
		t.Fatalf("the first batch was refused: %v", err)
	}
	if folded, rejected := foldBatch(frame, declared, first); folded != 2 || rejected != 0 {
		t.Fatalf("first batch: folded=%d rejected=%d", folded, rejected)
	}
	if frame.Cursor != "b1" || frame.Consumed != 2 || frame.Watermark != now.Add(time.Minute) {
		t.Fatalf("frame after the first batch: %+v", frame)
	}
	var window map[string]any
	if err := json.Unmarshal(frame.State["window"], &window); err != nil {
		t.Fatalf("window state: %v", err)
	}
	if window["count"].(float64) != 2 || window["sum"].(float64) != 10 {
		t.Fatalf("window folded wrongly: %v", window)
	}

	// The same batch delivered twice is idempotent: the cursor stands.
	if err := admitBatch(frame, declared, first); err != nil {
		t.Fatalf("a repeated batch was refused: %v", err)
	}
	// A gap or an old generation cannot advance the cursor.
	if err := admitBatch(frame, declared, Batch{ID: "b3", Predecessor: "b2"}); err == nil {
		t.Fatal("a batch with the wrong predecessor advanced the flow")
	}
	// A batch over the declared budget is refused, visibly, not trimmed.
	if err := admitBatch(frame, declared, Batch{ID: "b2", Predecessor: "b1", Signals: make([]Signal, 4)}); err == nil {
		t.Fatal("an over-budget batch was admitted")
	}
	// Late and timeless signals become dead letters; the rest folds.
	second := Batch{ID: "b2", Predecessor: "b1", Signals: []Signal{
		{Key: "hotel-a", At: now.Add(30 * time.Second), Value: json.RawMessage(`1`)},
		{Key: "hotel-b", Value: json.RawMessage(`2`)},
		{Key: "hotel-c", At: now.Add(2 * time.Minute), Value: json.RawMessage(`3`)},
	}}
	if err := admitBatch(frame, declared, second); err != nil {
		t.Fatalf("the successor batch was refused: %v", err)
	}
	folded, rejected := foldBatch(frame, declared, second)
	if folded != 1 || rejected != 2 || len(frame.DeadLetters) != 2 {
		t.Fatalf("second batch: folded=%d rejected=%d dead=%d", folded, rejected, len(frame.DeadLetters))
	}
	if frame.Cursor != "b2" || frame.Consumed != 3 || frame.Watermark != now.Add(2*time.Minute) {
		t.Fatalf("frame after the second batch: %+v", frame)
	}
	if frame.DeadLetters[0].Reason != "late: before the watermark" || frame.DeadLetters[1].Reason != "no event time" {
		t.Fatalf("dead letters: %+v", frame.DeadLetters)
	}
	if stateSize(frame.State) == 0 {
		t.Fatal("the frame keeps no state")
	}

	// Folding the same signals again under a fresh frame name does not lose the
	// count: state is only ever added through foldBatch.
	before := frame.Consumed
	foldBatch(frame, declared, Batch{ID: "b4", Signals: []Signal{{Key: "hotel-a", At: now.Add(3 * time.Minute), Value: json.RawMessage(`5`)}}})
	if frame.Consumed != before+1 {
		t.Fatalf("consumed: %d want %d", frame.Consumed, before+1)
	}
}
