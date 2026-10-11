package flow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func TestContinuousNativeOperatorsUseF32KernelAndAtomicFrame(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 10, 0, time.UTC)
	rule := platform.Continuous{Source: "measurements", Batch: 512, State: 1 << 20, FrameBytes: 2 << 20, DeadLetter: true, Concurrency: 8,
		Window:    &platform.StreamWindow{Node: "window", WindowMS: 30000, SlideMS: 5000, WatermarkMS: 2000, MaxRecords: 1000, LateEvents: "sideOutput"},
		Aggregate: &platform.StreamAggregate{Node: "aggregate", Kernel: "wasm:f32-window-stats", Signal: "reading", Group: []string{"assetId"}, Measures: []string{"count", "sum", "mean", "min", "max", "above"}, Above: 11.5},
		Threshold: &platform.StreamThreshold{Node: "threshold", Field: "mean", High: 11.5, Low: 9.5}, Output: &platform.StreamOutput{Node: "output", Name: "vibrationStats", Mode: "replace"}}
	frame := &BatchFrame{State: map[string]json.RawMessage{}}
	batch := Batch{ID: "one"}
	for i := range 8 {
		batch.Signals = append(batch.Signals, Signal{Key: fmt.Sprint(i), Partition: fmt.Sprint(i), At: now.Add(-6 * time.Second), Value: platform.Raw(map[string]any{"assetId": fmt.Sprint(i), "reading": 12.5000001, "mean": -999})})
	}
	if _, _, err := foldBatch(frame, rule, batch, now); err != nil {
		t.Fatal(err)
	}
	var records []AggregateRecord
	var alerts []Alert
	if json.Unmarshal(frame.OperatorOutputs["aggregate"], &records) != nil || len(records) != 8 {
		t.Fatalf("no real grouped aggregate: %s", frame.OperatorOutputs["aggregate"])
	}
	if json.Unmarshal(frame.OperatorOutputs["threshold"], &alerts) != nil || len(alerts) != 8 {
		t.Fatalf("no native threshold alerts: %s", frame.OperatorOutputs["threshold"])
	}
	for _, record := range records {
		if record.Values["mean"] != 12.5 || record.Values["above"] != 1 {
			t.Fatalf("the original f32 kernel was replaced: %+v", record)
		}
	}
	var output []map[string]any
	if json.Unmarshal(frame.OperatorOutputs["vibrationStats"], &output) != nil || len(output) != 8 || output[0]["mean"] != 12.5 {
		t.Fatal("metadata overwrote the accepted numeric output")
	}
	before := platform.Raw(frame)
	if _, _, err := foldBatch(frame, rule, batch, now); err != nil || !bytes.Equal(before, platform.Raw(frame)) {
		t.Fatal("same batch changed accepted hysteresis or aggregate")
	}
	rule.State = 1
	if _, _, err := foldBatch(frame, rule, Batch{ID: "two", Predecessor: "one", Signals: []Signal{{Key: "late", Partition: "x", At: now.Add(-time.Hour), Value: platform.Raw(1)}}}, now.Add(5*time.Second)); err == nil || !bytes.Equal(before, platform.Raw(frame)) {
		t.Fatal("rejected whole batch changed accepted state/cursor/dead letters")
	}
}

func TestContinuousThresholdDoesNotClearUntriggeredDebounce(t *testing.T) {
	at := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	rule := platform.StreamThreshold{Node: "threshold", Field: "mean", High: 11.5, Low: 9.5, DebounceMS: 2000}
	record := AggregateRecord{Group: "a", Values: map[string]float64{"mean": 12}}
	alert, raised, ok := advanceThreshold(rule, thresholdRaise{}, record, at, "one")
	if !ok || alert != nil || raised == nil || raised.Alerted {
		t.Fatal("debounce raised an immediate alarm")
	}
	record.Values["mean"] = 9
	if alert, next, _ := advanceThreshold(rule, *raised, record, at.Add(time.Second), "two"); alert != nil || next != nil {
		t.Fatal("an episode that never triggered emitted a false cleared alert")
	}
	record.Values["mean"] = 12
	_, raised, _ = advanceThreshold(rule, thresholdRaise{}, record, at, "one")
	alert, raised, _ = advanceThreshold(rule, *raised, record, at.Add(2*time.Second), "two")
	if alert == nil || alert.State != "triggered" || !raised.Alerted {
		t.Fatal("the completed debounce never triggered")
	}
	record.Values["mean"] = 9
	if alert, next, _ := advanceThreshold(rule, *raised, record, at.Add(3*time.Second), "three"); alert == nil || alert.State != "cleared" || next != nil {
		t.Fatal("a raised episode did not clear once")
	}
}

func TestContinuousPartitionWorkersOccupyEightSlotsAndCancel(t *testing.T) {
	started := make(chan struct{}, 8)
	release := make(chan struct{})
	done := make(chan int, 1)
	go func() {
		peak, _ := (foldPool{limit: 8}).run(8, func(_ int, _ func() bool) *kernel.Error { started <- struct{}{}; <-release; return nil })
		done <- peak
	}()
	for range 8 {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			close(release)
			t.Fatal("eight real jobs did not overlap")
		}
	}
	close(release)
	if peak := <-done; peak != 8 {
		t.Fatalf("actual worker occupancy=%d", peak)
	}
	var mu sync.Mutex
	called := map[int]bool{}
	_, err := (foldPool{limit: 1}).run(100, func(i int, _ func() bool) *kernel.Error {
		mu.Lock()
		called[i] = true
		mu.Unlock()
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "cancel batch")
	})
	if err == nil || len(called) != 1 {
		t.Fatalf("refused batch kept starting partition jobs: %d", len(called))
	}
}
