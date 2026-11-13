package host

import (
	"context"

	"slices"
	"time"

	"platformkernel/kernel"

	"platformserver/platform"
	dataowner "platformserver/services/data"
)

type ObservationEvaluation struct {
	Observation *platform.ObservationResult `json:"observation,omitempty"`
	Matched     bool                        `json:"matched"`
	Receipt     *SubmissionAnswer           `json:"receipt,omitempty"`
	Reused      bool                        `json:"reused,omitempty"`
}

func (t *Tenant) Observations(m platform.Member) ([]dataowner.ObservationInfo, *kernel.Error) {
	return recordRead(t, context.Background(), func(t *Tenant) ([]dataowner.ObservationInfo, *kernel.Error) { return t.scopedObservations(m) })
}

func (t *Tenant) scopedObservations(m platform.Member) ([]dataowner.ObservationInfo, *kernel.Error) {
	if refusal := t.Admits(m); refusal != nil {
		return nil, refusal
	}
	definitions, err := t.observationDefinitions(m)
	if err != nil {
		return nil, dataowner.ObservationUnavailable(err)
	}
	return dataowner.ObservationInfos(definitions), nil
}

func (t *Tenant) observationDefinitions(m platform.Member) ([]platform.Definition, error) {
	definitions, err := t.ownedAssetDefinitions(t.definitions)
	if err != nil {
		return nil, err
	}
	definitions = slices.DeleteFunc(definitions, func(def platform.Definition) bool { return def.Observation == nil })
	return t.definitionsFrom(m, definitions), nil
}

func (t *Tenant) ReadObservation(ctx context.Context, m platform.Member, ref platform.AssetRef, q dataowner.ObservationRead, now time.Time) (platform.ObservationResult, *kernel.Error) {
	return t.observationSources().Read(ctx, q, now, func() (dataowner.ObservationPlan, *kernel.Error) {
		return recordRead(t, ctx, func(t *Tenant) (dataowner.ObservationPlan, *kernel.Error) {
			return t.observationSources().Prepare(m, ref, q.Record, now, false)
		})
	})
}
