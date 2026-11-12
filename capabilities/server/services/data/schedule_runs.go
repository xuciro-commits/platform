package data

import (
	"encoding/json"
	"log"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type ScheduleRuns struct {
	Member    func(string) (platform.Member, bool)
	Exists    func(schedule string) bool
	Project   func(pipeline string) string
	Schedules func() ([]platform.Schedule, *kernel.Error)
	Composite func(platform.Member, string, []platform.CompositeEdit, time.Time) error
}

func (o *Owner) ExecutionSchedules(c platform.Caller) ([]platform.Schedule, *kernel.Error) {
	rows, _, err := platform.Find[platform.Schedule](c, platform.Query{Sort: []string{"id"}, Limit: 1000})
	if err != nil {
		return nil, err
	}
	out := make([]platform.Schedule, 0, len(rows))
	for _, row := range rows {
		fixed, err := o.ExecutionSchedule(row)
		if err != nil {
			continue
		}
		out = append(out, fixed)
	}
	return out, nil
}

func (r ScheduleRuns) Run(pipelines []Pipeline, now time.Time) {
	for _, pipeline := range pipelines {
		if pipeline.Every == "" {
			continue
		}
		member, ok := r.Member(pipeline.Runner)
		if !ok {
			continue
		}
		if pipeline.RunnerScopes != "" {
			_ = json.Unmarshal([]byte(pipeline.RunnerScopes), &member.Scopes)
		}
		id := "pipeline-" + pipeline.ID
		if !r.Exists(id) {
			project := r.Project(pipeline.ID)
			if project == "" {
				log.Printf("schedule migration refused: pipeline %s has no project", pipeline.ID)
				continue
			}
			edits := []platform.CompositeEdit{{Schema: platform.ScheduleType + ".create", Target: &pb.EntityRef{Type: platform.ScheduleType, Id: id}, Payload: platform.Raw(map[string]any{"name": "schedule" + pipeline.Name, "title": pipeline.Title, "pipeline": pipeline.ID, "every": pipeline.Every, "enabled": true, "project": project})}}
			if pipeline.Last != nil {
				edits = append(edits, platform.CompositeEdit{Schema: platform.SchemaScheduleRan, Target: &pb.EntityRef{Type: platform.ScheduleType, Id: id}, Payload: platform.Raw(map[string]any{"at": pipeline.Last.At})})
			}
			if err := r.Composite(member, "schedule-migration:"+pipeline.ID, edits, now); err != nil {
				log.Printf("schedule migration refused: %v", err)
			}
		}
	}
	schedules, refusal := r.Schedules()
	if refusal != nil {
		return
	}
	for _, schedule := range schedules {
		if !schedule.Enabled {
			continue
		}
		if next := schedule.NextDue(); next != nil && now.Before(*next) {
			continue
		}
		member, ok := r.Member(schedule.Runner)
		if !ok {
			continue
		}
		if schedule.RunnerScopes != "" && json.Unmarshal([]byte(schedule.RunnerScopes), &member.Scopes) != nil {
			continue
		}
		stamp := "first"
		if next := schedule.NextDue(); next != nil {
			stamp = next.UTC().Format(time.RFC3339Nano)
		}
		key := "schedule:" + schedule.ID + ":" + stamp
		edits := []platform.CompositeEdit{
			{Schema: PipelineType + ".run", Target: &pb.EntityRef{Type: PipelineType, Id: schedule.Pipeline}, Payload: platform.Raw(map[string]any{})},
			{Schema: platform.SchemaScheduleRan, Target: &pb.EntityRef{Type: platform.ScheduleType, Id: schedule.ID}, Payload: platform.Raw(map[string]any{"at": now})},
		}
		if err := r.Composite(member, key, edits, now); err != nil {
			log.Printf("schedule build refused: %v", err)
		}
	}
}

type PipelinePlan struct {
	Fixed    []Pipeline
	Versions map[string]int
	Changed  map[string]bool
}

func (o *Owner) PlanPipelines(c platform.Caller, query platform.Query) PipelinePlan {
	pipelines, _, _ := platform.Find[Pipeline](c, query)
	plan := PipelinePlan{Versions: map[string]int{}, Changed: map[string]bool{}}
	for _, p := range pipelines {
		p, err := o.ExecutionPipeline(p)
		if err != nil {
			log.Printf("pipeline definition refused: %v", err)
			continue
		}
		plan.Fixed = append(plan.Fixed, p)
		if ds, ok := platform.Get[Dataset](c, p.Input); ok {
			plan.Versions[p.ID] = ds.Version
		}
		if p.Last != nil && p.Last.Execution != nil {
			for _, id := range append([]string{p.Input}, p.StepDatasets()...) {
				if ds, ok := platform.Get[Dataset](c, id); ok && ds.Version != p.Last.Execution.Inputs[id] {
					plan.Changed[p.ID] = true
				}
			}
		}
	}
	return plan
}
