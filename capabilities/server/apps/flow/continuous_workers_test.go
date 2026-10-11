package flow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// Partition workers for one continuous batch (ADR-0047 §13.3): a declared
// concurrency is real partition work, the merge commits the sequential frame,
// and a refusal cancels the run instead of finishing a fold that the decision
// will discard.

func workerDeclaration(concurrency int) platform.Continuous {
	return platform.Continuous{Source: "telemetry", Batch: 8192, State: 32 << 20, FrameBytes: 33 << 20, Concurrency: concurrency,
		DeadLetter: &platform.StreamDeadLetter{Node: "dlq", MaxRecords: 10000},
		Intake:     &platform.StreamIntake{SourceRecord: "stream", Key: "sku", Partition: []string{"deviceId"}, EventTime: "at", Value: "signal"},
		Window:     &platform.StreamWindow{Node: "window", WindowMS: 60000, SlideMS: 1000, WatermarkMS: 5000, MaxRecords: 20000, LateEvents: "sideOutput"},
		Aggregate:  &platform.StreamAggregate{Node: "stats", Signal: "reading", Group: []string{"deviceId"}, Measures: []string{"count", "sum", "mean", "min", "max"}},
		Threshold:  &platform.StreamThreshold{Node: "high", Field: "mean", High: 11.5, Low: 9.5, Severity: "High", DebounceMS: 0},
	}
}

// workerBatch is a real source batch shape: several partitions, each with its
// own event keys, and values the declared aggregate reads field by field.
func workerBatch(now time.Time, partitions, perPartition int) Batch {
	batch := Batch{ID: "source:workers", Source: &SourceCheckpoint{Record: "stream", Config: "sha256:config", Cursor: fmt.Sprint(partitions * perPartition),
		Sources: []string{"build.source/stream", "build.connection/db"}}}
	for p := range partitions {
		device := fmt.Sprintf("device-%02d", p)
		for i := range perPartition {
			at := now.Add(time.Duration(i) * time.Millisecond)
			batch.Signals = append(batch.Signals, Signal{Key: fmt.Sprintf("%s:%d", device, i), Partition: device, At: at,
				Value: platform.Raw(map[string]any{"deviceId": device, "reading": 9.0 + float64((p+i)%60)/10, "at": at.Format(time.RFC3339Nano)})})
		}
	}
	return batch
}

func foldWorkerBatch(t *testing.T, declared platform.Continuous, batch Batch, now time.Time) (BatchFrame, map[string]json.RawMessage) {
	t.Helper()
	in := &FlowInstance{Record: platform.Record{ID: "flow.telemetry:workers"}, State: "waiting"}
	if _, _, refusal := foldDecision(in, declared, batch, now); refusal != nil {
		t.Fatalf("the batch was refused: %s", refusal.Message)
	}
	return *in.Batch, in.Outputs
}

func TestContinuousPartitionWorkersCommitTheSequentialFrame(t *testing.T) {
	now := time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC)
	batch := workerBatch(now, 8, 640)
	if why := admitBatch(nil, workerDeclaration(8), batch); why != nil {
		t.Fatalf("the batch is not admissible: %s", why.Message)
	}
	sequential, sequentialOutputs := foldWorkerBatch(t, workerDeclaration(0), batch, now)
	parallel, parallelOutputs := foldWorkerBatch(t, workerDeclaration(8), batch, now)
	// The declaration is real work: more than one worker folded the batch, and
	// never more than the declaration allows.
	if parallel.Workers <= 1 || parallel.Workers > 8 {
		t.Fatalf("the declared partition workers did not really overlap: %d", parallel.Workers)
	}
	if sequential.Workers != 0 {
		t.Fatalf("a sequential fold recorded workers: %d", sequential.Workers)
	}
	// Only the measured occupancy may differ: state, letters, outputs and the
	// window's own bookkeeping are the sequential frame's own bytes.
	parallel.Workers = 0
	if !bytes.Equal(platform.Raw(sequential), platform.Raw(&parallel)) {
		t.Fatalf("partition workers committed a different frame:\nsequential %s\nparallel   %s", platform.Raw(&sequential), platform.Raw(&parallel))
	}
	if !bytes.Equal(platform.Raw(sequentialOutputs), platform.Raw(parallelOutputs)) {
		t.Fatalf("partition workers produced different outputs:\nsequential %s\nparallel   %s", platform.Raw(sequentialOutputs), platform.Raw(parallelOutputs))
	}
	var window WindowState
	if err := json.Unmarshal(parallel.State["window"], &window); err != nil {
		t.Fatal(err)
	}
	if len(window.Rows) != 8*640 || !window.Ready {
		t.Fatalf("the merged window lost or reordered rows: %d rows, ready=%v", len(window.Rows), window.Ready)
	}
	var stats []AggregateRecord
	if err := json.Unmarshal(parallelOutputs["stats"], &stats); err != nil || len(stats) != 8 {
		t.Fatalf("the merged aggregate lost groups: %v %s", err, parallelOutputs["stats"])
	}
	for _, record := range stats {
		if record.Values["count"] != 640 {
			t.Fatalf("group %s counted %v rows", record.Group, record.Values["count"])
		}
	}
}

func TestContinuousPartitionWorkersStopAtTheFirstRefusal(t *testing.T) {
	now := time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC)
	declared := workerDeclaration(8)
	in := &FlowInstance{Record: platform.Record{ID: "flow.telemetry:workers"}, State: "waiting"}
	first := workerBatch(now, 8, 64)
	if _, _, refusal := foldDecision(in, declared, first, now); refusal != nil {
		t.Fatalf("the first batch was refused: %s", refusal.Message)
	}
	before := platform.Raw(in.Batch)
	// One partition re-delivers an event identity with different content: the
	// whole batch is refused and the accepted frame keeps every byte.
	changed := workerBatch(now.Add(time.Second), 1, 1)
	changed.ID, changed.Predecessor = "source:changed", first.ID
	changed.Signals[0].Key = first.Signals[0].Key
	changed.Signals[0].Partition = first.Signals[0].Partition
	changed.Signals[0].At = first.Signals[0].At
	changed.Signals[0].Value = platform.Raw(map[string]any{"deviceId": "device-00", "reading": "changed"})
	if _, _, refusal := foldDecision(in, declared, changed, now.Add(time.Second)); refusal == nil {
		t.Fatal("a worker's refusal did not refuse the batch")
	}
	if !bytes.Equal(before, platform.Raw(in.Batch)) {
		t.Fatal("a refused parallel fold advanced the accepted frame")
	}
}

func TestContinuousFoldWorkersReallyOverlap(t *testing.T) {
	const workers = 8
	release := make(chan struct{})
	entered := make(chan struct{})
	var arrived atomic.Int64
	var wg sync.WaitGroup
	wg.Add(workers)
	done := make(chan struct{})
	var peak int
	var refusal *kernel.Error
	go func() {
		defer close(done)
		peak, refusal = (foldPool{limit: workers}).run(workers, func(int, func() bool) *kernel.Error {
			if arrived.Add(1) == workers {
				close(entered)
			}
			<-release
			wg.Done()
			return nil
		})
	}()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		close(release)
		<-done
		t.Fatal("the declared workers did not run at the same time")
	}
	close(release)
	wg.Wait()
	<-done
	if refusal != nil || peak != workers {
		t.Fatalf("the pool reported peak=%d refusal=%v", peak, refusal)
	}
}

func TestContinuousFoldWorkersStopAfterARefusal(t *testing.T) {
	var ran atomic.Int64
	refusal := platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "the partition cannot be folded")
	for _, limit := range []int{1, 4} {
		ran.Store(0)
		peak, got := (foldPool{limit: limit}).run(8, func(i int, cancelled func() bool) *kernel.Error {
			ran.Add(1)
			if i == 0 {
				return refusal
			}
			if cancelled() {
				return nil
			}
			time.Sleep(2 * time.Millisecond) // give the refusal time to cancel the run
			return nil
		})
		if got != refusal {
			t.Fatalf("limit %d: the pool returned %v", limit, got)
		}
		if peak < 1 || peak > limit {
			t.Fatalf("limit %d: peak %d", limit, peak)
		}
		if ran.Load() > int64(limit)+1 {
			t.Fatalf("limit %d: %d jobs ran after the refusal", limit, ran.Load())
		}
	}
}

func TestContinuousConcurrentFoldDeclarationIsChecked(t *testing.T) {
	window := platform.StreamWindow{Node: "window", WindowMS: 5000, SlideMS: 1000, WatermarkMS: 0, MaxRecords: 100, LateEvents: "sideOutput"}
	aggregate := &platform.StreamAggregate{Node: "stats", Signal: "reading", Group: []string{"deviceId"}, Measures: []string{"count", "mean"}}
	for _, c := range []struct {
		why      string
		declared platform.Continuous
		ok       bool
	}{
		{"the default graph's declared eight workers", platform.Continuous{Window: &window, Aggregate: aggregate, Concurrency: 8}, true},
		{"one worker keeps the sequence", platform.Continuous{Window: &window, Aggregate: aggregate, Concurrency: 1}, true},
		{"no declaration keeps the sequence", platform.Continuous{Window: &window, Aggregate: aggregate}, true},
		{"nine workers", platform.Continuous{Window: &window, Aggregate: aggregate, Concurrency: 9}, false},
		{"a negative worker count", platform.Continuous{Window: &window, Aggregate: aggregate, Concurrency: -1}, false},
		{"workers without the event-time window they fold", platform.Continuous{Aggregate: aggregate, Concurrency: 8}, false},
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
