package kernel

import (
	"fmt"
	persistence "platformserver/kernel/store"
	"time"
)

func (s *Sequence) SettleAcceptedBatch(batch []persistence.AcceptedAppend, at time.Time, settlement Settlement, begin, end func(), apply func(persistence.AcceptedAppend, []byte, Settlement), quarantined func() bool) error {
	raw, err := s.AcceptBatch(batch, settlement.Quarantine)
	if err != nil {
		return err
	}
	begin()
	defer end()
	changed := map[string]bool{}
	coalesced := Settlement{
		Quarantine:        settlement.Quarantine,
		Applied:           func() {},
		Enqueue:           func(time.Time) {},
		ChangedOwner:      func(app string) { changed[app] = true },
		OperationsChanged: func(...string) {},
	}
	for i, item := range batch {
		if !StoredJSONEqual(raw[i], item.Entry.Body) {
			err = fmt.Errorf("committed import answer differs from its prepared result")
			settlement.Quarantine(err)
			return err
		}
		apply(item, raw[i], coalesced)
		if quarantined() {
			return fmt.Errorf("apply import result")
		}
	}
	settlement.Applied()
	if quarantined() {
		return fmt.Errorf("apply import position")
	}
	settlement.Enqueue(at)
	for owner := range changed {
		settlement.ChangedOwner(owner)
	}
	return nil
}
