package flow

import (
	"time"

	"platformserver/platform"

	"platformkernel/kernel"
)

// tick starts the current period of every scheduled flow that has not started
// it yet (ADR-0057 E1). The period's start time is the instance key, so a
// restart, a replay or a slow tick never doubles a period; a period missed
// entirely is not made up.
func (f *Flows) tick(c platform.Caller, now time.Time) *kernel.Error {
	for id := range f.defs {
		d, ok := f.latest(id)
		if !ok || d.Start.Every <= 0 {
			continue
		}
		key := now.UTC().Truncate(d.Start.Every).Format(time.RFC3339)
		if _, known := platform.Get[FlowInstance](c, id+":"+key); known {
			continue
		}
		if err := f.start(f.host.Automation(c, ID), d, key, map[string]any{"period": key}, d.Start.OnBehalf, nil, "", now); err != nil {
			return err
		}
	}
	return nil
}
