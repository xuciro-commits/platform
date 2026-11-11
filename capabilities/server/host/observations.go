package host

import (
	"context"
	"platformserver/kernel/records"

	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

	"platformserver/platform"
	dataowner "platformserver/services/data"
)

const observationResponseBytes = 1 << 20

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
	out := []dataowner.ObservationInfo{}
	for _, definition := range definitions {
		if definition.Observation != nil {
			out = append(out, dataowner.ObservationInfo{Ref: definition.Ref, Version: definition.Version, Definition: *definition.Observation})
		}
	}
	return out, nil
}

func (t *Tenant) observationDefinitions(m platform.Member) ([]platform.Definition, error) {
	definitions, err := t.ownedAssetDefinitions(t.definitions)
	if err != nil {
		return nil, err
	}
	definitions = slices.DeleteFunc(definitions, func(def platform.Definition) bool { return def.Observation == nil })
	return t.definitionsFrom(m, definitions), nil
}

func (t *Tenant) observationSourceActive(record string) bool {
	typ, id, valid := strings.Cut(record, "/")
	if !valid {
		return false
	}
	t.Projection.Lock()
	defer t.Projection.Unlock()
	owner := t.Projection.Type(typ)
	if owner == nil {
		return false
	}
	row := owner.Rows().Get(id)
	return row != nil && !records.Header(row.Value).Archived && records.BusinessField(owner.Info(), row.Value, "state") == "published"
}

func (t *Tenant) ReadObservation(ctx context.Context, m platform.Member, ref platform.AssetRef, q dataowner.ObservationRead, now time.Time) (platform.ObservationResult, *kernel.Error) {
	if err := dataowner.CheckObservationRead(&q, now); err != nil {
		return platform.ObservationResult{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	plan, refusal := recordRead(t, ctx, func(t *Tenant) (dataowner.ObservationPlan, *kernel.Error) {
		return t.observationSources().Prepare(m, ref, q.Record, now, false)
	})
	if refusal != nil {
		return platform.ObservationResult{}, refusal
	}
	if q.Until.Sub(q.Since) > time.Duration(plan.Info.Definition.ExpectedRetentionSeconds)*time.Second {
		return platform.ObservationResult{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Observation time window exceeds the declared retention")
	}
	result, err := t.observationSources().Fetch(ctx, plan, q, now)
	if err != nil {
		return platform.ObservationResult{}, dataowner.ObservationUnavailable(err)
	}
	current, refusal := recordRead(t, ctx, func(t *Tenant) (dataowner.ObservationPlan, *kernel.Error) {
		return t.observationSources().Prepare(m, ref, q.Record, now, false)
	})
	if refusal != nil {
		return platform.ObservationResult{}, refusal
	}
	if current.Stamp != plan.Stamp {
		return platform.ObservationResult{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Observation source or permissions changed while reading")
	}
	return result, nil
}
