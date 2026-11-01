package platformserver

import (
	"net/http"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/transport"
	"platformserver/platform"
	"platformserver/studio/build"
)

type PipelineSchedule struct {
	Pipeline  string     `json:"pipeline"`
	Name      string     `json:"name"`
	Title     string     `json:"title"`
	Every     string     `json:"every"`
	LastBuild *time.Time `json:"lastBuild,omitempty"`
	NextDue   *time.Time `json:"nextDue,omitempty"`
	State     string     `json:"state"`
}

func publishedPipelineQuery() platform.Query {
	filter := platform.Expr{Op: "=", Kind: "boolean", Args: []platform.Expr{platform.ExprReference("text", "state"), platform.ExprLiteralValue("text", "published")}}
	return platform.Query{Filter: &filter, Sort: []string{"id"}}
}

func (h *Host) routesSchedules(rt *routes) {
	rt.recordRead(transport.Route{Pattern: "GET /v1/schedules", Summary: "Published pipeline schedules visible to the caller, using their accepted definitions and retained last build", Answer: []PipelineSchedule{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		out, refusal := t.pipelineSchedules(m, h.Now())
		if refusal != nil {
			Reply(w, nil, refusal)
			return
		}
		WriteJSON(w, http.StatusOK, out)
	})
}

func (t *Tenant) pipelineSchedules(m platform.Member, now time.Time) ([]PipelineSchedule, *kernel.Error) {
	page, refusal := t.recordSelectionFrom(t.records, m, build.PipelineType, publishedPipelineQuery(), now)
	if refusal != nil {
		return nil, refusal
	}
	out := []PipelineSchedule{}
	owner := t.app(build.ID).(build.IntegrationDefinitions)
	for _, row := range page.Records {
		p, err := owner.ExecutionPipeline(row.(build.Pipeline))
		if err != nil {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
		if p.Every == "" {
			continue
		}
		schedule := PipelineSchedule{Pipeline: p.ID, Name: p.Name, Title: p.Title, Every: p.Every, NextDue: p.NextDue(), State: p.State}
		if p.Last != nil {
			at := p.Last.At
			schedule.LastBuild = &at
		}
		out = append(out, schedule)
	}
	return out, nil
}
