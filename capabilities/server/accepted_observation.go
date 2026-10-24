package platformserver

import (
	"fmt"
	resultformat "platformserver/internal/acceptance/result"
	"slices"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/host"
	"platformserver/platform"
)

func (d *stagedDecision) stageObservers() *kernel.Error {
	if d.observed {
		return d.failure
	}
	count := len(d.events)
	for _, e := range slices.Clone(d.events) {
		for _, app := range d.tenant.liveApps() {
			_, observer := app.(host.Observer)
			if !observer {
				continue
			}
			owner, ok := d.stateApp(app).(host.AcceptedObserver)
			if !ok {
				unsupportedStagedEffect()
			}
			id := app.Manifest().ID
			c := platform.NewCaller(d, platform.Member{ID: "app:" + id, Tenant: d.tenant.ID}, id, false, true)
			raw, err := owner.PlanObserved(c, e, d.tenant.protocolEvents(e))
			if err != nil || owner.ValidateObserved(raw) != nil {
				return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
			}
			d.observations = append(d.observations, resultformat.Observation{App: id, Body: raw})
		}
	}
	if len(d.events) != count {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	d.observed = true
	return d.failure
}

func (t *Tenant) validateAcceptedObservations(saved []resultformat.Observation) error {
	for _, item := range saved {
		owner, ok := t.app(item.App).(host.AcceptedObserver)
		if !ok || len(item.Body) == 0 {
			return fmt.Errorf("accepted observation needs missing projection %s", item.App)
		}
		if err := owner.ValidateObserved(item.Body); err != nil {
			return err
		}
	}
	return nil
}

func (t *Tenant) applyAcceptedObservations(saved []resultformat.Observation) error {
	for _, item := range saved {
		if err := t.app(item.App).(host.AcceptedObserver).ApplyObserved(item.Body); err != nil {
			return err
		}
	}
	return nil
}
