package host

import (
	"context"
	"fmt"
	dataowner "platformserver/services/data"

	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
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
	payload := platform.Raw(map[string]any{"run": run})
	key := "ran:" + p.ID + ":" + now.UTC().Format(time.RFC3339Nano)
	if run.Execution != nil {
		key = "pipeline:" + run.Execution.ID + ":" + phase
	}
	_, err := t.submitBackground(member, &pb.Submission{TenantId: t.ID, PrincipalId: member.ID, Authority: build.ID, IdempotencyKey: key, ExpectedRevision: revision,
		Target: &pb.EntityRef{Type: dataowner.PipelineType, Id: p.ID}, Schema: &pb.SchemaRef{Name: dataowner.SchemaPipelineRan, Version: 1}, Payload: payload}, now)
	if phase == "finished" {
		status, detail := "ok", run.Error
		if run.Error != "" || err != nil {
			status = "failed"
			if detail == "" && err != nil {
				detail = err.Error()
			}
		}
		t.observeOwnerRun(now, "worker", "pipeline:"+build.ID+":"+p.ID+":"+now.UTC().Format(time.RFC3339Nano), build.ID,
			"pipeline/"+p.Name, status, "Pipeline "+p.Title, detail)
	}
	return err
}

func (t *Tenant) unguarded(object, marking string) []string {
	value, _ := recordRead(t, context.Background(), func(t *Tenant) ([]string, *kernel.Error) { return t.unguardedRead(object, marking), nil })
	return value
}

func (t *Tenant) unguardedRead(object, marking string) []string {
	if !dataowner.Guarded(marking) {
		return nil
	}
	t.Projection.Lock()
	et := t.Projection.Type(object)
	t.Projection.Unlock()
	if et == nil {
		return nil
	}
	return dataowner.UnguardedFields(et.Info(), nil)
}

func (t *Tenant) pipelineInputs(p dataowner.Pipeline, marking string, compartments []string) (dataowner.PipelineInputs, *kernel.Error) {
	return recordRead(t, context.Background(), func(t *Tenant) (dataowner.PipelineInputs, *kernel.Error) {
		c := t.automation(build.ID, false)
		inputs := map[string]int{}
		data := map[string][]map[string]any{}
		var inputErr error
		for _, id := range append([]string{p.Input}, p.StepDatasets()...) {
			if _, seen := inputs[id]; seen {
				continue
			}
			rows, version, err := dataowner.DatasetRows(c, id, 0)
			if err != nil {
				inputErr = err
				break
			}
			inputs[id], data[id] = version, rows
			if ds, ok := platform.Get[dataowner.Dataset](c, id); ok {
				fixed, err := t.DataOwner().ExecutionDataset(ds)
				if err != nil {
					inputErr = err
					break
				}
				marking = platform.HigherMarking(marking, fixed.Marking)
				compartments = platform.JoinCompartments(compartments, fixed.Compartments)
			}
		}
		return dataowner.PipelineInputs{Inputs: inputs, Data: data, Err: inputErr, Marking: marking, Compartments: compartments}, nil
	})
}

func (t *Tenant) datasetOutput(dataset, key string) (int, error) {
	versions, err := recordRead(t, context.Background(), func(t *Tenant) ([]dataowner.DatasetVersion, *kernel.Error) {
		rows, _, refusal := platform.Find[dataowner.DatasetVersion](t.automation(build.ID, false), platform.Query{Domain: platform.Raw([][]any{{"dataset", "=", dataset}, {"requestKey", "=", key}}), Archived: true, Limit: 1})
		return rows, refusal
	})
	if err != nil {
		return 0, err
	}
	if len(versions) != 1 {
		return 0, fmt.Errorf("pipeline output version is ambiguous")
	}
	return versions[0].Version, nil
}
