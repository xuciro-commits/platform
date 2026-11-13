package data

import (
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type PipelineSchedule struct {
	platform.Schedule
	LastBuild *time.Time `json:"lastBuild,omitempty"`
	NextDue   *time.Time `json:"nextDue,omitempty"`
}

func (o *Owner) PipelineSchedules(rows []any, pipeline func(id string) (Pipeline, bool)) ([]PipelineSchedule, *kernel.Error) {
	out := []PipelineSchedule{}
	for _, row := range rows {
		schedule, err := o.ExecutionSchedule(row.(platform.Schedule))
		if err != nil {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
		item := PipelineSchedule{Schedule: schedule, NextDue: schedule.NextDue()}
		if found, ok := pipeline(schedule.Pipeline); ok && found.Last != nil {
			at := found.Last.At
			item.LastBuild = &at
		}
		out = append(out, item)
	}
	return out, nil
}
