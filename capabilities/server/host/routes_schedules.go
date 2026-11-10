package host

import (
	"net/http"
	dataowner "platformserver/services/data"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/transport"
	"platformserver/platform"
)

type PipelineSchedule struct {
	platform.Schedule
	LastBuild *time.Time `json:"lastBuild,omitempty"`
	NextDue   *time.Time `json:"nextDue,omitempty"`
}

func publishedPipelineQuery() platform.Query {
	filter := platform.Expr{Op: "=", Kind: "boolean", Args: []platform.Expr{platform.ExprReference("text", "state"), platform.ExprLiteralValue("text", "published")}}
	return platform.Query{Filter: &filter, Sort: []string{"id"}}
}

func (h *Host) routesSchedules(rt *routes) {
	rt.recordRead(transport.Route{Pattern: "GET /v1/schedules", Summary: "Independent pipeline schedules visible to the caller, with their next due time and last build", Answer: []PipelineSchedule{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		out, refusal := t.pipelineSchedules(m, h.Now())
		if refusal != nil {
			Reply(w, nil, refusal)
			return
		}
		WriteJSON(w, http.StatusOK, out)
	})
}

func (t *Tenant) pipelineSchedules(m platform.Member, now time.Time) ([]PipelineSchedule, *kernel.Error) {
	page, refusal := t.recordSelectionFrom(t.records, m, platform.ScheduleType, platform.Query{Sort: []string{"id"}}, now)
	if refusal != nil {
		return nil, refusal
	}
	out := []PipelineSchedule{}
	owner := t.dataOwner()
	for _, row := range page.Records {
		schedule, err := owner.ExecutionSchedule(row.(platform.Schedule))
		if err != nil {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
		item := PipelineSchedule{Schedule: schedule, NextDue: schedule.NextDue()}
		if view, refusal := t.recordOf(m, dataowner.PipelineType, schedule.Pipeline, now); refusal == nil {
			pipeline := view.Record.(dataowner.Pipeline)
			if pipeline.Last != nil {
				at := pipeline.Last.At
				item.LastBuild = &at
			}
		}
		out = append(out, item)
	}
	return out, nil
}
