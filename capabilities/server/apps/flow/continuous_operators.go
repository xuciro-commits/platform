package flow

import (
	"encoding/json"
	"fmt"
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
	Group  string             `json:"group"`
	From   time.Time          `json:"from,omitzero"`
	To     time.Time          `json:"to,omitzero"`
	Count  int                `json:"count"`
	Values map[string]float64 `json:"values,omitempty"`
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
	Node     string    `json:"node"`
	Group    string    `json:"group"`
	Field    string    `json:"field"`
	Value    float64   `json:"value"`
	High     float64   `json:"high"`
	Low      float64   `json:"low"`
	Severity string    `json:"severity,omitempty"`
	State    string    `json:"state"` // triggered or cleared
	At       time.Time `json:"at"`
	Batch    string    `json:"batch"`
}

var aggregateMeasures = []string{"count", "sum", "mean", "min", "max"}

// checkOperators validates the declared aggregate and threshold apart from any
// tenant state. It runs before folding, so a graph that cannot be wired is
// refused instead of silently never firing.
func checkOperators(declared platform.Continuous) error {
	a, t := declared.Aggregate, declared.Threshold
	if a == nil && t == nil {
		return nil
	}
	if declared.Window == nil {
		return fmt.Errorf("a streaming aggregate needs the declared event-time window")
	}
	if a != nil {
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
func foldOperators(frame *BatchFrame, declared platform.Continuous, batchID string, now time.Time) (stats []AggregateRecord, alerts []Alert, refusal *kernel.Error) {
	stats, alerts, refusal = foldDeclaredOperators(frame, declared, batchID, now)
	if refusal == nil {
		frame.retain(declared.DeadLetter, now)
	}
	return stats, alerts, refusal
}

// retain keeps the instance's dead-letter asset inside its declared bounds
// (ADR-0047 §13.3): oldest-first by count, and by age when a TTL is declared.
// Every letter it drops is counted in the frame, so trimming is visible rather
// than silent, and the numbers of the letters that stay never change — a
// replay names a letter's Seq, not its position.
func (f *BatchFrame) retain(rule *platform.StreamDeadLetter, now time.Time) {
	if rule == nil || len(f.DeadLetters) == 0 {
		return
	}
	kept := f.DeadLetters[:0]
	for _, letter := range f.DeadLetters {
		if rule.TTLMS > 0 && !now.IsZero() && !letter.Seen.IsZero() && now.Sub(letter.Seen) > time.Duration(rule.TTLMS)*time.Millisecond {
			f.Dropped++
			continue
		}
		kept = append(kept, letter)
	}
	if over := len(kept) - rule.MaxRecords; over > 0 {
		f.Dropped += over
		kept = kept[over:]
	}
	f.DeadLetters = kept
}

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
	kept := make([]Signal, 0, len(window.Rows))
	byGroup := map[string][]float64{}
	order := []string{}
	for _, row := range window.Rows {
		group, value, why := aggregateRow(*a, row)
		if why != "" {
			frame.DeadLetters = append(frame.DeadLetters, frame.letter(batchID, now, row, why))
			continue
		}
		if _, seen := byGroup[group]; !seen {
			order = append(order, group)
		}
		byGroup[group] = append(byGroup[group], value)
		kept = append(kept, row)
	}
	if len(kept) != len(window.Rows) {
		window.Rows = kept
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
	for _, group := range order {
		values := byGroup[group]
		record := AggregateRecord{Group: group, Count: len(values), Values: map[string]float64{}}
		sum, lo, hi := 0.0, values[0], values[0]
		for _, v := range values {
			sum, lo, hi = sum+v, min(lo, v), max(hi, v)
		}
		for _, m := range a.Measures {
			switch m {
			case "count":
				record.Values["count"] = float64(record.Count)
			case "sum":
				record.Values["sum"] = sum
			case "mean":
				record.Values["mean"] = sum / float64(len(values))
			case "min":
				record.Values["min"] = lo
			case "max":
				record.Values["max"] = hi
			}
		}
		record.To = at
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
		group = fmt.Sprint(parts)
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
		return &Alert{Node: t.Node, Group: record.Group, Field: t.Field, Value: value, High: t.High, Low: t.Low, Severity: t.Severity, State: state, At: at, Batch: batchID}
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
		return emit("cleared"), nil, true
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
