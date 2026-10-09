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
	// Identity is (source partition, event key), not device or arrival order.
	seen := map[signalIdentity]Signal{}
	for _, row := range state.Rows {
		seen[windowSignalKey(row)] = row
	}
	added := map[signalIdentity]bool{}
	state.Rows = slices.DeleteFunc(slices.Clone(state.Rows), func(s Signal) bool { return s.At.Before(oldest) })
	appendDead := func(signal Signal, reason string) {
		frame.DeadLetters = append(frame.DeadLetters, DeadLetter{Batch: batch.ID, Partition: signal.Partition, Key: signal.Key, At: signal.At, Reason: reason, Value: string(signal.Value)})
		rejected++
	}
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
		if signal.At.Before(late) || signal.At.Before(oldest) && w.LateEvents != "accept" {
			if w.LateEvents == "reject" {
				return 0, 0, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "An event is outside the declared window")
			}
			appendDead(signal, "late: outside the event-time window")
			continue
		}
		signal.Value = slices.Clone(signal.Value)
		state.Rows = append(state.Rows, signal)
		added[key] = true
		folded++
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
			appendDead(signal, "overflow: event-time window row budget")
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
