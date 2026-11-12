package host

import (
	"context"
	"platformkernel/kernel"
	"platformserver/platform"
	dataowner "platformserver/services/data"
	"platformserver/services/resources/authoring"
	"time"
)

func (t *Tenant) runSchedules(pipelines []dataowner.Pipeline, now time.Time) {
	dataowner.ScheduleRuns{Member: t.Member,
		Exists: func(id string) bool {
			exists, _ := coordinatedRecordRead(t, context.Background(), func(t *Tenant) (bool, *kernel.Error) {
				_, ok := platform.Get[platform.Schedule](t.automation(build.ID, false), id)
				return ok, nil
			})
			return exists
		},
		Project: func(pipeline string) string {
			project, _ := recordRead(t, context.Background(), func(t *Tenant) (string, *kernel.Error) {
				return t.resourceProject(dataowner.PipelineType, pipeline), nil
			})
			return project
		},
		Schedules: func() ([]platform.Schedule, *kernel.Error) {
			return coordinatedRecordRead(t, context.Background(), func(t *Tenant) ([]platform.Schedule, *kernel.Error) {
				return t.DataOwner().ExecutionSchedules(t.automation(build.ID, false))
			})
		},
		Composite: func(member platform.Member, key string, edits []platform.CompositeEdit, now time.Time) error {
			if _, refusal := t.Composite(member, key, edits, now); refusal != nil {
				return refusal
			}
			return nil
		}}.Run(pipelines, now)
}
