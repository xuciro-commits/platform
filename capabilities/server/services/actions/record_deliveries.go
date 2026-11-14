package actions

import (
	"time"

	"platformserver/services/objects"
)

func (o *Outbox) RecordDeliveries(target string, _ time.Time) []objects.RecordDelivery {
	matched := o.Select(func(x Queued) bool { return x.Target == target })
	out := make([]objects.RecordDelivery, 0, len(matched))
	for i := len(matched) - 1; i >= 0; i-- {
		effect := matched[i].Effect
		out = append(out, objects.RecordDelivery{ID: effect.ID, Event: effect.Event, State: effect.State, At: effect.At, Message: effect.PublicMessage})
	}
	return out
}

func (o *Outbox) CopyTo(to *Outbox) {
	var effects []Queued
	for _, effect := range o.Select(nil) {
		effects = append(effects, Queued{Effect: effect.Effect, Since: effect.Since})
	}
	to.Restore(effects)
}
