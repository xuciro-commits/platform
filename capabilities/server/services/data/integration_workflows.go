package data

import (
	"log"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func (g Integration) PullSources(now time.Time) {
	sources, refusal := g.Sources()
	if refusal != nil {
		log.Printf("source preparation unavailable: %v", refusal)
		return
	}
	for _, source := range sources {
		if source.Due(now) {
			g.PullSource(source, now)
		}
	}
}

func (g Integration) ApplyRows(member platform.Member, plan IntegrationPreparation, execution *IntegrationExecution, now time.Time, pull *SourcePull) bool {
	return RowApplication{Quarantined: g.Quarantined, Pending: g.Pending, Refused: g.Refused, Target: g.Target,
		Submit: func(member platform.Member, app, object string, binding *platform.DataSourceBinding, row SourceRow, key string, now time.Time) *kernel.Error {
			return g.SubmitIntegrationRow(member, app, object, binding, row, key, now)
		}}.Apply(member, plan, execution, now, pull)
}

func (g Integration) SubmitIntegrationRow(member platform.Member, app, object string, binding *platform.DataSourceBinding, row SourceRow, key string, now time.Time) *kernel.Error {
	submission := &pb.Submission{TenantId: g.Tenant, PrincipalId: member.ID, Authority: app, IdempotencyKey: key,
		Target: &pb.EntityRef{Type: object, Id: row.ID}, Schema: &pb.SchemaRef{Name: object + row.Verb, Version: 1}, Payload: row.Payload}
	return g.SubmitSource(member, submission, binding, row, now)
}

func (g Integration) LoadDataset(member platform.Member, dataset, producer string, marking platform.MarkingSet, rows []map[string]any, key string, now time.Time, transaction string) *kernel.Error {
	_, err := g.Submit(member, DatasetLoadSubmission(g.Tenant, g.Authority, member, dataset, producer, marking, rows, key, transaction), now)
	return err
}

func (g Integration) SourceResult(member platform.Member, source Source, pull SourcePull, phase string, revision *uint32, now time.Time) *kernel.Error {
	_, err := g.Submit(member, SourcePullSubmission(g.Tenant, g.Authority, member, source, pull, phase, revision, now), now)
	if phase == "finished" {
		status, detail := RunOutcome(pull.Error, err)
		g.ObserveRun(now, "data", "source:"+g.Authority+":"+source.ID+":"+now.UTC().Format(time.RFC3339Nano), g.Authority,
			"source/"+source.Name, status, "Source "+source.Title, detail)
	}
	return err
}

func (g Integration) PipelineResult(member platform.Member, pipeline Pipeline, run PipelineRun, phase string, revision *uint32, now time.Time) *kernel.Error {
	_, err := g.SubmitBackground(member, PipelineRunSubmission(g.Tenant, g.Authority, member, pipeline, run, phase, revision, now), now)
	if phase == "finished" {
		status, detail := RunOutcome(run.Error, err)
		g.ObserveRun(now, "worker", "pipeline:"+g.Authority+":"+pipeline.ID+":"+now.UTC().Format(time.RFC3339Nano), g.Authority,
			"pipeline/"+pipeline.Name, status, "Pipeline "+pipeline.Title, detail)
	}
	return err
}

func (g Integration) RunPipelines(now time.Time) {
	prepared, refusal := g.PipelinePlan()
	if refusal != nil {
		return
	}
	g.RunSchedules(prepared.Fixed, now)
	for i, pipeline := range prepared.Fixed {
		current, refusal := g.CurrentPipeline(pipeline.ID)
		if refusal == nil {
			prepared.Fixed[i].Record = current.Record
			prepared.Fixed[i].Requested = current.Requested
			prepared.Fixed[i].Runner = current.Runner
			prepared.Fixed[i].RunnerScopes = current.RunnerScopes
		}
	}
	for _, pipeline := range prepared.Fixed {
		if pipeline.Due(now, prepared.Versions[pipeline.ID]) || pipeline.State == "published" && prepared.Changed[pipeline.ID] {
			g.RunPipeline(pipeline, now)
		}
	}
}

func (g Integration) RunSchedules(pipelines []Pipeline, now time.Time) {
	ScheduleRuns{Member: g.Member, Exists: g.ScheduleExists, Project: g.PipelineProject, Schedules: g.ExecutionSchedules, Composite: g.Composite}.Run(pipelines, now)
}
