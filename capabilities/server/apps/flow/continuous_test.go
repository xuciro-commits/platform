package flow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// Continuous Flow: admission, watermark, dead letters and the batch frame
// (ADR-0047 §13, plan A).

func TestContinuousBatchAdmissionAndFolding(t *testing.T) {
	declared := platform.Continuous{Source: "k8.meters", Batch: 3, DeadLetter: &platform.StreamDeadLetter{Node: "dlq", MaxRecords: 10000}}
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
	declared := platform.Continuous{Source: "k8.meters", Batch: 512, State: 4096, DeadLetter: &platform.StreamDeadLetter{Node: "dlq", MaxRecords: 10000}}
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
	declared := platform.Continuous{Source: "k8.meters", Batch: 512, DeadLetter: &platform.StreamDeadLetter{Node: "dlq", MaxRecords: 10000}}
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
	declared := platform.Continuous{Source: "k8.meters", Batch: 512, State: 16 << 20, FrameBytes: 17 << 20, DeadLetter: &platform.StreamDeadLetter{Node: "dlq", MaxRecords: 10000},
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

func TestContinuousWindowAggregateAndHysteresisThreshold(t *testing.T) {
	now := time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC)
	declared := platform.Continuous{Source: "k8.meters", Batch: 512, State: 1 << 20, FrameBytes: 2 << 20, DeadLetter: &platform.StreamDeadLetter{Node: "dlq", MaxRecords: 10000},
		Window:    &platform.StreamWindow{Node: "window", WindowMS: 5000, SlideMS: 1000, WatermarkMS: 2000, MaxRecords: 50000, LateEvents: "sideOutput"},
		Aggregate: &platform.StreamAggregate{Node: "stats", Signal: "vibration", Group: []string{"deviceId"}, Measures: []string{"count", "mean", "max"}},
		Threshold: &platform.StreamThreshold{Node: "high", Field: "mean", High: 11.5, Low: 9.5, Severity: "High"}}
	frame := &BatchFrame{State: map[string]json.RawMessage{}}
	measurement := func(device string, vibration float64) json.RawMessage {
		return json.RawMessage(fmt.Sprintf(`{"deviceId":%q,"vibration":%g}`, device, vibration))
	}
	run := func(id, predecessor string, at time.Time, signals ...Signal) ([]AggregateRecord, []Alert, *kernel.Error) {
		t.Helper()
		batch := Batch{ID: id, Predecessor: predecessor, Signals: signals}
		if _, _, err := foldBatch(frame, declared, batch, at); err != nil {
			return nil, nil, err
		}
		return foldOperators(frame, declared, id, at)
	}
	// A slide whose mean is over the high mark raises and, with no debounce,
	// alarms once with the aggregate's own field names.
	stats, alerts, err := run("b1", "", now, []Signal{
		{Key: "m1", Partition: "plant-a", At: now, Value: measurement("d1", 12)},
		{Key: "m2", Partition: "plant-a", At: now.Add(time.Second), Value: measurement("d1", 13)},
		{Key: "m3", Partition: "plant-a", At: now.Add(2 * time.Second), Value: measurement("d1", 12)},
	}...)
	if err != nil {
		t.Fatalf("aggregate fold: %v", err)
	}
	if len(stats) != 1 || stats[0].Count != 3 || stats[0].Values["mean"] != 37.0/3 || stats[0].Values["max"] != 13 {
		t.Fatalf("aggregate records: %+v", stats)
	}
	if len(alerts) != 1 || alerts[0].State != "triggered" || alerts[0].Field != "mean" || alerts[0].Group != "[d1]" || alerts[0].Severity != "High" || alerts[0].Batch != "b1" {
		t.Fatalf("triggered alert: %+v", alerts)
	}
	// Inside the band the episode stays raised and says nothing; below the low
	// mark it clears exactly once.
	if _, alerts, err = run("b2", "b1", now.Add(5*time.Second), Signal{Key: "m4", Partition: "plant-a", At: now.Add(6 * time.Second), Value: measurement("d1", 10)}); err != nil || len(alerts) != 0 {
		t.Fatalf("hysteresis band emitted: %+v %v", alerts, err)
	}
	if _, alerts, err = run("b3", "b2", now.Add(10*time.Second), Signal{Key: "m5", Partition: "plant-a", At: now.Add(11 * time.Second), Value: measurement("d1", 8)}); err != nil || len(alerts) != 1 || alerts[0].State != "cleared" {
		t.Fatalf("cleared alert: %+v %v", alerts, err)
	}
	// A row the declared signal cannot read is dead-lettered once and leaves
	// the window, so the next slide does not report it again.
	if _, _, err = run("b4", "b3", now.Add(15*time.Second), Signal{Key: "m6", Partition: "plant-a", At: now.Add(16 * time.Second), Value: json.RawMessage(`{"deviceId":"d1"}`)}); err != nil {
		t.Fatal(err)
	}
	if len(frame.DeadLetters) != 1 || frame.DeadLetters[0].Reason != "aggregate: no numeric vibration" {
		t.Fatalf("dead letters: %+v", frame.DeadLetters)
	}
	if _, _, err = run("b5", "b4", now.Add(20*time.Second), Signal{Key: "m7", Partition: "plant-a", At: now.Add(21 * time.Second), Value: measurement("d1", 12)}); err != nil || len(frame.DeadLetters) != 1 {
		t.Fatalf("dead letter reported twice: %+v %v", frame.DeadLetters, err)
	}
	// The declaration is checked, not inferred: a threshold field the
	// aggregate does not produce is refused and the frame is untouched.
	before := platform.Raw(frame)
	mismatched := declared
	reading := *declared.Threshold
	reading.Field = "reading"
	mismatched.Threshold = &reading
	if _, _, err := foldOperators(frame, mismatched, "b6", now.Add(25*time.Second)); err == nil || err.Code != pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT {
		t.Fatalf("an unwired threshold field was accepted: %v", err)
	}
	if !bytes.Equal(before, platform.Raw(frame)) {
		t.Fatal("a refused declaration changed the accepted frame")
	}
	// An accepted frame replays without re-emitting its alarms.
	var restored BatchFrame
	if err := json.Unmarshal(before, &restored); err != nil {
		t.Fatal(err)
	}
	if _, alerts, err := foldOperators(&restored, declared, "b5", now.Add(30*time.Second)); err != nil || len(alerts) != 0 {
		t.Fatalf("replay re-emitted alarms: %+v %v", alerts, err)
	}
}

func TestContinuousDeadLettersAreNumberedAndBounded(t *testing.T) {
	now := time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC)
	declared := platform.Continuous{Source: "k8.meters", Batch: 512, DeadLetter: &platform.StreamDeadLetter{Node: "dlq", MaxRecords: 2}}
	frame := &BatchFrame{State: map[string]json.RawMessage{}}
	dead := func(id, predecessor string, at time.Time, keys ...string) {
		t.Helper()
		signals := make([]Signal, 0, len(keys))
		for _, key := range keys {
			// No event time: the signal cannot be folded and is kept as a letter.
			signals = append(signals, Signal{Key: key, Partition: "plant-a", Value: json.RawMessage("1")})
		}
		if _, _, err := foldBatch(frame, declared, Batch{ID: id, Predecessor: predecessor, Signals: signals}, at); err != nil {
			t.Fatalf("batch %s: %v", id, err)
		}
		if _, _, err := foldOperators(frame, declared, id, at); err != nil {
			t.Fatalf("operators %s: %v", id, err)
		}
	}
	// The declared count bounds the asset: the oldest letter beyond it is
	// dropped and counted, and the letters that stay keep their numbers.
	dead("b1", "", now, "a", "b")
	if len(frame.DeadLetters) != 2 || frame.DeadLetters[0].Seq != 1 || frame.DeadLetters[1].Seq != 2 || frame.DeadLetters[0].Seen != now || frame.Dropped != 0 {
		t.Fatalf("letters: %+v dropped %d", frame.DeadLetters, frame.Dropped)
	}
	dead("b2", "b1", now.Add(time.Minute), "c")
	if len(frame.DeadLetters) != 2 || frame.DeadLetters[0].Seq != 2 || frame.DeadLetters[1].Seq != 3 || frame.Dropped != 1 || frame.LetterSeq != 3 {
		t.Fatalf("bounded letters: %+v dropped %d issued %d", frame.DeadLetters, frame.Dropped, frame.LetterSeq)
	}
	// A declared age trims a letter whose decision is older than the TTL; the
	// fresh one keeps the number it was given, not a new one.
	aged := declared
	aged.DeadLetter = &platform.StreamDeadLetter{Node: "dlq", MaxRecords: 100, TTLMS: 60000}
	if _, _, err := foldOperators(frame, aged, "b3", now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if len(frame.DeadLetters) != 1 || frame.DeadLetters[0].Seq != 3 || frame.Dropped != 2 || frame.LetterSeq != 3 {
		t.Fatalf("aged letters: %+v dropped %d issued %d", frame.DeadLetters, frame.Dropped, frame.LetterSeq)
	}
}

func TestContinuousOperatorDeclarationIsChecked(t *testing.T) {
	window := &platform.StreamWindow{Node: "window", WindowMS: 30000, SlideMS: 5000, WatermarkMS: 2000, MaxRecords: 50000, LateEvents: "sideOutput"}
	aggregate := &platform.StreamAggregate{Node: "stats", Signal: "vibration", Group: []string{"deviceId"}, Measures: []string{"count", "mean", "max"}}
	threshold := func(field string, low, high float64) *platform.StreamThreshold {
		return &platform.StreamThreshold{Node: "high", Field: field, High: high, Low: low, DebounceMS: 3000}
	}
	for _, c := range []struct {
		why      string
		declared platform.Continuous
		ok       bool
	}{
		{"the default graph's declared field", platform.Continuous{Window: window, Aggregate: aggregate, Threshold: threshold("mean", 9.5, 11.5)}, true},
		{"a field the aggregate does not produce", platform.Continuous{Window: window, Aggregate: aggregate, Threshold: threshold("reading", 9.5, 11.5)}, false},
		{"a threshold without its aggregate", platform.Continuous{Window: window, Threshold: threshold("mean", 9.5, 11.5)}, false},
		{"an aggregate without the event-time window", platform.Continuous{Aggregate: aggregate, Threshold: threshold("mean", 9.5, 11.5)}, false},
		{"a measure outside the fixed set", platform.Continuous{Window: window, Aggregate: &platform.StreamAggregate{Node: "stats", Signal: "vibration", Measures: []string{"median"}}}, false},
		{"low above high", platform.Continuous{Window: window, Aggregate: aggregate, Threshold: threshold("mean", 12, 11.5)}, false},
	} {
		err := checkOperators(c.declared)
		if c.ok && err != nil {
			t.Errorf("%s: refused: %v", c.why, err)
		}
		if !c.ok && err == nil {
			t.Errorf("%s: accepted", c.why)
		}
	}
}
