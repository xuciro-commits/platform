package host

import (
	"context"
	dataowner "platformserver/services/data"

	"time"

	"platformkernel/kernel"

	"platformserver/platform"
	"platformserver/services/resources/authoring"
)

func (t *Tenant) RunPipelines(now time.Time) {
	if t.businessStopped() || !t.OwnerActive(build.ID) {
		return
	}
	prepared, refusal := recordRead(t, context.Background(), func(t *Tenant) (dataowner.PipelinePlan, *kernel.Error) {
		query := publishedPipelineQuery()
		query.Limit = 200
		return t.DataOwner().PlanPipelines(t.automation(build.ID, false), query), nil
	})
	if refusal != nil {
		return
	}
	fixed, versions, changed := prepared.Fixed, prepared.Versions, prepared.Changed
	t.runSchedules(fixed, now)
	for i, p := range fixed {
		if current, refusal := coordinatedRecordRead(t, context.Background(), func(t *Tenant) (dataowner.Pipeline, *kernel.Error) {
			row, _ := platform.Get[dataowner.Pipeline](t.automation(build.ID, false), p.ID)
			return row, nil
		}); refusal == nil {
			fixed[i].Record = current.Record
			fixed[i].Requested = current.Requested
			fixed[i].Runner = current.Runner
			fixed[i].RunnerScopes = current.RunnerScopes
		}
	}
	for _, p := range fixed {
		if p.Due(now, versions[p.ID]) || p.State == "published" && changed[p.ID] {
			t.integration().RunPipeline(p, now)
		}
	}
}

func (t *Tenant) pipelineResult(member platform.Member, p dataowner.Pipeline, run dataowner.PipelineRun, phase string, revision *uint32, now time.Time) *kernel.Error {
	_, err := t.submitBackground(member, dataowner.PipelineRunSubmission(t.ID, build.ID, member, p, run, phase, revision, now), now)
	if phase == "finished" {
		status, detail := dataowner.RunOutcome(run.Error, err)
		t.observeOwnerRun(now, "worker", "pipeline:"+build.ID+":"+p.ID+":"+now.UTC().Format(time.RFC3339Nano), build.ID,
			"pipeline/"+p.Name, status, "Pipeline "+p.Title, detail)
	}
	return err
}

func (t *Tenant) unguarded(object, marking string) []string {
	value, _ := recordRead(t, context.Background(), func(t *Tenant) ([]string, *kernel.Error) {
		if !dataowner.Guarded(marking) {
			return nil, nil
		}
		t.Projection.Lock()
		et := t.Projection.Type(object)
		t.Projection.Unlock()
		if et == nil {
			return nil, nil
		}
		return dataowner.UnguardedFields(et.Info(), nil), nil
	})
	return value
}

func (t *Tenant) pipelineInputs(p dataowner.Pipeline, marking string, compartments []string) (dataowner.PipelineInputs, *kernel.Error) {
	return recordRead(t, context.Background(), func(t *Tenant) (dataowner.PipelineInputs, *kernel.Error) {
		return t.DataOwner().CollectPipelineInputs(t.automation(build.ID, false), p, marking, compartments), nil
	})
}

func (t *Tenant) datasetOutput(dataset, key string) (int, error) {
	return readWithError(t, context.Background(), func(t *Tenant) (int, error) {
		return dataowner.DatasetOutputVersion(t.automation(build.ID, false), dataset, key)
	})
}
