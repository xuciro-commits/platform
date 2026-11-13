package host

import (
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/services/actions"
	"platformserver/services/objects"
	"time"
)

func (t *Tenant) recordOf(m platform.Member, typ, id string, now time.Time) (objects.RecordView, *kernel.Error) {
	return t.objectOwner(t.Projection).RecordOf(m, typ, id, now)
}

func (t *Tenant) recordDetail(m platform.Member, typ, id string, now time.Time, sections ...string) (objects.RecordView, *kernel.Error) {
	return t.objectOwner(t.Projection).RecordDetail(m, typ, id, now, sections...)
}

func (t *Tenant) PersonalReads() []platform.PersonalRead { return t.audit.PersonalReads() }

func (t *Tenant) recordDeliveries(target string, now time.Time) []objects.RecordDelivery {
	matched := t.outbound.Select(func(x actions.Queued) bool { return x.Target == target })
	var effects []platform.Effect
	for i := len(matched) - 1; i >= 0; i-- {
		effects = append(effects, matched[i].Effect)
	}
	out := make([]objects.RecordDelivery, 0, len(effects))
	for _, effect := range effects {
		out = append(out, objects.RecordDelivery{ID: effect.ID, Event: effect.Event, State: effect.State, At: effect.At, Message: effect.PublicMessage})
	}
	return out
}
