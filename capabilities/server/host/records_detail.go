package host

import (
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/services/objects"
	"time"
)

type RecordView = objects.RecordView
type RecordSectionView = objects.RecordSectionView
type RecordDelivery = objects.RecordDelivery
type Related = objects.Related

const (
	RecordSectionLinks    = objects.RecordSectionLinks
	RecordSectionActivity = objects.RecordSectionActivity
	RecordSectionFiles    = objects.RecordSectionFiles
	RecordSectionWork     = objects.RecordSectionWork
	RecordSectionHistory  = objects.RecordSectionHistory
	RecordSectionComments = objects.RecordSectionComments
)

func (t *Tenant) recordOf(m platform.Member, typ, id string, now time.Time) (RecordView, *kernel.Error) {
	return t.objectOwner(t.Projection).RecordOf(m, typ, id, now)
}

func (t *Tenant) recordSection(m platform.Member, typ, id, section string, now time.Time) (RecordSectionView, *kernel.Error) {
	return t.objectOwner(t.Projection).RecordSection(m, typ, id, section, now)
}

func (t *Tenant) recordDetail(m platform.Member, typ, id string, now time.Time, sections ...string) (RecordView, *kernel.Error) {
	return t.objectOwner(t.Projection).RecordDetail(m, typ, id, now, sections...)
}

func (t *Tenant) PersonalReads() []platform.PersonalRead { return t.audit.PersonalReads() }

func (t *Tenant) recordDeliveries(target string, now time.Time) []RecordDelivery {
	t.opsMu.Lock()
	var effects []platform.Effect
	for i := len(t.outbound) - 1; i >= 0; i-- {
		if t.outbound[i].Target == target {
			effects = append(effects, t.outbound[i].Effect)
		}
	}
	t.opsMu.Unlock()
	out := make([]RecordDelivery, 0, len(effects))
	for _, effect := range effects {
		out = append(out, RecordDelivery{ID: effect.ID, Event: effect.Event, State: effect.State, At: effect.At, Message: effect.PublicMessage})
	}
	return out
}
