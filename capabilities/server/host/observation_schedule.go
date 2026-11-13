package host

import (
	"context"
	"time"

	"platformkernel/kernel"
	"platformserver/platform"
	dataowner "platformserver/services/data"
	controlowner "platformserver/services/security/control"
)

func (t *Tenant) EvaluateObservations(at time.Time) {
	if t.businessStopped() || !t.OwnerActive(controlowner.BuildApp) {
		return
	}
	t.observationSchedule().RunDue(at)
}

func (t *Tenant) observationSchedule() dataowner.ObservationSchedule {
	return dataowner.ObservationSchedule{Member: t.Member, Windows: &t.schedules.Windows,
		Definitions: func() ([]platform.Definition, error) {
			definitions, _ := coordinatedRecordRead(t, context.Background(), func(t *Tenant) ([]platform.Definition, *kernel.Error) {
				definitions, err := t.ownedAssetDefinitions(t.definitions)
				if err != nil {
					return nil, nil
				}
				return definitions, nil
			})
			return definitions, nil
		},
		Active: t.Projection.ObservationSourceActive, Publisher: t.Projection.ObservationPublisher,
		Records: func(member platform.Member, object string, query platform.Query, at time.Time) ([]any, int, *kernel.Error) {
			page, refusal := t.Records(context.Background(), member, object, query, at)
			return page.Records, page.Total, refusal
		},
		Evaluate: func(member platform.Member, ref platform.AssetRef, query dataowner.ObservationEvaluate, at time.Time) *kernel.Error {
			_, refusal := t.EvaluateObservation(context.Background(), member, ref, query, at)
			return refusal
		}}
}
