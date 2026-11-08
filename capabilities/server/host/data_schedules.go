package host

import (
	"context"
	"encoding/json"
	"log"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/services/resources/authoring"
	"time"
)

func (t *Tenant) runSchedules(pipelines []build.Pipeline, now time.Time) {
	for _, pipeline := range pipelines {
		if pipeline.Every == "" {
			continue
		}
		member, ok := t.Member(pipeline.Runner)
		if !ok {
			continue
		}
		if pipeline.RunnerScopes != "" {
			_ = json.Unmarshal([]byte(pipeline.RunnerScopes), &member.Scopes)
		}
		id := "pipeline-" + pipeline.ID
		exists, _ := coordinatedRecordRead(t, context.Background(), func(t *Tenant) (bool, *kernel.Error) {
			_, ok := platform.Get[platform.Schedule](t.automation(build.ID, false), id)
			return ok, nil
		})
		if !exists {
			project, _ := recordRead(t, context.Background(), func(t *Tenant) (string, *kernel.Error) {
				return t.resourceProject(build.PipelineType, pipeline.ID), nil
			})
			if project == "" {
				log.Printf("schedule migration refused: pipeline %s has no project", pipeline.ID)
				continue
			}
			edits := []platform.CompositeEdit{{Schema: platform.ScheduleType + ".create", Target: &pb.EntityRef{Type: platform.ScheduleType, Id: id}, Payload: platform.Raw(map[string]any{"name": "schedule" + pipeline.Name, "title": pipeline.Title, "pipeline": pipeline.ID, "every": pipeline.Every, "enabled": true, "project": project})}}
			if pipeline.Last != nil {
				edits = append(edits, platform.CompositeEdit{Schema: platform.SchemaScheduleRan, Target: &pb.EntityRef{Type: platform.ScheduleType, Id: id}, Payload: platform.Raw(map[string]any{"at": pipeline.Last.At})})
			}
			if _, err := t.Composite(member, "schedule-migration:"+pipeline.ID, edits, now); err != nil {
				log.Printf("schedule migration refused: %v", err)
			}
		}
	}
	schedules, refusal := coordinatedRecordRead(t, context.Background(), func(t *Tenant) ([]platform.Schedule, *kernel.Error) {
		rows, _, err := platform.Find[platform.Schedule](t.automation(build.ID, false), platform.Query{Sort: []string{"id"}, Limit: 1000})
		if err != nil {
			return nil, err
		}
		owner := t.dataOwner()
		out := make([]platform.Schedule, 0, len(rows))
		for _, row := range rows {
			fixed, err := owner.ExecutionSchedule(row)
			if err != nil {
				continue
			}
			out = append(out, fixed)
		}
		return out, nil
	})
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
		member, ok := t.Member(schedule.Runner)
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
			{Schema: build.PipelineType + ".run", Target: &pb.EntityRef{Type: build.PipelineType, Id: schedule.Pipeline}, Payload: platform.Raw(map[string]any{})},
			{Schema: platform.SchemaScheduleRan, Target: &pb.EntityRef{Type: platform.ScheduleType, Id: schedule.ID}, Payload: platform.Raw(map[string]any{"at": now})},
		}
		if _, err := t.Composite(member, key, edits, now); err != nil {
			log.Printf("schedule build refused: %v", err)
		}
	}
}
