package flow

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// The native operators the default streaming graph declares (ADR-0047 §13.1):
// the window's retained event-time rows feed a fixed aggregate, and the
// aggregate's records drive a hysteresis threshold. Both run inside the Flow's
// own accepted frame — the same owner, the same batch decision, no second
// executor. Outputs are the accepted result's data channel: the alert records
// this batch produced are carried as outputs, and only bounded per-group state
// stays in the frame.
//
// The declaration is checked, never inferred: a threshold field the aggregate
// does not produce is refused, which is what the imported graph's silent
// mean/reading mismatch must become.

// AggregateRecord is one group's fixed measures over the retained window. Its
// field names are exactly the declared measure names.
type AggregateRecord struct {
	Group    string                     `json:"group"`
	From     time.Time                  `json:"from,omitzero"`
	To       time.Time                  `json:"to,omitzero"`
	Count    int                        `json:"count"`
	Values   map[string]float64         `json:"values,omitempty"`
	Metadata map[string]json.RawMessage `json:"metadata,omitempty"`
}

// AggregateState is the aggregate node's frame state: the last record per
// group. Records for groups the current slide no longer carries are dropped
// once they are clear, so state stays bounded by the window, not by history.
type AggregateState struct {
	Groups map[string]AggregateRecord `json:"groups"`
}

// thresholdRaise is one group's hysteresis episode.
type thresholdRaise struct {
	Since   time.Time `json:"since"`
	Alerted bool      `json:"alerted"`
}

// ThresholdState is the threshold node's frame state.
type ThresholdState struct {
	Raised map[string]thresholdRaise `json:"raised,omitempty"`
}

// Alert is one emitted hysteresis event for the accepted result's outputs.
type Alert struct {
	Node     string                     `json:"node"`
	Group    string                     `json:"group"`
	Field    string                     `json:"field"`
	Value    float64                    `json:"value"`
	High     float64                    `json:"high"`
	Low      float64                    `json:"low"`
	Severity string                     `json:"severity,omitempty"`
	State    string                     `json:"state"` // triggered or cleared
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

// checkOperators validates the declared aggregate and threshold apart from any
// tenant state. It runs before folding, so a graph that cannot be wired is
// refused instead of silently never firing.
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
	// The declared partition workers are part of the same whole-declaration
	// check: the fold runs at most 8 of them, and more than one needs the
	// event-time window whose partitions they fold (ADR-0047 §13.3).
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

// foldOperators advances the declared operators by one accepted batch: the
// ready window's rows become aggregate records, and those records move the
// hysteresis state and produce this batch's alerts. Rows that cannot be
// aggregated are dead-lettered once and leave the window, so they are not
// re-reported on every slide.
func foldDeclaredOperators(frame *BatchFrame, declared platform.Continuous, batchID string, now time.Time) (stats []AggregateRecord, alerts []Alert, refusal *kernel.Error) {
	// Empty, never nil: a batch that produced no records says so, and the
	// accepted result replaces the previous batch's operator outputs instead
	// of leaving stale ones in place.
	stats, alerts = []AggregateRecord{}, []Alert{}
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
	// Group the retained rows under the declared measure contract. A row whose
	// value or group fields do not fit becomes a dead letter and leaves the
	// window; the batch itself still succeeds, as the graph's error policy says.
	//
	// Reading every retained row's value is the fold's heaviest per-row work,
	// so a declaration of concurrency classifies per source partition with
	// real workers. The merge below then walks the rows in their own order, so
	// sums, records, letters and hysteresis stay exactly the sequential ones.
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
				measured, err = windowF32Stats(values, a.Above)
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
	// A group the slide no longer carries keeps only a live episode: its
	// hysteresis must survive, its last record need not.
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
	// The record's state size must cover the operators' own state: a sealed
	// frame is expanded against the accepted summary, and a stale size would
	// make every later read of the artifact disagree with its record.
	frame.StateBytes = stateSize(frame.State)
	return stats, alerts, nil
}

// rowClass is one retained row's classification: the group and numeric value
// the aggregate takes from it, or why it cannot be aggregated.
type rowClass struct {
	group string
	value float64
	why   string
}

// classifyAggregateRows reads every retained row under the declared measure
// contract. A declaration of concurrency classifies per source partition with
// real workers — each worker owns its partition's rows alone — and the
// returned peak is the number of workers that really overlapped. Reading a
// row's value is where the fold spends its time, so the workers do the fold's
// real work, not a bookkeeping pass over it.
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
				return nil // a sibling refused the batch; its merge never runs
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

// aggregateRow takes one retained row's group and numeric value, or says why it
// cannot be aggregated.
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

// advanceThreshold runs one hysteresis step. A cleared episode returns a nil
// next state; a triggered alert is emitted once per episode.
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
			return nil, nil, true // never raised, still below the high mark
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
		return nil, &prior, true // inside the band, or already alarmed for this episode
	}
	if at.Sub(prior.Since) < time.Duration(t.DebounceMS)*time.Millisecond {
		return nil, &prior, true
	}
	prior.Alerted = true
	return emit("triggered"), &prior, true
}

// Output projection starts with explicitly captured metadata and then fixes
// semantic fields. Metadata cannot replace an accepted aggregate or alert.
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
