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

func TestContinuousCheckpointIntervalCountsAcceptedSourceBatches(t *testing.T) {
	now := time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC)
	declared := platform.Continuous{Source: "stream", CheckpointEvery: 2, DeadLetter: true}
	frame := &BatchFrame{State: map[string]json.RawMessage{}}
	first := Batch{ID: "one", Signals: []Signal{{Key: "one", At: now, Value: json.RawMessage(`1`)}}}
	if _, _, err := foldBatch(frame, declared, first, now); err != nil {
		t.Fatal(err)
	}
	if frame.CheckpointEvery != 2 || frame.BatchesSinceCheckpoint != 1 || frame.CheckpointCursor != "" || frame.checkpointDue {
		t.Fatalf("the first batch did not wait for the declared interval: %+v", frame)
	}
	if _, _, err := foldBatch(frame, declared, first, now); err != nil || frame.BatchesSinceCheckpoint != 1 || frame.checkpointDue {
		t.Fatalf("an idempotent retry advanced the checkpoint schedule: %+v %v", frame, err)
	}
	second := Batch{ID: "two", Predecessor: "one", Signals: []Signal{{Key: "two", At: now.Add(time.Second), Value: json.RawMessage(`2`)}}}
	if _, _, err := foldBatch(frame, declared, second, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if frame.CheckpointCursor != "two" || frame.BatchesSinceCheckpoint != 0 || !frame.checkpointDue {
		t.Fatalf("the interval did not mark its exact accepted cursor: %+v", frame)
	}
	before := platform.Raw(frame)
	if _, _, err := foldBatch(frame, declared, Batch{ID: "gap", Predecessor: "one"}, now.Add(2*time.Second)); err == nil || !bytes.Equal(before, platform.Raw(frame)) {
		t.Fatal("a refused predecessor advanced or changed the checkpoint frame")
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

func TestContinuousSlideTickAdvancesWindowWithoutSourceProgress(t *testing.T) {
	now := time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC)
	window := platform.StreamWindow{Node: "window", WindowMS: 30000, SlideMS: 5000, WatermarkMS: 2000, MaxRecords: 100, LateEvents: "sideOutput"}
	checkpoint := &SourceCheckpoint{Record: "stream", Config: "source-config", Cursor: "12", Sources: []string{"build.source/stream", "build.connection/db"}, Offset: "earliest", Initialized: true}
	state := WindowState{Rows: []Signal{{Key: "sensor", Partition: "plant-a", At: now, Value: json.RawMessage(`7`)}}, EmittedAt: now.Add(-5 * time.Second)}
	stateBytes, _ := json.Marshal(state)
	frame := &BatchFrame{Cursor: "source:12", Watermark: now, Consumed: 1, State: map[string]json.RawMessage{"window": stateBytes}, Source: checkpoint}
	rule := platform.Continuous{Source: "stream", Window: &window, Intake: &platform.StreamIntake{SourceRecord: "stream", Offset: "earliest"}}
	plan := &BatchPreparation{before: FlowInstance{ID: "flow:live", Version: 3, Batch: frame}, rule: rule}
	tickAt := now.Add(7 * time.Second)
	batch, due, refusal := plan.WindowTick(tickAt, nil)
	if refusal != nil || !due || !batch.Tick || batch.ID == "" || batch.Predecessor != frame.Cursor {
		t.Fatalf("idle slide did not produce a stable host tick: batch=%+v due=%v refusal=%v", batch, due, refusal)
	}
	if batch.Source == nil || batch.Source.Cursor != checkpoint.Cursor || !sameSourceCheckpoint(batch.Source, checkpoint) {
		t.Fatalf("the timer advanced or rebound the source cursor: %+v", batch.Source)
	}
	if folded, rejected, err := foldBatch(frame, rule, batch, tickAt); err != nil || folded != 0 || rejected != 0 {
		t.Fatalf("timer tick did not fold an empty successor: folded=%d rejected=%d err=%v", folded, rejected, err)
	}
	var next WindowState
	if err := json.Unmarshal(frame.State[window.Node], &next); err != nil || !next.Ready || !next.EmittedAt.Equal(now.Add(5*time.Second)) || len(windowSignals(next, window)) != 1 {
		t.Fatalf("idle tick did not close the grace-delayed slide: state=%+v err=%v", next, err)
	}
	if frame.Cursor != batch.ID || frame.Source.Cursor != checkpoint.Cursor || frame.Consumed != 1 {
		t.Fatalf("idle tick changed the data cursor or signal count: %+v", frame)
	}
	before := platform.Raw(frame)
	if folded, rejected, err := foldBatch(frame, rule, batch, tickAt); err != nil || folded != 0 || rejected != 0 || !bytes.Equal(before, platform.Raw(frame)) {
		t.Fatalf("repeated tick was not idempotent: folded=%d rejected=%d err=%v", folded, rejected, err)
	}
	later := &BatchPreparation{before: FlowInstance{ID: "flow:live", Version: 3, Batch: frame}, rule: rule}
	if _, due, refusal := later.WindowTick(tickAt, nil); refusal != nil || due {
		t.Fatalf("the same slide kept ticking after acceptance: due=%v refusal=%v", due, refusal)
	}
}

func TestContinuousIdleComputeEmitsAnEmptySlide(t *testing.T) {
	now := time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC)
	window := platform.StreamWindow{Node: "window", WindowMS: 30000, SlideMS: 5000, WatermarkMS: 2000, MaxRecords: 100, LateEvents: "sideOutput"}
	checkpoint := &SourceCheckpoint{Record: "stream", Config: "source-config", Cursor: "12", Sources: []string{"build.source/stream", "build.connection/db"}, Offset: "earliest", Initialized: true}
	state := WindowState{Rows: []Signal{}, EmittedAt: now.Add(-5 * time.Second)}
	stateBytes, _ := json.Marshal(state)
	frame := &BatchFrame{Cursor: "source:12", Watermark: now, State: map[string]json.RawMessage{"window": stateBytes}, Source: checkpoint}
	rule := platform.Continuous{Source: "stream", Entry: "aggregate", Window: &window, Intake: &platform.StreamIntake{SourceRecord: "stream", Offset: "earliest"}}
	plan := &BatchPreparation{before: FlowInstance{ID: "flow:empty-slide", Version: 1, Batch: frame}, rule: rule, entry: "aggregate"}
	tickAt := now.Add(7 * time.Second)
	batch, due, refusal := plan.WindowTick(tickAt, nil)
	if refusal != nil || !due || !batch.Tick {
		t.Fatalf("a quiet retained Compute did not schedule its empty slide: %+v due=%v err=%v", batch, due, refusal)
	}
	if _, _, err := foldBatch(frame, rule, batch, tickAt); err != nil {
		t.Fatal(err)
	}
	var emitted WindowState
	if err := json.Unmarshal(frame.State[window.Node], &emitted); err != nil || !emitted.Ready || len(windowSignals(emitted, window)) != 0 {
		t.Fatalf("the empty slide was not retained for threshold/reset evaluation: %+v err=%v", emitted, err)
	}
}

func TestContinuousDeadLetterExpiryTickDoesNotAdvanceSourceCursor(t *testing.T) {
	now := time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC)
	window := platform.StreamWindow{Node: "window", WindowMS: 30000, SlideMS: 5000, WatermarkMS: 2000, MaxRecords: 100, LateEvents: "sideOutput"}
	checkpoint := &SourceCheckpoint{Record: "stream", Config: "source-config", Cursor: "12", Sources: []string{"build.source/stream", "build.connection/db"}, Offset: "earliest", Initialized: true}
	state := WindowState{Rows: []Signal{}, EmittedAt: now.Add(5 * time.Second)}
	stateBytes, _ := json.Marshal(state)
	frame := &BatchFrame{
		Cursor: "source:12", Watermark: now.Add(7 * time.Second), State: map[string]json.RawMessage{"window": stateBytes},
		Source: checkpoint, DeadLetters: []DeadLetter{{Batch: "old", Key: "late", At: now, RecordedAt: now}},
	}
	rule := platform.Continuous{Source: "stream", Window: &window, Intake: &platform.StreamIntake{SourceRecord: "stream", Offset: "earliest"}, DeadLetterTTLMs: 1000}
	plan := &BatchPreparation{before: FlowInstance{ID: "flow:dead-letter-expiry", Version: 1, Batch: frame}, rule: rule}
	batch, due, refusal := plan.WindowTick(now.Add(7*time.Second), nil)
	if refusal != nil || !due || !batch.Tick {
		t.Fatalf("expired dead letter did not receive a bounded cleanup tick: %+v due=%v err=%v", batch, due, refusal)
	}
	if _, _, err := foldBatch(frame, rule, batch, now.Add(7*time.Second)); err != nil {
		t.Fatal(err)
	}
	if len(frame.DeadLetters) != 0 || frame.DeadLetterDropped != 1 || frame.Source.Cursor != checkpoint.Cursor {
		t.Fatalf("expiry tick did not prune only the dead-letter detail: %+v", frame)
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
	second := Batch{ID: "two", Predecessor: "one", Signals: []Signal{
		first.Signals[0],
		{Key: "within-grace", Partition: "plant-a", At: now.Add(-30 * time.Second), Value: measurement},
		{Key: "late", Partition: "plant-a", At: now.Add(-31 * time.Second), Value: measurement},
	}}
	if folded, rejected, err := foldBatch(frame, declared, second, now.Add(time.Second)); err != nil || folded != 1 || rejected != 1 {
		t.Fatalf("duplicate/grace/late routing: folded=%d rejected=%d frame=%+v err=%v", folded, rejected, frame, err)
	}
	if window().Duplicate != 1 || window().Ready || len(frame.DeadLetters) != 1 || frame.DeadLetters[0].Key != "late" || frame.DeadLetters[0].Partition != "plant-a" {
		t.Fatalf("window phase or late-event identity lost: %+v", frame)
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
	if folded, rejected, err := foldBatch(frame, declared, third, now.Add(5*time.Second)); err != nil || folded != 1 || rejected != 2 {
		t.Fatalf("overflow consumption: folded=%d rejected=%d error=%v", folded, rejected, err)
	}
	if len(window().Rows) != 2 || !window().Ready || len(frame.DeadLetters) != 3 || frame.DeadLetters[2].Key != "same" || frame.DeadLetters[2].Partition != "plant-b" {
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
