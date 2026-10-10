package flow

import (
	"crypto/sha256"
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
	Cursor      string                      `json:"cursor,omitempty"`      // the batch identity consumed last
	Fingerprint string                      `json:"fingerprint,omitempty"` // binds that cursor to the accepted batch bytes
	Watermark   time.Time                   `json:"watermark,omitzero"`    // event time through which signals were folded
	Consumed    int                         `json:"consumed"`              // signals folded, ever
	Rejected    int                         `json:"rejected"`              // signals the flow refused, ever
	State       map[string]json.RawMessage  `json:"state,omitempty"`       // node name → its state
	DeadLetters []DeadLetter                `json:"deadLetters,omitempty"` // what could not be folded, and why
	StateBytes  int                         `json:"stateBytes,omitempty"`
	Sealed      *platform.FlowStateArtifact `json:"sealed,omitempty"`
	Source      *SourceCheckpoint           `json:"source,omitempty"`
}

// SourceCheckpoint is the instance's own incremental position; it is accepted
// with its window state, rather than written onto the import source's cursor.
type SourceCheckpoint struct {
	Record  string   `json:"record"`
	Config  string   `json:"config"`
	Cursor  string   `json:"cursor"`
	Sources []string `json:"sources"`
}

// DeadLetter keeps one signal that could not be folded, with its cause, so it
// can be inspected and replayed under the original authorization (ADR-0047 §13.3).
type DeadLetter struct {
	Batch     string    `json:"batch"`
	Partition string    `json:"partition,omitempty"`
	Key       string    `json:"key,omitempty"`
	At        time.Time `json:"at,omitzero"`
	Reason    string    `json:"reason"`
	Value     string    `json:"value,omitempty"`
}

// Batch is one accepted batch of a real source: its identity, its predecessor,
// and the signals it carries with their event times.
type Batch struct {
	ID          string
	Predecessor string
	Signals     []Signal
	Source      *SourceCheckpoint `json:"Source,omitempty"`
}

// Signal is one arriving measurement.
type Signal struct {
	Key       string
	Partition string
	At        time.Time
	Value     json.RawMessage
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
	if x.Batch != nil && x.Batch.Sealed != nil {
		return BatchOutcome{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A sealed Flow frame is prepared on the host I/O lane")
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
		// The declared operators advance in the same decision as the fold: a
		// refusal here discards the batch, so the cursor never advances past
		// signals whose aggregate or threshold state could not be committed.
		stats, alerts, refusal := foldOperators(frame, declared, batch.ID, now)
		if refusal != nil {
			return refusal
		}
		// The frame's state and the outputs it produced are the accepted
		// result's data channel: both are committed with this decision.
		in.Outputs = maps.Clone(in.Outputs)
		if in.Outputs == nil {
			in.Outputs = map[string]json.RawMessage{}
		}
		for node, state := range frame.State {
			if len(state) <= 48<<10 {
				in.Outputs["state:"+node] = slices.Clone(state)
			} else {
				// The durable frame owns these bytes. A compact, verifiable
				// reference keeps token output limits instead of copying a large
				// window into every ordinary output value.
				in.Outputs["state:"+node] = platform.Raw(BatchStateReference{Instance: in.ID, Node: node, Digest: fmt.Sprintf("sha256:%x", sha256.Sum256(state)), Size: len(state)})
			}
		}
		in.Outputs["batch"] = json.RawMessage(fmt.Sprintf(`{"cursor":%q,"consumed":%d,"rejected":%d}`, frame.Cursor, frame.Consumed, frame.Rejected))
		// The operators' own outputs: the groups' fixed measures and the
		// hysteresis alerts this batch accepted, empty when this batch
		// produced none. They travel as the accepted result's data channel
		// like every other Flow output.
		if declared.Aggregate != nil {
			in.Outputs["stats"] = platform.Raw(stats)
		}
		if declared.Threshold != nil {
			in.Outputs["alerts"] = platform.Raw(alerts)
		}
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

// BatchStateReference names original instance state, not an arbitrary file or
// URL. A consumer must read that instance as its member and verify the digest.
type BatchStateReference struct {
	Instance string `json:"instance"`
	Node     string `json:"node"`
	Digest   string `json:"digest"`
	Size     int    `json:"size"`
}

// admitBatch is the batch's admission rule, apart from any tenant state: a batch
// over the declared budget, or one whose predecessor is not the frame's cursor,
// is refused so the source can retry; the same batch twice is idempotent.
func admitBatch(frame *BatchFrame, declared platform.Continuous, batch Batch) *kernel.Error {
	if declared.Intake != nil && batch.Source == nil || declared.Intake == nil && batch.Source != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A source checkpoint must match the declared intake")
	}
	if batch.ID == "" {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: "the batch has no identity"}
	}
	if declared.Batch > 0 && len(batch.Signals) > declared.Batch {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT,
			Message: fmt.Sprintf("the batch carries %d signals, the flow accepts %d", len(batch.Signals), declared.Batch)}
	}
	if declared.Batch < 0 || declared.State < 0 || declared.FrameBytes < 0 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Continuous batch budgets are non-negative")
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

// foldBatch folds every signal of a batch into the frame's state, keeping late
// and timeless signals as dead letters instead of dropping them silently.
func foldBatch(frame *BatchFrame, declared platform.Continuous, batch Batch, acceptedAt ...time.Time) (folded, rejected int, refusal *kernel.Error) {
	if err := admitBatch(frame, declared, batch); err != nil {
		return 0, 0, err
	}
	if frame.Cursor == batch.ID {
		return 0, 0, nil
	}
	// No cursor, watermark, counter or dead letter is committed before the
	// whole successor fits its declaration. Original state bytes stay intact.
	next := *frame
	next.State = maps.Clone(frame.State)
	next.DeadLetters = slices.Clone(frame.DeadLetters)
	if next.State == nil {
		next.State = map[string]json.RawMessage{}
	}
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
		for _, signal := range batch.Signals {
			switch {
			case signal.At.IsZero():
				next.DeadLetters = append(next.DeadLetters, DeadLetter{Batch: batch.ID, Key: signal.Key, Reason: "no event time", Value: string(signal.Value)})
				rejected++
			case !next.Watermark.IsZero() && signal.At.Before(next.Watermark):
				next.DeadLetters = append(next.DeadLetters, DeadLetter{Batch: batch.ID, Key: signal.Key, At: signal.At, Reason: "late: before the watermark", Value: string(signal.Value)})
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
	next.Cursor = batch.ID
	if batch.Source != nil {
		checkpoint := *batch.Source
		checkpoint.Sources = slices.Clone(batch.Source.Sources)
		next.Source = &checkpoint
	}
	next.Fingerprint, _ = batchFingerprint(batch)
	next.Consumed += folded
	next.Rejected += rejected
	next.StateBytes = stateSize(next.State)
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

// fold preserves the historical scalar count/sum/last profile. It is not the
// default graph's event-time window, aggregate or threshold implementation.
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
