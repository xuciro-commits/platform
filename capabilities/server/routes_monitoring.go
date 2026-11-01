package platformserver

import (
	"net/http"
	"strconv"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/monitoring"
	"platformserver/kernel/transport"
	"platformserver/platform"
)

func (h *Host) routesMonitoring(rt *routes) {
	rt.recordRead(transport.Route{Pattern: "GET /v1/monitoring/views", Summary: "The caller's saved monitoring run filters", Answer: []platform.MonitoringView{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		out, err := t.readFromApp(m, monitoring.ID, monitoring.ReadViews)
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, out)
	})
	rt.recordRead(transport.Route{Pattern: "GET /v1/monitoring/runs", Summary: "Current and retained runs visible through their original owners, with unfiltered overview counts and recovery actions", Answer: MonitoringRunPage{}, Query: []transport.Param{
		{Name: "type", Description: "sync, build, automation, function or delivery; empty selects all"},
		{Name: "status", Description: "ok, failed, pending, running, warning, paused, cancelled or unknown; empty selects all"},
		{Name: "offset", Description: "Nonnegative run offset, default 0"},
		{Name: "limit", Description: "1-100, default 50"},
	}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		filter := MonitoringRunFilter{Type: r.URL.Query().Get("type"), Status: r.URL.Query().Get("status"), Limit: 50}
		for key, dest := range map[string]*int{"offset": &filter.Offset, "limit": &filter.Limit} {
			if r.URL.Query().Has(key) {
				value, err := strconv.Atoi(r.URL.Query().Get(key))
				if err != nil {
					Reply(w, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT})
					return
				}
				*dest = value
			}
		}
		if !filter.valid() {
			Reply(w, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT})
			return
		}
		page, refusal := t.monitoringRuns(m, filter, h.Now())
		if refusal != nil {
			Reply(w, nil, refusal)
			return
		}
		WriteJSON(w, http.StatusOK, page)
	})
}
