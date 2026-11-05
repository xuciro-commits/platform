package host

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/transport"
	"platformserver/platform"
	"platformserver/services/resources/monitoring"
	controlowner "platformserver/services/security/control"
)

const maxObservationWindow = 30 * 24 * time.Hour

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
	rt.handle(transport.Route{Pattern: "GET /v1/monitoring/logs", Summary: "Operational logs visible to the caller, newest first, with retention coverage", Answer: platform.ObservationPage{}, Query: []transport.Param{
		{Name: "from", Description: "Inclusive RFC 3339 start, default one hour before to"},
		{Name: "to", Description: "Exclusive RFC 3339 end, default now"},
		{Name: "level", Description: "debug, info, warn or error"},
		{Name: "source", Description: "request, submit, run, worker, data or app"},
		{Name: "app", Description: "app identity"},
		{Name: "member", Description: "member identity; other members always read their own logs"},
		{Name: "requestId", Description: "HTTP request identity"},
		{Name: "traceId", Description: "W3C trace identity"},
		{Name: "runId", Description: "owner run identity"},
		{Name: "resource", Description: "resource identity"},
		{Name: "q", Description: "search the redacted message and indexed attributes"},
		{Name: "cursor", Description: "opaque cursor from the previous page"},
		{Name: "limit", Description: "1-200, default 100"},
	}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		now := h.Now()
		query, refusal := observationQuery(r, t.ID, now)
		if refusal != nil {
			Reply(w, nil, refusal)
			return
		}
		tenantWide := m.Holds(controlowner.PlatformApp, controlowner.Admin) || m.Holds(controlowner.PlatformApp, platform.Auditor)
		if !tenantWide {
			query.Member = m.ID
		}
		t.audit.Remember(platform.AuditEntry{
			At:     now,
			Member: m.ID,
			App:    controlowner.PlatformApp,
			Action: "platform.log.read",
			Target: observationFilterSummary(query),
		})
		t.FlushObservations()
		page, err := t.store().QueryObservations(query)
		if err != nil {
			if query.Cursor != "" && strings.Contains(err.Error(), "invalid observation cursor") {
				Reply(w, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT})
				return
			}
			Reply(w, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNSPECIFIED, Message: "Operational logs are unavailable"})
			return
		}
		if !tenantWide {
			page.Coverage.Filtered = true
		}
		page.Coverage.Dropped = t.observationDropped.Load()
		if (query.Source == "" || query.Source == "request") && t.logRequestPercent() < 100 {
			page.Coverage.Sampled = true
		}
		WriteJSON(w, http.StatusOK, page)
	})
}

func observationQuery(r *http.Request, tenant string, now time.Time) (platform.ObservationQuery, *kernel.Error) {
	invalid := func() (platform.ObservationQuery, *kernel.Error) {
		return platform.ObservationQuery{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	values := r.URL.Query()
	to := now.UTC()
	if text := values.Get("to"); text != "" {
		parsed, err := time.Parse(time.RFC3339, text)
		if err != nil {
			return invalid()
		}
		to = parsed.UTC()
	}
	from := to.Add(-time.Hour)
	if text := values.Get("from"); text != "" {
		parsed, err := time.Parse(time.RFC3339, text)
		if err != nil {
			return invalid()
		}
		from = parsed.UTC()
	}
	if !from.Before(to) || to.Sub(from) > maxObservationWindow {
		return invalid()
	}
	limit := 100
	if text := values.Get("limit"); text != "" {
		parsed, err := strconv.Atoi(text)
		if err != nil || parsed < 1 || parsed > 200 {
			return invalid()
		}
		limit = parsed
	}
	level := values.Get("level")
	if level != "" && levelRank(level) < 0 {
		return invalid()
	}
	source := values.Get("source")
	if source != "" && !slices.Contains([]string{"request", "submit", "run", "worker", "data", "app"}, source) {
		return invalid()
	}
	return platform.ObservationQuery{
		Tenant:    tenant,
		From:      from,
		To:        to,
		Level:     level,
		Source:    source,
		App:       values.Get("app"),
		Member:    values.Get("member"),
		RequestID: values.Get("requestId"),
		TraceID:   values.Get("traceId"),
		RunID:     values.Get("runId"),
		Resource:  values.Get("resource"),
		Search:    values.Get("q"),
		Cursor:    values.Get("cursor"),
		Limit:     limit,
	}, nil
}

func observationFilterSummary(query platform.ObservationQuery) string {
	parts := []string{
		"from=" + query.From.Format(time.RFC3339),
		"to=" + query.To.Format(time.RFC3339),
		"limit=" + strconv.Itoa(query.Limit),
	}
	for _, filter := range []struct{ name, value string }{
		{"level", query.Level},
		{"source", query.Source},
		{"app", query.App},
		{"member", query.Member},
		{"requestId", query.RequestID},
		{"traceId", query.TraceID},
		{"runId", query.RunID},
		{"resource", query.Resource},
	} {
		if filter.value != "" {
			parts = append(parts, filter.name+"="+filter.value)
		}
	}
	if query.Search != "" {
		parts = append(parts, "q=set")
	}
	return strings.Join(parts, " ")
}
