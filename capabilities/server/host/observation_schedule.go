package host

import (
	"context"
	"platformkernel/kernel"
	dataowner "platformserver/services/data"
	controlowner "platformserver/services/security/control"

	"time"

	"platformserver/platform"
)

func (t *Tenant) EvaluateObservations(at time.Time) {
	if t.businessStopped() || !t.OwnerActive(controlowner.BuildApp) {
		return
	}
	sources, _ := coordinatedRecordRead(t, context.Background(), func(t *Tenant) ([]dataowner.ScheduledObservation, *kernel.Error) {
		definitions, err := t.ownedAssetDefinitions(t.definitions)
		if err != nil {
			return nil, nil
		}
		var sources []dataowner.ScheduledObservation
		for _, definition := range definitions {
			if definition.Observation == nil || !definition.Observation.Scheduled || definition.OwnerRecord == "" || !t.Projection.ObservationSourceActive(definition.OwnerRecord) {
				continue
			}
			if publisher, scopes, _ := t.Projection.ObservationPublisher(definition.OwnerRecord); publisher != "" {
				sources = append(sources, dataowner.ScheduledObservation{Ref: definition.Ref, Version: definition.Version, Definition: *definition.Observation, Publisher: publisher, Scopes: scopes})
			}
		}
		return sources, nil
	})
	dataowner.ObservationSchedule{Member: t.Member, Windows: &t.schedules.Windows,
		Records: func(m platform.Member, object string, q platform.Query, at time.Time) ([]any, int, *kernel.Error) {
			page, refusal := t.Records(context.Background(), m, object, q, at)
			return page.Records, page.Total, refusal
		},
		Evaluate: func(m platform.Member, ref platform.AssetRef, q dataowner.ObservationEvaluate, at time.Time) *kernel.Error {
			_, refusal := t.EvaluateObservation(context.Background(), m, ref, q, at)
			return refusal
		}}.Run(sources, at)
}
