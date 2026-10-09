package flow

import (
	"bytes"
	"encoding/json"
	"strings"
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
	if folded, rejected, err := foldBatch(frame, declared, first); err != nil || folded != 2 || rejected != 0 {
		t.Fatalf("first batch: folded=%d rejected=%d error=%v", folded, rejected, err)
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
	folded, rejected, err := foldBatch(frame, declared, second)
	if err != nil || folded != 1 || rejected != 2 || len(frame.DeadLetters) != 2 {
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
	if _, _, err := foldBatch(frame, declared, Batch{ID: "b4", Predecessor: frame.Cursor, Signals: []Signal{{Key: "hotel-a", At: now.Add(3 * time.Minute), Value: json.RawMessage(`5`)}}}); err != nil {
		t.Fatal(err)
	}
	if frame.Consumed != before+1 {
		t.Fatalf("consumed: %d want %d", frame.Consumed, before+1)
	}
}

func TestContinuousBatchRefusalKeepsEveryAcceptedFrameByte(t *testing.T) {
	now := time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC)
	declared := platform.Continuous{Source: "k8.meters", Batch: 512, State: 4096, DeadLetter: true}
	frame := &BatchFrame{State: map[string]json.RawMessage{}}
	first := Batch{ID: "one", Signals: []Signal{{Key: "one", At: now, Value: json.RawMessage(`4`)}}}
	if _, _, err := foldBatch(frame, declared, first); err != nil {
		t.Fatal(err)
	}
	unchanged := func(before []byte) {
		t.Helper()
		if after, _ := json.Marshal(frame); !bytes.Equal(before, after) {
			t.Fatalf("refused batch changed accepted frame: %s => %s", before, after)
		}
	}
	before, _ := json.Marshal(frame)
	if count, _, err := foldBatch(frame, declared, first); err != nil || count != 0 {
		t.Fatal("identical batch was not idempotent")
	}
	unchanged(before)
	changed := first
	changed.Signals = []Signal{{Key: "one", At: now, Value: json.RawMessage(`40`)}}
	if _, _, err := foldBatch(frame, declared, changed); err == nil {
		t.Fatal("changed bytes reused an accepted batch identity")
	}
	unchanged(before)
	declared.State = len(frame.State["window"]) + 80
	large := Batch{ID: "two", Predecessor: "one", Signals: []Signal{{Key: "small", At: now.Add(time.Second), Value: json.RawMessage(`1`)}, {Key: "large", At: now.Add(2 * time.Second), Value: platform.Raw(strings.Repeat("x", 512))}}}
	if _, _, err := foldBatch(frame, declared, large); err == nil {
		t.Fatal("state overflow silently consumed a batch")
	}
	unchanged(before)
	declared.State = 4096
	declared.FrameBytes = len(before) + 1
	late := Batch{ID: "two", Predecessor: "one", Signals: []Signal{{Key: "late", At: now.Add(-time.Second), Value: platform.Raw(strings.Repeat("x", 512))}}}
	if _, _, err := foldBatch(frame, declared, late); err == nil {
		t.Fatal("dead letters bypassed the complete frame budget")
	}
	unchanged(before)
	declared.FrameBytes = 4096
	if folded, _, err := foldBatch(frame, declared, large); err != nil || folded != 2 || frame.Cursor != "two" {
		t.Fatalf("a refused batch could not retry: %+v %v", frame, err)
	}
	var restored BatchFrame
	if err := json.Unmarshal(platform.Raw(frame), &restored); err != nil {
		t.Fatal(err)
	}
	if _, _, err := foldBatch(&restored, declared, large); err != nil || restored.Consumed != frame.Consumed {
		t.Fatalf("restored cursor refolded input: %+v %v", restored, err)
	}
}

func TestContinuousStateEncodingFailureDoesNotConsumeItsSuccessor(t *testing.T) {
	now := time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC)
	declared := platform.Continuous{Source: "k8.meters", Batch: 512, DeadLetter: true}
	for _, raw := range []json.RawMessage{json.RawMessage(`{"count":1,"sum":1e308}`), json.RawMessage(`[]`)} {
		frame := &BatchFrame{Cursor: "one", State: map[string]json.RawMessage{"window": raw}}
		before, _ := json.Marshal(frame)
		batch := Batch{ID: "two", Predecessor: "one", Signals: []Signal{{Key: "next", At: now, Value: json.RawMessage(`1e308`)}}}
		if _, _, err := foldBatch(frame, declared, batch); err == nil {
			t.Fatal("corrupt or unencodable state was consumed")
		}
		if after, _ := json.Marshal(frame); !bytes.Equal(before, after) {
			t.Fatal("state failure changed its predecessor")
		}
	}
}

func TestContinuousEventWindowRetainsTimingPartitionIdentityAndBudget(t *testing.T) {
	now := time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC)
	declared := platform.Continuous{Source: "k8.meters", Batch: 512, State: 16 << 20, FrameBytes: 17 << 20, DeadLetter: true,
		Window: &platform.StreamWindow{Node: "window", WindowMS: 30000, SlideMS: 5000, WatermarkMS: 2000, MaxRecords: 50000, LateEvents: "sideOutput"}}
	values := make([]float64, 100)
	for i := range values {
		values[i] = float64(i)
	}
	measurement := platform.Raw(map[string]any{"assetId": "A", "values": values})
	first := Batch{ID: "one", Signals: []Signal{
		{Key: "same", Partition: "plant-a", At: now.Add(-time.Second), Value: measurement},
		{Key: "same", Partition: "plant-b", At: now.Add(-10 * time.Second), Value: measurement},
	}}
	frame := &BatchFrame{State: map[string]json.RawMessage{}}
	if folded, rejected, err := foldBatch(frame, declared, first, now); err != nil || folded != 2 || rejected != 0 {
		t.Fatalf("out-of-order valid input was lost: %+v %v", frame, err)
	}
	window := func() WindowState {
		t.Helper()
		var result WindowState
		if err := json.Unmarshal(frame.State["window"], &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	state := window()
	if len(state.Rows) != 2 || state.Rows[0].Partition != "plant-b" || !bytes.Equal(state.Rows[0].Value, measurement) || !state.Ready || frame.Watermark != now {
		t.Fatalf("partition, event order or original 100 signals changed: %+v", state)
	}
	second := Batch{ID: "two", Predecessor: "one", Signals: []Signal{first.Signals[0], {Key: "late", Partition: "plant-a", At: now.Add(-31 * time.Second), Value: measurement}}}
	if folded, rejected, err := foldBatch(frame, declared, second, now.Add(time.Second)); err != nil || folded != 0 || rejected != 1 {
		t.Fatalf("duplicate/late routing: %+v %v", frame, err)
	}
	if window().Duplicate != 1 || window().Ready || len(frame.DeadLetters) != 1 || frame.DeadLetters[0].Partition != "plant-a" {
		t.Fatalf("window phase or dead-letter identity lost: %+v", frame)
	}
	before := platform.Raw(frame)
	changed := first.Signals[0]
	changed.Value = platform.Raw("different bytes")
	if _, _, err := foldBatch(frame, declared, Batch{ID: "three", Predecessor: "two", Signals: []Signal{changed}}, now.Add(2*time.Second)); err == nil {
		t.Fatal("same event identity was mutated")
	}
	if !bytes.Equal(before, platform.Raw(frame)) {
		t.Fatal("changed event altered the accepted predecessor")
	}
	// Evict the older retained row to the overflow output, while counting the
	// new record that actually entered the next window.
	declared.Window.MaxRecords = 2
	third := Batch{ID: "three", Predecessor: "two", Signals: []Signal{{Key: "new", Partition: "plant-c", At: now.Add(5 * time.Second), Value: measurement}}}
	if folded, rejected, err := foldBatch(frame, declared, third, now.Add(5*time.Second)); err != nil || folded != 1 || rejected != 1 {
		t.Fatalf("overflow consumption: folded=%d rejected=%d error=%v", folded, rejected, err)
	}
	if len(window().Rows) != 2 || !window().Ready || frame.DeadLetters[1].Key != "same" || frame.DeadLetters[1].Partition != "plant-b" {
		t.Fatalf("overflow was silent or evicted the wrong event: %+v", frame)
	}
	before = platform.Raw(frame)
	declared.State = 1
	if _, _, err := foldBatch(frame, declared, Batch{ID: "four", Predecessor: "three"}, now.Add(time.Minute)); err == nil {
		t.Fatal("oversized event window was consumed")
	}
	if !bytes.Equal(before, platform.Raw(frame)) {
		t.Fatal("refused window expiry changed state, watermark or cursor")
	}
	declared.State = 16 << 20
	declared.Window.LateEvents = "reject"
	if _, _, err := foldBatch(frame, declared, Batch{ID: "four", Predecessor: "three", Signals: []Signal{{Key: "old", At: now.Add(-time.Minute), Value: measurement}}}, now.Add(6*time.Second)); err == nil {
		t.Fatal("late-event refusal policy was ignored")
	}
	if !bytes.Equal(before, platform.Raw(frame)) {
		t.Fatal("late refusal consumed source input")
	}
	var restored BatchFrame
	if err := json.Unmarshal(before, &restored); err != nil {
		t.Fatal(err)
	}
	if _, _, err := foldBatch(&restored, declared, third, now.Add(time.Minute)); err != nil || !bytes.Equal(before, platform.Raw(&restored)) {
		t.Fatalf("restored input was refolded: %v", err)
	}
}
