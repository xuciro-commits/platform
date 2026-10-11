package flow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// WindowState is part of the original accepted frame, never a browser cache.
// The next aggregate consumes its retained rows after the source cursor commits.
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
	oldest := watermark.Add(-time.Duration(w.WindowMS) * time.Millisecond)
	late := oldest.Add(-time.Duration(w.WatermarkMS) * time.Millisecond)
	// Classification is where a batch's signals are read, matched against the
	// saved rows and sorted into additions, duplicates and dead letters. It is
	// the fold's own work, so a declaration of concurrency runs it per
	// partition with real workers; the merge below stays the only writer of
	// the frame, which keeps the accepted bytes identical either way.
	verdicts, peak, refusal := classifyWindowSignals(state.Rows, declared, w, oldest, late, batch.Signals)
	if refusal != nil {
		return 0, 0, refusal
	}
	if declared.Concurrency > 1 && peak > frame.Workers {
		frame.Workers = peak
	}
	added := map[signalIdentity]bool{}
	state.Rows = slices.DeleteFunc(slices.Clone(state.Rows), func(s Signal) bool { return s.At.Before(oldest) })
	for i, signal := range batch.Signals {
		switch verdicts[i].kind {
		case "duplicate":
			state.Duplicate++
		case "dead":
			frame.DeadLetters = append(frame.DeadLetters, frame.letter(batch.ID, now, signal, verdicts[i].reason))
			rejected++
		case "add":
			signal.Value = slices.Clone(signal.Value)
			state.Rows = append(state.Rows, signal)
			added[windowSignalKey(signal)] = true
			folded++
		default:
			return 0, 0, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A window worker left a signal unclassified")
		}
	}
	// Event order determines retention and grouping; physical delivery order
	// cannot make an older valid row late or collapse different partitions.
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
			frame.DeadLetters = append(frame.DeadLetters, frame.letter(batch.ID, now, signal, "overflow: event-time window row budget"))
			rejected++
			if added[windowSignalKey(signal)] {
				folded--
			}
		}
		state.Rows = slices.Clone(state.Rows[overflow:])
	}
	boundary := now.UTC().Truncate(time.Duration(w.SlideMS) * time.Millisecond)
	state.Ready = state.EmittedAt.IsZero() || boundary.After(state.EmittedAt)
	if state.Ready {
		state.EmittedAt = boundary
	}
	raw, err := json.Marshal(state)
	if err != nil || declared.State > 0 && len(raw) > declared.State {
		return 0, 0, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The event-time window exceeds its declared state budget")
	}
	frame.State[w.Node], frame.Watermark = raw, watermark
	return folded, rejected, nil
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

// signalVerdict is one batch signal's classification: what the merge does with
// it. A worker writes only its own partition's slots, the merge reads them in
// the batch's own order, and the accepted frame never depends on scheduling.
type signalVerdict struct {
	kind   string // add, duplicate or dead
	reason string
}

// classifyWindowSignals decides for every signal of one batch whether the
// window adds it, absorbs it as a duplicate or keeps it as a dead letter with
// its reason. A declaration of concurrency classifies per source partition
// with real workers: a saved row can only match a signal of its own partition,
// so each worker reads and advances its partition's saved rows alone. The
// returned peak is the number of workers that really overlapped.
func classifyWindowSignals(rows []Signal, declared platform.Continuous, w platform.StreamWindow, oldest, late time.Time, signals []Signal) ([]signalVerdict, int, *kernel.Error) {
	verdicts := make([]signalVerdict, len(signals))
	if declared.Concurrency <= 1 {
		seen := map[signalIdentity]Signal{}
		for _, row := range rows {
			seen[windowSignalKey(row)] = row
		}
		for i, signal := range signals {
			verdict, refusal := classifyWindowSignal(seen, signal, w, oldest, late)
			if refusal != nil {
				return nil, 0, refusal
			}
			verdicts[i] = verdict
		}
		return verdicts, 1, nil
	}
	partitions := []string{}
	byPartition := map[string][]int{}
	for i, signal := range signals {
		if _, known := byPartition[signal.Partition]; !known {
			partitions = append(partitions, signal.Partition)
		}
		byPartition[signal.Partition] = append(byPartition[signal.Partition], i)
	}
	saved := map[string]map[signalIdentity]Signal{}
	for _, partition := range partitions {
		saved[partition] = map[signalIdentity]Signal{}
	}
	for _, row := range rows {
		if seen, known := saved[row.Partition]; known {
			seen[windowSignalKey(row)] = row
		}
	}
	peak, refusal := (foldPool{limit: declared.Concurrency}).run(len(partitions), func(g int, cancelled func() bool) *kernel.Error {
		partition := partitions[g]
		seen := saved[partition]
		for _, i := range byPartition[partition] {
			if cancelled() {
				return nil // the batch is refused whole; its merge is never reached
			}
			verdict, refusal := classifyWindowSignal(seen, signals[i], w, oldest, late)
			if refusal != nil {
				return refusal
			}
			verdicts[i] = verdict
		}
		return nil
	})
	if refusal != nil {
		return nil, 0, refusal
	}
	return verdicts, peak, nil
}

// classifyWindowSignal is the one-signal rule both folds share: identity is
// the source partition and event key, not device or arrival order.
func classifyWindowSignal(seen map[signalIdentity]Signal, signal Signal, w platform.StreamWindow, oldest, late time.Time) (signalVerdict, *kernel.Error) {
	if signal.At.IsZero() || signal.Key == "" {
		return signalVerdict{kind: "dead", reason: "missing event identity or time"}, nil
	}
	key := windowSignalKey(signal)
	if prior, exists := seen[key]; exists {
		if !prior.At.Equal(signal.At) || !bytes.Equal(prior.Value, signal.Value) {
			return signalVerdict{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The event identity was reused with different content")
		}
		return signalVerdict{kind: "duplicate"}, nil
	}
	seen[key] = signal
	if signal.At.Before(late) || signal.At.Before(oldest) && w.LateEvents != "accept" {
		if w.LateEvents == "reject" {
			return signalVerdict{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "An event is outside the declared window")
		}
		return signalVerdict{kind: "dead", reason: "late: outside the event-time window"}, nil
	}
	return signalVerdict{kind: "add"}, nil
}
