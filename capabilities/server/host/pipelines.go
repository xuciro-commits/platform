package host

import (
	"context"
	"time"

	"platformkernel/kernel"
	"platformserver/platform"
	dataowner "platformserver/services/data"
	"platformserver/services/resources/authoring"
)

func (t *Tenant) RunPipelines(now time.Time) {
	if t.businessStopped() || !t.OwnerActive(build.ID) {
		return
	}
	t.integration().RunPipelines(now)
}

func (t *Tenant) pipelineResult(member platform.Member, pipeline dataowner.Pipeline, run dataowner.PipelineRun, phase string, revision *uint32, now time.Time) *kernel.Error {
	return t.integration().PipelineResult(member, pipeline, run, phase, revision, now)
}

func (t *Tenant) pipelineInputs(pipeline dataowner.Pipeline, marking string, compartments []string) (dataowner.PipelineInputs, *kernel.Error) {
	return recordRead(t, context.Background(), func(t *Tenant) (dataowner.PipelineInputs, *kernel.Error) {
		return t.DataOwner().CollectPipelineInputs(t.automation(build.ID, false), pipeline, marking, compartments), nil
	})
}

func (t *Tenant) unguarded(object, marking string) []string {
	value, _ := recordRead(t, context.Background(), func(t *Tenant) ([]string, *kernel.Error) {
		if !dataowner.Guarded(marking) {
			return nil, nil
		}
		t.Projection.Lock()
		entity := t.Projection.Type(object)
		t.Projection.Unlock()
		if entity == nil {
			return nil, nil
		}
		return dataowner.UnguardedFields(entity.Info(), nil), nil
	})
	return value
}

func (t *Tenant) datasetOutput(dataset, key string) (int, error) {
	return readWithError(t, context.Background(), func(t *Tenant) (int, error) {
		return dataowner.DatasetOutputVersion(t.automation(build.ID, false), dataset, key)
	})
}
