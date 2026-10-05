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

// Continuous Flow (ADR-0047 §13, plan A; ordered by the owner on 2026-10-05).
//
// Plan A keeps the original Flow as the only owner that advances state: a
// continuous instance waits in its own token, and each arriving batch is one
// decision — a batch frame on the instance carries the consumed cursor, the
// watermark, the window/aggregate state and the dead letters, and the accepted
// result commits that frame together with the outputs it produced. A batch that
// cannot be accepted (unknown predecessor, over budget) changes nothing: the
// flow refuses it, so the source can back off instead of losing data silently.
//
// This is the batch-frame and state half of plan A's landing order. The
// per-call result channel for results above the inline budget is the host's
// staged-result facility (compute_channel.go); the worker ABI to use it is a
// later step and is not claimed here.

// BatchFrame is the continuous instance's own bookkeeping (ADR-0047 §13.3):
// what has been consumed, the watermark, per-node state and what could not be
// accepted. It is committed with the accepted result and replayed with it.
type BatchFrame struct {
	Cursor      string                     `json:"cursor,omitempty"`      // the batch identity consumed last
	Watermark   time.Time                  `json:"watermark,omitzero"`    // event time through which signals were folded
	Consumed    int                        `json:"consumed"`              // signals folded, ever
	Rejected    int                        `json:"rejected"`              // signals the flow refused, ever
	State       map[string]json.RawMessage `json:"state,omitempty"`       // node name → its state
	DeadLetters []DeadLetter               `json:"deadLetters,omitempty"` // what could not be folded, and why
}

// DeadLetter keeps one signal that could not be folded, with its cause, so it
// can be inspected and replayed under the original authorization (ADR-0047 §13.3).
type DeadLetter struct {
	Batch  string    `json:"batch"`
	Key    string    `json:"key,omitempty"`
	At     time.Time `json:"at,omitzero"`
	Reason string    `json:"reason"`
	Value  string    `json:"value,omitempty"`
}

// Batch is one accepted batch of a real source: its identity, its predecessor,
// and the signals it carries with their event times.
type Batch struct {
	ID          string
	Predecessor string
	Signals     []Signal
}

// Signal is one arriving measurement.
type Signal struct {
	Key   string
	At    time.Time
	Value json.RawMessage
}

// BatchOutcome is what the flow did with a batch.
type BatchOutcome struct {
	Cursor    string    `json:"cursor"`
	Watermark time.Time `json:"watermark,omitzero"`
	Consumed  int       `json:"consumed"`
	Rejected  int       `json:"rejected"`
	StateSize int       `json:"stateSize"`
}

// ConsumeBatch folds one batch into a continuous instance as one decision.
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
	if ended(x.State) {
		return BatchOutcome{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT, Message: "the instance has ended"}
	}
	declared := *d.Continuous
	if err := admitBatch(x.Batch, declared, batch); err != nil {
		return BatchOutcome{}, err
	}
	if x.Batch != nil && batch.ID == x.Batch.Cursor {
		// The same batch delivered twice: answer with the frame as it stands.
		return BatchOutcome{Cursor: x.Batch.Cursor, Watermark: x.Batch.Watermark, Consumed: x.Batch.Consumed, Rejected: x.Batch.Rejected,
			StateSize: stateSize(x.Batch.State)}, nil
	}
	var folded, rejected int
	err := f.step(c, id, now, func(_ *session, in *FlowInstance) {
		frame := in.Batch
		if frame == nil {
			frame = &BatchFrame{State: map[string]json.RawMessage{}}
			in.Batch = frame
		}
		folded, rejected = foldBatch(frame, declared, batch)
		// The frame's state and the outputs it produced are the accepted
		// result's data channel: both are committed with this decision.
		in.Outputs = maps.Clone(in.Outputs)
		if in.Outputs == nil {
			in.Outputs = map[string]json.RawMessage{}
		}
		for node, state := range frame.State {
			in.Outputs["state:"+node] = slices.Clone(state)
		}
		in.Outputs["batch"] = json.RawMessage(fmt.Sprintf(`{"cursor":%q,"consumed":%d,"rejected":%d}`, frame.Cursor, frame.Consumed, frame.Rejected))
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

// admitBatch is the batch's admission rule, apart from any tenant state: a batch
// over the declared budget, or one whose predecessor is not the frame's cursor,
// is refused so the source can retry; the same batch twice is idempotent.
func admitBatch(frame *BatchFrame, declared platform.Continuous, batch Batch) *kernel.Error {
	if batch.ID == "" {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: "the batch has no identity"}
	}
	if declared.Batch > 0 && len(batch.Signals) > declared.Batch {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT,
			Message: fmt.Sprintf("the batch carries %d signals, the flow accepts %d", len(batch.Signals), declared.Batch)}
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

// foldBatch folds every signal of a batch into the frame's state, keeping late
// and timeless signals as dead letters instead of dropping them silently.
func foldBatch(frame *BatchFrame, declared platform.Continuous, batch Batch) (folded, rejected int) {
	if frame.State == nil {
		frame.State = map[string]json.RawMessage{}
	}
	for _, signal := range batch.Signals {
		switch {
		case signal.At.IsZero():
			frame.DeadLetters = append(frame.DeadLetters, DeadLetter{Batch: batch.ID, Key: signal.Key, Reason: "no event time", Value: string(signal.Value)})
			rejected++
		case !frame.Watermark.IsZero() && signal.At.Before(frame.Watermark):
			frame.DeadLetters = append(frame.DeadLetters, DeadLetter{Batch: batch.ID, Key: signal.Key, At: signal.At, Reason: "late: before the watermark", Value: string(signal.Value)})
			rejected++
		default:
			fold(frame, declared, signal)
			folded++
		}
		if signal.At.After(frame.Watermark) {
			frame.Watermark = signal.At
		}
	}
	frame.Cursor = batch.ID
	frame.Consumed += folded
	frame.Rejected += rejected
	return folded, rejected
}

// fold folds one signal into the named node's state. Window and aggregate keep
// what §13.1's default flow needs: count, sum, min/max and the last values per
// key, so a threshold step can read them without re-reading the source.
func fold(frame *BatchFrame, declared platform.Continuous, signal Signal) {
	state := map[string]any{}
	if raw := frame.State["window"]; len(raw) > 0 {
		_ = json.Unmarshal(raw, &state)
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
	if raw, err := json.Marshal(state); err == nil && (declared.State == 0 || len(raw) <= declared.State) {
		frame.State["window"] = raw
	}
}

func stateSize(state map[string]json.RawMessage) int {
	total := 0
	for _, raw := range state {
		total += len(raw)
	}
	return total
}
