package automation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

type BatchFrame = platform.FlowBatchFrame

type SourceCheckpoint = platform.FlowSourceCheckpoint

type DeadLetter = platform.FlowDeadLetter

type Batch = platform.FlowBatch

type Signal = platform.FlowSignal

type BatchOutcome = platform.FlowBatchOutcome

type ContinuousWindowInput = platform.FlowWindowInput

func (f *Flows) ConsumeBatch(c platform.Caller, id string, batch Batch, now time.Time) (BatchOutcome, *kernel.Error) {
	invalid := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	if batch.ID == "" {
		return BatchOutcome{}, invalid
	}
	x, ok := platform.Get[FlowInstance](c, id)
	if !ok {
		return BatchOutcome{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	d := f.def(x.Flow, x.Version)
	if d == nil || d.Continuous == nil {
		return BatchOutcome{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA,
			Message: "this instance's flow is not continuous"}
	}
	if d.Continuous.Entry != "" {
		return BatchOutcome{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A window Compute entry requires the host-prepared source acceptance path")
	}
	if d.Continuous.CheckpointEvery > 0 {
		return BatchOutcome{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Periodic checkpoints require the host-prepared Flow frame path")
	}
	if ended(x.State) {
		return BatchOutcome{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT, Message: "the instance has ended"}
	}
	if x.Batch != nil && x.Batch.Sealed != nil {
		return BatchOutcome{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A sealed Flow frame is prepared on the host I/O lane")
	}
	declared := *d.Continuous
	if err := admitBatch(x.Batch, declared, batch); err != nil {
		return BatchOutcome{}, err
	}
	if x.Batch != nil && batch.ID == x.Batch.Cursor {
		return BatchOutcome{Cursor: x.Batch.Cursor, Watermark: x.Batch.Watermark, Consumed: x.Batch.Consumed, Rejected: x.Batch.Rejected,
			StateSize: stateSize(x.Batch.State)}, nil
	}
	var folded, rejected int
	err := f.update(c, id, now, func(_ *session, in *FlowInstance) *kernel.Error {
		frame := in.Batch
		if frame == nil {
			frame = &BatchFrame{State: map[string]json.RawMessage{}}
			in.Batch = frame
		}
		var refusal *kernel.Error
		folded, rejected, refusal = foldBatch(frame, declared, batch, now)
		if refusal != nil {
			return refusal
		}
		in.Outputs = maps.Clone(in.Outputs)
		if in.Outputs == nil {
			in.Outputs = map[string]json.RawMessage{}
		}
		for node, state := range frame.State {
			if len(state) <= 48<<10 {
				in.Outputs["state:"+node] = slices.Clone(state)
			} else {
				in.Outputs["state:"+node] = platform.Raw(BatchStateReference{Instance: in.ID, Node: node, Digest: fmt.Sprintf("sha256:%x", sha256.Sum256(state)), Size: len(state)})
			}
		}
		for node, raw := range frame.OperatorOutputs {
			in.Outputs[node] = slices.Clone(raw)
		}
		in.Outputs["batch"] = json.RawMessage(fmt.Sprintf(`{"cursor":%q,"consumed":%d,"rejected":%d}`, frame.Cursor, frame.Consumed, frame.Rejected))
		encoded, err := json.Marshal(in.Outputs)
		if err != nil || len(encoded) > 60<<10 {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The batch outputs exceed the Flow output budget")
		}
		return nil
	})
	if err != nil {
		return BatchOutcome{}, err
	}
	x, _ = platform.Get[FlowInstance](c, id)
	out := BatchOutcome{Consumed: folded, Rejected: rejected}
	if x.Batch != nil {
		out.Cursor, out.Watermark, out.StateSize = x.Batch.Cursor, x.Batch.Watermark, stateSize(x.Batch.State)
	}
	return out, nil
}

type BatchStateReference = platform.FlowBatchStateReference

func admitBatch(frame *BatchFrame, declared platform.Continuous, batch Batch) *kernel.Error {
	if declared.Intake != nil && batch.Source == nil || declared.Intake == nil && batch.Source != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A source checkpoint must match the declared intake")
	}
	if declared.Intake != nil && batch.Source != nil && (batch.Source.Initialized && batch.Source.Offset != declared.Intake.Offset || declared.Intake.Offset == "latest" && !batch.Source.Initialized) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A source checkpoint's initial offset must match the declared intake")
	}
	if batch.Bootstrap && (declared.Intake == nil || declared.Intake.Offset != "latest" || len(batch.Signals) != 0 || batch.Source == nil || !batch.Source.Initialized || batch.Source.Offset != "latest" || batch.Tick) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A latest-offset bootstrap must be an empty controlled-source checkpoint")
	}
	if batch.Tick {
		if declared.Intake == nil || declared.Window == nil || batch.Source == nil || batch.Bootstrap || len(batch.Signals) != 0 {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A slide tick needs an empty controlled-source window batch")
		}
		if frame == nil || frame.Source == nil || !sameSourceCheckpoint(frame.Source, batch.Source) {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A slide tick must preserve the accepted source checkpoint")
		}
	}
	if batch.ID == "" {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: "the batch has no identity"}
	}
	if declared.Batch > 0 && len(batch.Signals) > declared.Batch {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT,
			Message: fmt.Sprintf("the batch carries %d signals, the flow accepts %d", len(batch.Signals), declared.Batch)}
	}
	if declared.Batch < 0 || declared.State < 0 || declared.FrameBytes < 0 || declared.CheckpointEvery < 0 || declared.CheckpointEvery > 1_000_000 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Continuous batch budgets and checkpoint intervals must be within their bounds")
	}
	digest, err := batchFingerprint(batch)
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A batch needs valid JSON signal values")
	}
	if frame != nil && frame.Cursor == batch.ID && frame.Fingerprint != "" && frame.Fingerprint != digest {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The batch identity was reused with different content")
	}
	if frame == nil || frame.Cursor == "" || batch.ID == frame.Cursor {
		return nil
	}
	if batch.Predecessor != frame.Cursor {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT,
			Message: fmt.Sprintf("the batch follows %s, the flow consumed %s", batch.Predecessor, frame.Cursor)}
	}
	return nil
}

func sameSourceCheckpoint(a, b *SourceCheckpoint) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Record == b.Record && a.Config == b.Config && a.Cursor == b.Cursor &&
		a.Offset == b.Offset && a.Initialized == b.Initialized && slices.Equal(a.Sources, b.Sources)
}

func foldBatch(frame *BatchFrame, declared platform.Continuous, batch Batch, acceptedAt ...time.Time) (folded, rejected int, refusal *kernel.Error) {
	if err := admitBatch(frame, declared, batch); err != nil {
		return 0, 0, err
	}
	if err := checkOperators(declared); err != nil {
		return 0, 0, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	if frame.Cursor == batch.ID {
		return 0, 0, nil
	}
	if frame.CheckpointEvery != 0 && frame.CheckpointEvery != declared.CheckpointEvery {
		return 0, 0, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The retained checkpoint interval differs from this Flow version")
	}
	next := *frame
	next.CheckpointEvery = declared.CheckpointEvery
	next.State = maps.Clone(frame.State)
	next.OperatorOutputs = maps.Clone(frame.OperatorOutputs)
	next.DeadLetters = slices.Clone(frame.DeadLetters)
	if next.State == nil {
		next.State = map[string]json.RawMessage{}
	}
	if !batch.Bootstrap {
		if declared.Window != nil {
			if len(acceptedAt) != 1 || acceptedAt[0].IsZero() {
				return 0, 0, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "An event-time batch needs its accepted decision time")
			}
			var err *kernel.Error
			folded, rejected, err = foldWindow(&next, declared, batch, acceptedAt[0])
			if err != nil {
				return 0, 0, err
			}
		} else {
			var recordedAt time.Time
			if len(acceptedAt) == 1 {
				recordedAt = acceptedAt[0]
			}
			for _, signal := range batch.Signals {
				switch {
				case signal.At.IsZero():
					next.DeadLetters = append(next.DeadLetters, DeadLetter{Batch: batch.ID, Key: signal.Key, RecordedAt: recordedAt, Reason: "no event time", Value: string(signal.Value)})
					rejected++
				case !next.Watermark.IsZero() && signal.At.Before(next.Watermark):
					next.DeadLetters = append(next.DeadLetters, DeadLetter{Batch: batch.ID, Key: signal.Key, At: signal.At, RecordedAt: recordedAt, Reason: "late: before the watermark", Value: string(signal.Value)})
					rejected++
				default:
					if err := fold(&next, declared, signal); err != nil {
						return 0, 0, err
					}
					folded++
				}
				if signal.At.After(next.Watermark) {
					next.Watermark = signal.At
				}
			}
		}
	}
	var retainedAt time.Time
	if len(acceptedAt) == 1 {
		retainedAt = acceptedAt[0]
	}
	if !batch.Bootstrap && declared.Aggregate != nil {
		stats, alerts, err := foldDeclaredOperators(&next, declared, batch.ID, retainedAt)
		if err != nil {
			return 0, 0, err
		}
		if next.OperatorOutputs == nil {
			next.OperatorOutputs = map[string]json.RawMessage{}
		}
		next.OperatorOutputs[declared.Aggregate.Node] = platform.Raw(stats)
		if declared.Threshold != nil {
			next.OperatorOutputs[declared.Threshold.Node] = platform.Raw(alerts)
		}
		if declared.Output != nil {
			next.OperatorOutputs[declared.Output.Name] = platform.Raw(aggregateOutput(stats))
		}
	}
	pruneDeadLetters(&next, declared, retainedAt)
	for i := range next.DeadLetters {
		if next.DeadLetters[i].Seq == 0 {
			next.DeadLetterSequence++
			next.DeadLetters[i].Seq = next.DeadLetterSequence
			if declared.Window != nil {
				next.DeadLetters[i].Node = declared.Window.Node
			}
		}
	}
	next.Cursor, next.Predecessor = batch.ID, batch.Predecessor
	if batch.Source != nil {
		checkpoint := *batch.Source
		checkpoint.Sources = slices.Clone(batch.Source.Sources)
		next.Source = &checkpoint
	}
	next.Fingerprint, _ = batchFingerprint(batch)
	next.Consumed += folded
	next.Rejected += rejected
	next.StateBytes = stateSize(next.State)
	if declared.CheckpointEvery > 0 && !batch.Bootstrap && !batch.Tick {
		next.BatchesSinceCheckpoint++
		if next.BatchesSinceCheckpoint >= declared.CheckpointEvery {
			next.BatchesSinceCheckpoint = 0
			next.CheckpointCursor = next.Cursor
		}
	}
	raw, err := json.Marshal(next)
	if err != nil {
		return 0, 0, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The batch frame cannot be encoded")
	}
	if declared.FrameBytes > 0 && len(raw) > declared.FrameBytes {
		return 0, 0, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The batch frame exceeds its declared budget")
	}
	*frame = next
	return folded, rejected, nil
}

func pruneDeadLetters(frame *BatchFrame, declared platform.Continuous, now time.Time) {
	if declared.DeadLetterTTLMs > 0 && !now.IsZero() {
		cutoff := now.Add(-time.Duration(declared.DeadLetterTTLMs) * time.Millisecond)
		kept := frame.DeadLetters[:0]
		for _, letter := range frame.DeadLetters {
			recorded := letter.RecordedAt
			if recorded.IsZero() {
				recorded = letter.At
			}
			if !recorded.IsZero() && recorded.Before(cutoff) {
				frame.DeadLetterDropped++
				continue
			}
			kept = append(kept, letter)
		}
		frame.DeadLetters = kept
	}
	if limit := declared.DeadLetterMaxRecords; limit > 0 && len(frame.DeadLetters) > limit {
		dropped := len(frame.DeadLetters) - limit
		frame.DeadLetterDropped += dropped
		frame.DeadLetters = slices.Clone(frame.DeadLetters[dropped:])
	}
}

func fold(frame *BatchFrame, declared platform.Continuous, signal Signal) *kernel.Error {
	state := map[string]any{}
	if raw := frame.State["window"]; len(raw) > 0 {
		if json.Unmarshal(raw, &state) != nil || state == nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The saved window state cannot be decoded")
		}
	}
	count, _ := state["count"].(float64)
	sum, _ := state["sum"].(float64)
	last := map[string]any{}
	if prior, ok := state["last"].(map[string]any); ok {
		last = prior
	}
	var value float64
	if json.Unmarshal(signal.Value, &value) == nil {
		count, sum = count+1, sum+value
	}
	if signal.Key != "" {
		last[signal.Key] = signal.Value
	}
	state["count"], state["sum"], state["last"] = count, sum, last
	raw, err := json.Marshal(state)
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The window state cannot be encoded")
	}
	if declared.State > 0 && len(raw) > declared.State {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The window state exceeds its declared budget")
	}
	frame.State["window"] = raw
	return nil
}

func batchFingerprint(batch Batch) (string, error) {
	raw, err := json.Marshal(batch)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256(raw)), nil
}

func stateSize(state map[string]json.RawMessage) int {
	total := 0
	for _, raw := range state {
		total += len(raw)
	}
	return total
}

func windowF32Stats(values []float64, threshold float64, cancelled func() bool) (map[string]float64, error) {
	if len(values) > 1_000_000 || math.IsNaN(threshold) || math.IsInf(threshold, 0) {
		return nil, fmt.Errorf("invalid telemetry kernel input bound")
	}
	deadline := time.Now().Add(2 * time.Second)
	check := func() error {
		if cancelled() {
			return fmt.Errorf("telemetry kernel execution: %w", context.Canceled)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("telemetry kernel execution: %w", context.DeadlineExceeded)
		}
		return nil
	}
	cutoff := float32(threshold)
	sum, mean, above := 0.0, 0.0, 0
	lo, hi := float32(math.MaxFloat32), -float32(math.MaxFloat32)
	for i, value := range values {
		if i%1024 == 0 {
			if err := check(); err != nil {
				return nil, err
			}
		}
		v := float32(value)
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return nil, fmt.Errorf("telemetry reading cannot be represented as finite f32")
		}
		sum += float64(v)
		if v < lo {
			lo = v
		}
		if v > hi {
			hi = v
		}
		if v > cutoff {
			above++
		}
	}
	if err := check(); err != nil {
		return nil, err
	}
	if len(values) == 0 {
		lo, hi = 0, 0
	} else {
		mean = sum / float64(len(values))
	}
	result := map[string]float64{"count": float64(len(values))}
	outputs := [...]float32{float32(sum), float32(mean), lo, hi, float32(above)}
	for i, name := range [...]string{"sum", "mean", "min", "max", "above"} {
		value := float64(outputs[i])
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, fmt.Errorf("telemetry kernel produced an invalid %s", name)
		}
		result[name] = value
	}
	return result, nil
}

type AggregateRecord struct {
	Group    string                     `json:"group"`
	From     time.Time                  `json:"from,omitzero"`
	To       time.Time                  `json:"to,omitzero"`
	Count    int                        `json:"count"`
	Values   map[string]float64         `json:"values,omitempty"`
	Metadata map[string]json.RawMessage `json:"metadata,omitempty"`
}

type AggregateState struct {
	Groups map[string]AggregateRecord `json:"groups"`
}

type thresholdRaise struct {
	Since   time.Time `json:"since"`
	Alerted bool      `json:"alerted"`
}

type ThresholdState struct {
	Raised map[string]thresholdRaise `json:"raised,omitempty"`
}

type Alert struct {
	Node     string                     `json:"node"`
	Group    string                     `json:"group"`
	Field    string                     `json:"field"`
	Value    float64                    `json:"value"`
	High     float64                    `json:"high"`
	Low      float64                    `json:"low"`
	Severity string                     `json:"severity,omitempty"`
	State    string                     `json:"state"`
	At       time.Time                  `json:"at"`
	Batch    string                     `json:"batch"`
	Metadata map[string]json.RawMessage `json:"metadata,omitempty"`
}

var aggregateMeasures = []string{"count", "sum", "mean", "min", "max", "above"}

func streamOutputName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	first := name[0]
	if !(first >= 'A' && first <= 'Z' || first >= 'a' && first <= 'z') {
		return false
	}
	for i := 1; i < len(name); i++ {
		b := name[i]
		if b != '_' && (b < '0' || b > '9') && (b < 'A' || b > 'Z') && (b < 'a' || b > 'z') {
			return false
		}
	}
	return true
}

func checkOperators(declared platform.Continuous) error {
	a, t := declared.Aggregate, declared.Threshold
	if declared.Intake != nil {
		if err := declared.Intake.Retry.Check(); err != nil {
			return err
		}
	}
	if declared.Window != nil {
		if err := declared.Window.Retry.Check(); err != nil {
			return err
		}
	}
	if declared.Concurrency < 0 || declared.Concurrency > 8 {
		return fmt.Errorf("a stream fold runs with 1 to 8 partition workers")
	}
	if declared.Concurrency > 1 && declared.Window == nil {
		return fmt.Errorf("a stream fold with more than one partition worker needs the declared event-time window")
	}
	if output := declared.Output; output != nil {
		if a == nil || output.Node == "" || !streamOutputName(output.Name) || output.Name == "batch" || output.Name == "alerts" || output.Mode != "replace" {
			return fmt.Errorf("a stream output needs an aggregate, a safe variable name, and replace mode")
		}
	}
	if a == nil && t == nil {
		return nil
	}
	if declared.Window == nil {
		return fmt.Errorf("a streaming aggregate needs the declared event-time window")
	}
	if a != nil {
		if a.Kernel != "" && a.Kernel != "wasm:f32-window-stats" {
			return fmt.Errorf("the retained stream aggregate kernel is unavailable")
		}
		if a.Node == "" || len(a.Measures) == 0 || len(a.Measures) > len(aggregateMeasures) {
			return fmt.Errorf("a stream aggregate needs a node and bounded measures")
		}
		seen := map[string]bool{}
		for _, m := range a.Measures {
			if !slices.Contains(aggregateMeasures, m) || seen[m] {
				return fmt.Errorf("a stream aggregate measures %s once, from %v", m, aggregateMeasures)
			}
			seen[m] = true
		}
		if len(a.Group) > 8 {
			return fmt.Errorf("a stream aggregate groups by at most 8 fields")
		}
	}
	if t != nil {
		if a == nil {
			return fmt.Errorf("a stream threshold needs the aggregate whose field it reads")
		}
		if t.Node == "" || t.Field == "" || !slices.Contains(a.Measures, t.Field) {
			return fmt.Errorf("a stream threshold reads field %q, which the aggregate does not produce", t.Field)
		}
		if !(t.Low < t.High) || t.DebounceMS < 0 || t.DebounceMS > 600000 {
			return fmt.Errorf("a stream threshold needs low < high and a bounded debounce")
		}
	}

	return nil
}

func foldDeclaredOperators(frame *BatchFrame, declared platform.Continuous, batchID string, now time.Time) (stats []AggregateRecord, alerts []Alert, refusal *kernel.Error) {
	stats, alerts = []AggregateRecord{}, []Alert{}
	frame.Workers = 0
	if err := checkOperators(declared); err != nil {
		return stats, alerts, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	w := declared.Window
	if w == nil {
		return stats, alerts, nil
	}
	window := WindowState{Rows: []Signal{}}
	if raw := frame.State[w.Node]; len(raw) > 0 {
		if json.Unmarshal(raw, &window) != nil || window.Rows == nil {
			return stats, alerts, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The saved event-time window cannot be decoded")
		}
	}
	if !window.Ready {
		return stats, alerts, nil
	}
	a := declared.Aggregate
	if a == nil {
		return stats, alerts, nil
	}
	classes, peak, refusal := classifyAggregateRows(a, windowSignals(window, *w), declared)
	if refusal != nil {
		return stats, alerts, refusal
	}
	frame.Workers = peak
	invalid := map[signalIdentity]bool{}
	byGroup := map[string][]float64{}
	groupPartitions := map[string]string{}
	groupMetadata := map[string]map[string]json.RawMessage{}
	order := []string{}
	for i, row := range windowSignals(window, *w) {
		if classes[i].why != "" {
			frame.DeadLetters = append(frame.DeadLetters, DeadLetter{Batch: batchID, Partition: row.Partition, Key: row.Key, At: row.At, RecordedAt: now, Value: string(row.Value), Reason: classes[i].why})
			invalid[windowSignalKey(row)] = true
			continue
		}
		if _, seen := byGroup[classes[i].group]; !seen {
			order = append(order, classes[i].group)
		}
		byGroup[classes[i].group] = append(byGroup[classes[i].group], classes[i].value)
		groupPartitions[classes[i].group] = row.Partition
		var metadata map[string]json.RawMessage
		if json.Unmarshal(row.Value, &metadata) == nil {
			groupMetadata[classes[i].group] = metadata
		}
	}
	if len(invalid) > 0 {
		window.Rows = slices.DeleteFunc(window.Rows, func(row Signal) bool { return invalid[windowSignalKey(row)] })
		if raw, err := json.Marshal(window); err == nil {
			frame.State[w.Node] = raw
		}
	}
	state := AggregateState{Groups: map[string]AggregateRecord{}}
	if raw := frame.State[a.Node]; len(raw) > 0 {
		if json.Unmarshal(raw, &state) != nil || state.Groups == nil {
			return stats, alerts, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The saved aggregate state cannot be decoded")
		}
	}
	thresholds := ThresholdState{Raised: map[string]thresholdRaise{}}
	if t := declared.Threshold; t != nil {
		if raw := frame.State[t.Node]; len(raw) > 0 {
			if json.Unmarshal(raw, &thresholds) != nil || thresholds.Raised == nil {
				return stats, alerts, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The saved threshold state cannot be decoded")
			}
		}
	}
	slices.Sort(order)
	at := window.EmittedAt
	if at.IsZero() {
		at = now
	}
	records := make([]AggregateRecord, len(order))
	partitionGroups := map[string][]int{}
	partitions := []string{}
	for i, group := range order {
		partition := groupPartitions[group]
		if _, exists := partitionGroups[partition]; !exists {
			partitions = append(partitions, partition)
		}
		partitionGroups[partition] = append(partitionGroups[partition], i)
	}
	peak, err := (foldPool{limit: declared.Concurrency}).run(len(partitions), func(partition int, cancelled func() bool) *kernel.Error {
		for _, i := range partitionGroups[partitions[partition]] {
			if cancelled() {
				return nil
			}
			group, values := order[i], byGroup[order[i]]
			var measured map[string]float64
			if a.Kernel == "wasm:f32-window-stats" {
				var err error
				measured, err = windowF32Stats(values, a.Above, cancelled)
				if err != nil {
					return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
				}
			} else {
				sum, lo, hi, above := 0.0, values[0], values[0], 0.0
				for _, value := range values {
					sum, lo, hi = sum+value, min(lo, value), max(hi, value)
					if value > a.Above {
						above++
					}
				}
				measured = map[string]float64{"count": float64(len(values)), "sum": sum, "mean": sum / float64(len(values)), "min": lo, "max": hi, "above": above}
			}
			record := AggregateRecord{Group: group, Count: len(values), Values: map[string]float64{}, Metadata: groupMetadata[group]}
			for _, name := range a.Measures {
				record.Values[name] = measured[name]
			}
			records[i] = record
		}
		return nil
	})
	if err != nil {
		return stats, alerts, err
	}
	frame.Workers = peak
	for _, record := range records {
		group := record.Group
		record.From, record.To = at.Add(-time.Duration(w.WindowMS)*time.Millisecond), at
		state.Groups[group] = record
		stats = append(stats, record)
		if t := declared.Threshold; t != nil {
			emit, next, ok := advanceThreshold(*t, thresholds.Raised[group], record, at, batchID)
			if !ok {
				return stats, alerts, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The aggregate record has no value for the declared threshold field")
			}
			if emit != nil {
				alerts = append(alerts, *emit)
			}
			if next == nil {
				delete(thresholds.Raised, group)
			} else {
				thresholds.Raised[group] = *next
			}
		}
	}
	for group := range state.Groups {
		if _, present := byGroup[group]; !present {
			if _, raised := thresholds.Raised[group]; !raised {
				delete(state.Groups, group)
			}
		}
	}
	if raw, err := json.Marshal(state); err != nil {
		return stats, alerts, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The aggregate state cannot be encoded")
	} else if declared.State > 0 && len(raw) > declared.State {
		return stats, alerts, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The aggregate state exceeds its declared budget")
	} else {
		frame.State[a.Node] = raw
	}
	if t := declared.Threshold; t != nil {
		raw, err := json.Marshal(thresholds)
		if err != nil {
			return stats, alerts, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The threshold state cannot be encoded")
		}
		if declared.State > 0 && len(raw) > declared.State {
			return stats, alerts, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The threshold state exceeds its declared budget")
		}
		frame.State[t.Node] = raw
	}
	if declared.FrameBytes > 0 {
		raw, err := json.Marshal(frame)
		if err != nil || len(raw) > declared.FrameBytes {
			return stats, alerts, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The batch frame exceeds its declared budget")
		}
	}
	frame.StateBytes = stateSize(frame.State)
	return stats, alerts, nil
}

type rowClass struct {
	group string
	value float64
	why   string
}

func classifyAggregateRows(a *platform.StreamAggregate, rows []Signal, declared platform.Continuous) ([]rowClass, int, *kernel.Error) {
	classes := make([]rowClass, len(rows))
	if declared.Concurrency <= 1 {
		for i, row := range rows {
			group, value, why := aggregateRow(*a, row)
			classes[i] = rowClass{group: group, value: value, why: why}
		}
		return classes, 1, nil
	}
	partitions := []string{}
	byPartition := map[string][]int{}
	for i, row := range rows {
		if _, known := byPartition[row.Partition]; !known {
			partitions = append(partitions, row.Partition)
		}
		byPartition[row.Partition] = append(byPartition[row.Partition], i)
	}
	peak, refusal := (foldPool{limit: declared.Concurrency}).run(len(partitions), func(g int, cancelled func() bool) *kernel.Error {
		for _, i := range byPartition[partitions[g]] {
			if cancelled() {
				return nil
			}
			group, value, why := aggregateRow(*a, rows[i])
			classes[i] = rowClass{group: group, value: value, why: why}
		}
		return nil
	})
	if refusal != nil {
		return nil, 0, refusal
	}
	return classes, peak, nil
}

func aggregateRow(a platform.StreamAggregate, row Signal) (group string, value float64, why string) {
	if len(a.Group) == 0 {
		if row.Key == "" {
			return "", 0, "aggregate: no group identity"
		}
		group = row.Partition + "\x00" + row.Key
	} else {
		var object map[string]any
		if json.Unmarshal(row.Value, &object) != nil || object == nil {
			return "", 0, "aggregate: value is not an object with the declared group fields"
		}
		parts := make([]string, 0, len(a.Group))
		for _, name := range a.Group {
			switch v := object[name].(type) {
			case string:
				parts = append(parts, v)
			case float64:
				parts = append(parts, fmt.Sprint(v))
			default:
				return "", 0, fmt.Sprintf("aggregate: no group field %s", name)
			}
		}
		group = string(platform.Raw([]any{row.Partition, parts}))
	}
	if a.Signal == "" {
		if json.Unmarshal(row.Value, &value) != nil {
			return "", 0, "aggregate: value is not a number"
		}
		return group, value, ""
	}
	var object map[string]any
	if json.Unmarshal(row.Value, &object) != nil || object == nil {
		return "", 0, "aggregate: value is not an object with the declared signal"
	}
	n, ok := object[a.Signal].(float64)
	if !ok {
		return "", 0, fmt.Sprintf("aggregate: no numeric %s", a.Signal)
	}
	return group, n, ""
}

func advanceThreshold(t platform.StreamThreshold, prior thresholdRaise, record AggregateRecord, at time.Time, batchID string) (alert *Alert, next *thresholdRaise, ok bool) {
	value, ok := record.Values[t.Field]
	if !ok {
		return nil, nil, false
	}
	emit := func(state string) *Alert {
		return &Alert{Node: t.Node, Group: record.Group, Field: t.Field, Value: value, High: t.High, Low: t.Low, Severity: t.Severity, State: state, At: at, Batch: batchID, Metadata: record.Metadata}
	}
	if prior.Since.IsZero() {
		if value < t.High {
			return nil, nil, true
		}
		raise := thresholdRaise{Since: at}
		if t.DebounceMS == 0 {
			raise.Alerted = true
			return emit("triggered"), &raise, true
		}
		return nil, &raise, true
	}
	if value <= t.Low {
		if prior.Alerted {
			return emit("cleared"), nil, true
		}
		return nil, nil, true
	}
	if value < t.High || prior.Alerted {
		return nil, &prior, true
	}
	if at.Sub(prior.Since) < time.Duration(t.DebounceMS)*time.Millisecond {
		return nil, &prior, true
	}
	prior.Alerted = true
	return emit("triggered"), &prior, true
}

func aggregateOutput(records []AggregateRecord) []map[string]json.RawMessage {
	out := make([]map[string]json.RawMessage, 0, len(records))
	for _, record := range records {
		item := maps.Clone(record.Metadata)
		if item == nil {
			item = map[string]json.RawMessage{}
		}
		for key, value := range record.Values {
			item[key] = platform.Raw(value)
		}
		item["group"], item["from"], item["to"] = platform.Raw(record.Group), platform.Raw(record.From), platform.Raw(record.To)
		item["count"] = platform.Raw(record.Count)
		out = append(out, item)
	}
	return out
}

func alertOutput(alerts []Alert) []map[string]json.RawMessage {
	out := make([]map[string]json.RawMessage, 0, len(alerts))
	for _, alert := range alerts {
		item := maps.Clone(alert.Metadata)
		if item == nil {
			item = map[string]json.RawMessage{}
		}
		for key, value := range map[string]any{"node": alert.Node, "group": alert.Group, "field": alert.Field, "reading": alert.Value, "value": alert.Value, "high": alert.High, "low": alert.Low, "severity": alert.Severity, "state": alert.State, "at": alert.At, "batch": alert.Batch} {
			item[key] = platform.Raw(value)
		}
		out = append(out, item)
	}
	return out
}

type WindowState struct {
	Rows      []Signal  `json:"rows"`
	EmittedAt time.Time `json:"emittedAt,omitzero"`
	Ready     bool      `json:"ready"`
	Duplicate int       `json:"duplicate"`
}

func checkWindow(w platform.StreamWindow) error {
	if w.Node == "" || w.WindowMS < 100 || w.WindowMS > 3600000 || w.SlideMS < 1 || w.SlideMS > w.WindowMS || w.WatermarkMS < 0 || w.WatermarkMS > 600000 || w.MaxRecords < 1 || w.MaxRecords > 1000000 || !slices.Contains([]string{"sideOutput", "accept", "reject"}, w.LateEvents) {
		return fmt.Errorf("a stream window needs a node, bounded event-time periods, row budget and late-event policy")
	}
	return nil
}

func foldWindow(frame *BatchFrame, declared platform.Continuous, batch Batch, now time.Time) (folded, rejected int, refusal *kernel.Error) {
	w := *declared.Window
	if err := checkWindow(w); err != nil {
		return 0, 0, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	state := WindowState{Rows: []Signal{}}
	if raw := frame.State[w.Node]; len(raw) > 0 {
		if json.Unmarshal(raw, &state) != nil || state.Rows == nil {
			return 0, 0, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The saved event-time window cannot be decoded")
		}
	}
	watermark := maxTime(frame.Watermark, now)
	for _, signal := range batch.Signals {
		watermark = maxTime(watermark, signal.At)
	}
	boundary := windowBoundary(w, watermark, now)
	retentionBoundary := boundary
	if !boundary.After(state.EmittedAt) {
		retentionBoundary = state.EmittedAt.Add(time.Duration(w.SlideMS) * time.Millisecond)
		if w.LateEvents == "accept" {
			retentionBoundary = state.EmittedAt
		}
	}
	oldest := retentionBoundary.Add(-time.Duration(w.WindowMS) * time.Millisecond)
	seen := map[signalIdentity]Signal{}
	for _, row := range state.Rows {
		seen[windowSignalKey(row)] = row
	}
	added := map[signalIdentity]bool{}
	state.Rows = slices.DeleteFunc(slices.Clone(state.Rows), func(s Signal) bool { return s.At.Before(oldest) })
	appendDead := func(signal Signal, reason string) {
		frame.DeadLetters = append(frame.DeadLetters, DeadLetter{Batch: batch.ID, Partition: signal.Partition, Key: signal.Key, At: signal.At, RecordedAt: now, Reason: reason, Value: string(signal.Value)})
		rejected++
	}
	lateAccepted := false
	for _, signal := range batch.Signals {
		if signal.At.IsZero() || signal.Key == "" {
			appendDead(signal, "missing event identity or time")
			continue
		}
		key := windowSignalKey(signal)
		if prior, exists := seen[key]; exists {
			if !prior.At.Equal(signal.At) || !bytes.Equal(prior.Value, signal.Value) {
				return 0, 0, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The event identity was reused with different content")
			}
			state.Duplicate++
			continue
		}
		seen[key] = signal
		if signal.At.Before(oldest) {
			switch w.LateEvents {
			case "reject":
				return 0, 0, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "An event is outside the declared window and watermark grace")
			case "sideOutput":
				appendDead(signal, "late: outside the event-time window and watermark grace")
				continue
			case "accept":
				lateAccepted = true
			}
		}
		signal.Value = slices.Clone(signal.Value)
		state.Rows = append(state.Rows, signal)
		added[key] = true
		folded++
	}
	slices.SortFunc(state.Rows, func(a, b Signal) int {
		if n := a.At.Compare(b.At); n != 0 {
			return n
		}
		if a.Partition != b.Partition {
			if a.Partition < b.Partition {
				return -1
			}
			return 1
		}
		if a.Key < b.Key {
			return -1
		}
		if a.Key > b.Key {
			return 1
		}
		return 0
	})
	if overflow := len(state.Rows) - w.MaxRecords; overflow > 0 {
		for _, signal := range state.Rows[:overflow] {
			appendDead(signal, "overflow: event-time window row budget")
			if added[windowSignalKey(signal)] {
				folded--
			}
		}
		state.Rows = slices.Clone(state.Rows[overflow:])
	}
	pruneDeadLetters(frame, declared, now)
	newSlide := state.EmittedAt.IsZero() || boundary.After(state.EmittedAt)
	state.Ready = false
	if newSlide {
		state.EmittedAt = boundary
		state.Ready = len(windowSignals(state, w)) > 0 || declared.Entry != "" || declared.Aggregate != nil
	} else if lateAccepted {
		state.Ready = true
	}
	raw, err := json.Marshal(state)
	if err != nil || declared.State > 0 && len(raw) > declared.State {
		return 0, 0, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The event-time window exceeds its declared state budget")
	}
	frame.State[w.Node], frame.Watermark = raw, watermark
	return folded, rejected, nil
}

func windowBoundary(w platform.StreamWindow, watermark, now time.Time) time.Time {
	high := maxTime(watermark, now)
	grace := time.Duration(w.WatermarkMS) * time.Millisecond
	return high.Add(-grace).UTC().Truncate(time.Duration(w.SlideMS) * time.Millisecond)
}

func windowSignals(state WindowState, w platform.StreamWindow) []Signal {
	if state.EmittedAt.IsZero() {
		return nil
	}
	start := state.EmittedAt.Add(-time.Duration(w.WindowMS) * time.Millisecond)
	rows := make([]Signal, 0, len(state.Rows))
	for _, signal := range state.Rows {
		if (w.LateEvents == "accept" || !signal.At.Before(start)) && signal.At.Before(state.EmittedAt) {
			rows = append(rows, signal)
		}
	}
	return rows
}

type signalIdentity struct{ partition, key string }

func windowSignalKey(signal Signal) signalIdentity {
	return signalIdentity{signal.Partition, signal.Key}
}

func maxTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

type foldPool struct{ limit int }

func (p foldPool) run(jobs int, job func(i int, cancelled func() bool) *kernel.Error) (int, *kernel.Error) {
	if jobs == 0 {
		return 0, nil
	}
	var (
		next   = make(chan int)
		done   = make(chan struct{})
		once   sync.Once
		wg     sync.WaitGroup
		active atomic.Int64
		peak   atomic.Int64
		mu     sync.Mutex
		first  *kernel.Error
	)
	limit := min(max(p.limit, 1), jobs)
	stopped := func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}
	stop := func(err *kernel.Error) {
		once.Do(func() {
			mu.Lock()
			first = err
			mu.Unlock()
			close(done)
		})
	}
	for range limit {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				if stopped() {
					continue
				}
				n := active.Add(1)
				for {
					was := peak.Load()
					if n <= was || peak.CompareAndSwap(was, n) {
						break
					}
				}
				if err := job(i, stopped); err != nil {
					stop(err)
				}
				active.Add(-1)
			}
		}()
	}
	for i := 0; i < jobs && !stopped(); i++ {
		next <- i
	}
	close(next)
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	return int(peak.Load()), first
}
