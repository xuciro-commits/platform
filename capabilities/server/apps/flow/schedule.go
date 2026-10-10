package flow

import (
	"fmt"
	"maps"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// tick starts the current period of every scheduled flow that has not started
// it yet (ADR-0057 E1). The period's start time is the instance key, so a
// restart, a replay or a slow tick never doubles a period; a period missed
// entirely is not made up.
func (f *Flows) tick(c platform.Caller, now time.Time) *kernel.Error {
	continuous := map[string]*flowDef{}
	for _, id := range slices.Sorted(maps.Keys(f.defs)) {
		d, ok := f.latest(id)
		if ok && d.Start.Continuous {
			continuous[id] = d
		}
	}
	active := map[string]bool{}
	if len(continuous) > 0 {
		err := f.eachRunning(c, func(x FlowInstance) error {
			active[x.Flow] = true
			return nil
		})
		if err != nil {
			return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT, Message: err.Error()}
		}
	}
	for _, id := range slices.Sorted(maps.Keys(f.defs)) {
		d, ok := f.latest(id)
		if !ok {
			continue
		}
		if d.Start.Continuous {
			if active[id] || d.Start.OnBehalf == "" || f.host == nil {
				continue
			}
			if d.Start.Enabled != nil && !d.Start.Enabled(f.host.Automation(c, d.app)) {
				continue
			}
			if _, exists := f.host.Member(d.Start.OnBehalf); !exists {
				continue
			}
			key := fmt.Sprintf("continuous-v%d", d.Version)
			if _, known := platform.Get[FlowInstance](c, id+":"+key); known {
				continue // a completed/canceled start is an explicit stop, not a restart loop
			}
			if err := f.start(f.host.Automation(c, ID), d, key, nil, d.Start.OnBehalf, nil, "", now, true); err != nil {
				return err
			}
			active[id] = true
			continue
		}
		if d.Start.Every <= 0 {
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
