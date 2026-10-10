package flow

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// Threshold effects (ADR-0047 §13.3): what the hysteresis raises is delivered
// to the declaring app's own actions. The intent is durable — the frame holds
// it, so it survives the accepted result, sealing and recovery — and delivery
// runs on the flow's own timer as the app's automation principal with a stable
// idempotency key, exactly like a manual step's act. The original ledger is the
// deduplication authority: replaying the same batch reuses the same key, so the
// business effect is applied once; a refusal retries with the platform's own
// backoff and then becomes a dead letter with the refusal's reason.

// EffectRequest is one episode's durable delivery intent.
type EffectRequest struct {
	Seq      int64           `json:"seq"`  // the instance's own number for it
	Node     string          `json:"node"` // the graph node that raised it
	Action   string          `json:"action"`
	Target   string          `json:"target"`
	State    string          `json:"state"` // triggered or cleared
	At       time.Time       `json:"at,omitzero"`
	Batch    string          `json:"batch"`
	Key      string          `json:"key"` // the stable idempotency key
	Payload  json.RawMessage `json:"payload"`
	Attempts int             `json:"attempts,omitempty"`
	Error    string          `json:"error,omitempty"`
	Due      time.Time       `json:"due,omitzero"`
}

// pendingEffects is the most intents one frame holds. A backlog beyond it is
// dead-lettered with its reason rather than growing without bound.
const pendingEffects = 256

// effectKey binds the instance, the node, the episode and the batch that first
// saw it: the ledger answers a retry or a replay of that batch with the
// original accepted effect instead of delivering a second one.
func effectKey(instance string, effect platform.StreamEffect, alert Alert, batch string) string {
	return fmt.Sprintf("flow-effect:%x", sha256.Sum256(platform.Raw([]any{instance, effect.Node, effect.Action, alert.Group, alert.State, batch})))
}

// alertField reads one alert field as a string, for an effect's target.
func alertField(alert Alert, name string) string {
	switch name {
	case "group":
		return alert.Group
	case "node":
		return alert.Node
	case "field":
		return alert.Field
	case "severity":
		return alert.Severity
	case "state":
		return alert.State
	case "batch":
		return alert.Batch
	}
	return ""
}

// queueEffects turns one batch's alerts into durable intents on the frame.
// Alerts whose episode already has an intent for this batch are not queued
// twice, so a retried fold cannot double-queue.
func queueEffects(frame *BatchFrame, declared platform.Continuous, instance, batch string, alerts []Alert, now time.Time) {
	for _, alert := range alerts {
		for _, effect := range declared.Effects {
			if effect.Node != alert.Node || len(effect.States) > 0 && !slices.Contains(effect.States, alert.State) {
				continue
			}
			target := alert.Group
			if effect.Target != "" {
				target = alertField(alert, effect.Target)
			}
			if target == "" {
				continue
			}
			key := effectKey(instance, effect, alert, batch)
			if slices.ContainsFunc(frame.Pending, func(p EffectRequest) bool { return p.Key == key }) {
				continue
			}
			if len(frame.Pending) >= pendingEffects {
				frame.DeadLetters = append(frame.DeadLetters, frame.letter(batch, now, Signal{Key: target, Partition: alert.Node, At: alert.At, Value: platform.Raw(alert)}, "effect backlog: "+effect.Action))
				continue
			}
			frame.EffectSeq++
			frame.Pending = append(frame.Pending, EffectRequest{Seq: frame.EffectSeq, Node: effect.Node, Action: effect.Action, Target: target,
				State: alert.State, At: alert.At, Batch: batch, Key: key, Payload: platform.Raw(alert)})
		}
	}
}

// deliverEffects sends the instance's due intents, at most a bounded number per
// run. One attempt and its bookkeeping are one decision — the intent's removal
// when it is accepted, its backoff when it is refused, its reason as a dead
// letter once the platform's attempts are spent — so the flow keeps advancing
// its own revision and no attempt is lost or repeated by a key collision.
func (f *Flows) deliverEffects(c platform.Caller, id string, now time.Time) *kernel.Error {
	for round := 0; round < 8; round++ {
		x, ok := platform.Get[FlowInstance](c, id)
		if !ok || ended(x.State) || x.Batch == nil {
			return nil
		}
		index := slices.IndexFunc(x.Batch.Pending, func(p EffectRequest) bool { return !p.Due.After(now) })
		if index < 0 {
			return nil
		}
		delivered := false
		if err := f.update(c, id, now, func(ss *session, in *FlowInstance) *kernel.Error {
			d := f.def(in.Flow, in.Version)
			if d == nil || in.Batch == nil || index >= len(in.Batch.Pending) {
				return nil
			}
			pending := &in.Batch.Pending[index]
			spent := func(err *kernel.Error) {
				pending.Attempts++
				pending.Error = err.Code.String()
				if pending.Attempts < stepAttempts {
					pending.Due = now.Add(backoff(pending.Attempts))
					return
				}
				in.Batch.DeadLetters = append(in.Batch.DeadLetters, in.Batch.letter(pending.Batch, now,
					Signal{Key: pending.Target, Partition: pending.Node, At: pending.At, Value: pending.Payload},
					"effect "+pending.Action+": "+err.Code.String()))
				in.Batch.Pending = slices.Delete(in.Batch.Pending, index, index+1)
			}
			// The action is the declaring app's own, submitted as its
			// automation principal: authorization, payload validation and
			// idempotency stay the original ones.
			if _, declared, ok := f.host.Action(pending.Action); !ok || declared.Target == "" {
				spent(platform.Refuse(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA, "The effect action is not declared"))
				return nil
			}
			if _, err := ss.attemptAct(d.app, pending.Action, pending.Target, pending.Payload, pending.Key); err != nil {
				spent(err)
				return nil
			}
			ss.trace(in, pending.Node, "effect", pending.Action+" "+pending.Target, c.ID)
			in.Batch.Pending = slices.Delete(in.Batch.Pending, index, index+1)
			delivered = true
			return nil
		}); err != nil {
			return err
		}
		if !delivered {
			continue
		}
	}
	return nil
}
