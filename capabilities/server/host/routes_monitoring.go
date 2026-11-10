package host

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/transport"
	"platformserver/platform"
	build "platformserver/services/resources/authoring"
	"platformserver/services/resources/monitoring"
	controlowner "platformserver/services/security/control"
)

const maxObservationWindow = 30 * 24 * time.Hour
const maxAuditWindow = 400 * 24 * time.Hour

func (h *Host) routesMonitoring(rt *routes) {
	rt.authorized(transport.Route{Pattern: "GET /v1/audit", Summary: "Immutable accepted and security audit events, newest first, with retained coverage", Answer: platform.AuditPage{}, Query: []transport.Param{
		{Name: "from", Description: "Inclusive RFC 3339 start, default 30 days before to"},
		{Name: "to", Description: "Exclusive RFC 3339 end, default now"},
		{Name: "member", Description: "member identity"},
		{Name: "app", Description: "app identity"},
		{Name: "action", Description: "exact action schema or security event"},
		{Name: "q", Description: "search member, app, action and target"},
		{Name: "cursor", Description: "opaque cursor from the previous page"},
		{Name: "limit", Description: "1-200, default 100"},
	}}, controlowner.PlatformApp, []string{controlowner.Admin, platform.Auditor}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		now := h.Now()
		query, refusal := auditQuery(r, t.ID, now)
		if refusal != nil {
			Reply(w, nil, refusal)
			return
		}
		page, err := t.store().QueryAuditEntries(query)
		if err != nil {
			if query.Cursor != "" && strings.Contains(err.Error(), "invalid audit cursor") {
				Reply(w, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT})
				return
			}
			Reply(w, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNSPECIFIED, Message: "Audit records are unavailable"})
			return
		}
		t.audit.Remember(platform.AuditEntry{
			At: now, Member: m.ID, App: controlowner.PlatformApp,
			Action: "platform.audit.read", Target: auditFilterSummary(query),
		})
		WriteJSON(w, http.StatusOK, page)
	})
	rt.authorized(transport.Route{Pattern: "GET /v1/personal-reads", Summary: "Immutable reads of personal data, newest first, with retained coverage", Answer: platform.PersonalReadPage{}, Query: []transport.Param{
		{Name: "from", Description: "Inclusive RFC 3339 start, default 30 days before to"},
		{Name: "to", Description: "Exclusive RFC 3339 end, default now"},
		{Name: "member", Description: "member identity"},
		{Name: "type", Description: "record type"},
		{Name: "cursor", Description: "opaque cursor from the previous page"},
		{Name: "limit", Description: "1-200, default 100"},
	}}, controlowner.PlatformApp, []string{controlowner.Admin, platform.Auditor}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		now := h.Now()
		query, refusal := personalReadQuery(r, t.ID, now)
		if refusal != nil {
			Reply(w, nil, refusal)
			return
		}
		page, err := t.store().QueryPersonalReads(query)
		if err != nil {
			if query.Cursor != "" && strings.Contains(err.Error(), "invalid audit cursor") {
				Reply(w, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT})
				return
			}
			Reply(w, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNSPECIFIED, Message: "Personal-read audit is unavailable"})
			return
		}
		t.audit.Remember(platform.AuditEntry{
			At: now, Member: m.ID, App: controlowner.PlatformApp,
			Action: "platform.personal-reads.read", Target: personalReadFilterSummary(query),
		})
		WriteJSON(w, http.StatusOK, page)
	})
	rt.recordRead(transport.Route{Pattern: "GET /v1/monitoring/views", Summary: "The caller's saved monitoring run filters", Answer: []platform.MonitoringView{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		out, err := t.readFromApp(m, monitoring.ID, monitoring.ReadViews)
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, out)
	})
	rt.authorized(transport.Route{Pattern: "GET /v1/monitoring/log-policies", Summary: "Effective tenant and application log policies", Answer: []LogPolicyView{}}, controlowner.PlatformApp, []string{controlowner.Admin}, func(w http.ResponseWriter, _ *http.Request, _ platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, t.LogPolicies())
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
	rt.handle(transport.Route{Pattern: "GET /v1/monitoring/traces", Summary: "Request traces visible to the caller, grouped by W3C trace identity", Answer: platform.ObservationPage{}, Query: []transport.Param{
		{Name: "from", Description: "Inclusive RFC 3339 start"}, {Name: "to", Description: "Exclusive RFC 3339 end"},
		{Name: "member", Description: "member identity"}, {Name: "app", Description: "application identity"},
		{Name: "q", Description: "search the redacted message"}, {Name: "cursor", Description: "opaque cursor"},
		{Name: "limit", Description: "1-200, default 100"},
	}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		now := h.Now()
		query, refusal := observationQuery(r, t.ID, now)
		if refusal != nil {
			Reply(w, nil, refusal)
			return
		}
		query.Source = "request"
		query.Level = ""
		if !m.Holds(controlowner.PlatformApp, controlowner.Admin) && !m.Holds(controlowner.PlatformApp, platform.Auditor) {
			query.Member = m.ID
		}
		t.audit.Remember(platform.AuditEntry{At: now, Member: m.ID, App: controlowner.PlatformApp, Action: "platform.trace.read", Target: observationFilterSummary(query)})
		page, err := t.store().QueryObservations(query)
		if err != nil {
			Reply(w, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNSPECIFIED, Message: "Traces are unavailable"})
			return
		}
		page.Coverage.Dropped = t.observationDropped.Load()
		if t.logRequestPercent() < 100 {
			page.Coverage.Sampled = true
		}
		WriteJSON(w, http.StatusOK, page)
	})
	rt.handle(transport.Route{Pattern: "GET /v1/monitoring/metrics", Summary: "Fixed operational rollups for the selected time window", Answer: platform.ObservationSummary{}, Query: []transport.Param{
		{Name: "from", Description: "Inclusive RFC 3339 start"}, {Name: "to", Description: "Exclusive RFC 3339 end"},
	}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		now := h.Now()
		query, refusal := observationQuery(r, t.ID, now)
		if refusal != nil {
			Reply(w, nil, refusal)
			return
		}
		if !m.Holds(controlowner.PlatformApp, controlowner.Admin) && !m.Holds(controlowner.PlatformApp, platform.Auditor) {
			query.Member = m.ID
		}
		summary, err := t.store().SummarizeObservations(t.ID, query.Member, query.From, query.To)
		if err != nil {
			Reply(w, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNSPECIFIED, Message: "Metrics are unavailable"})
			return
		}
		summary.Dropped = t.observationDropped.Load()
		t.audit.Remember(platform.AuditEntry{At: now, Member: m.ID, App: controlowner.PlatformApp, Action: "platform.metrics.read", Target: observationFilterSummary(query)})
		WriteJSON(w, http.StatusOK, summary)
	})
	rt.recordRead(transport.Route{Pattern: "GET /v1/monitoring/alerts", Summary: "Active automations with a notification effect visible to the caller", Answer: []MonitoringAlert{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		coverage := MonitoringCoverage{Source: "alerts", Basis: "current"}
		processes, refusal := monitoringRecords[build.Process](t, m, build.ProcessType, &coverage, h.Now())
		if refusal != nil {
			Reply(w, nil, refusal)
			return
		}
		out := []MonitoringAlert{}
		for _, process := range processes {
			var installed build.Process
			if process.Archived || process.Paused || json.Unmarshal([]byte(process.Published), &installed) != nil || installed.Kind != "automation" {
				continue
			}
			for _, step := range installed.Steps {
				if step.Notify != nil {
					out = append(out, MonitoringAlert{ID: process.ID, Title: installed.Title, Object: installed.Object, Repeat: installed.Repeat, Message: step.Notify.Body, Role: step.Notify.Role})
					break
				}
			}
		}
		WriteJSON(w, http.StatusOK, out)
	})
}

func auditQuery(r *http.Request, tenant string, now time.Time) (platform.AuditQuery, *kernel.Error) {
	query, refusal := auditWindow(r, now)
	if refusal != nil {
		return platform.AuditQuery{}, refusal
	}
	query.Tenant = tenant
	query.Member = r.URL.Query().Get("member")
	query.App = r.URL.Query().Get("app")
	query.Action = r.URL.Query().Get("action")
	query.Search = r.URL.Query().Get("q")
	query.Cursor = r.URL.Query().Get("cursor")
	limit, refusal := auditLimitFrom(r)
	if refusal != nil {
		return platform.AuditQuery{}, refusal
	}
	query.Limit = limit
	return query, nil
}

func personalReadQuery(r *http.Request, tenant string, now time.Time) (platform.PersonalReadQuery, *kernel.Error) {
	window, refusal := auditWindow(r, now)
	if refusal != nil {
		return platform.PersonalReadQuery{}, refusal
	}
	limit, refusal := auditLimitFrom(r)
	if refusal != nil {
		return platform.PersonalReadQuery{}, refusal
	}
	query := platform.PersonalReadQuery{
		Tenant: tenant, From: window.From, To: window.To,
		Member: r.URL.Query().Get("member"), Type: r.URL.Query().Get("type"),
		Cursor: r.URL.Query().Get("cursor"), Limit: limit,
	}
	return query, nil
}

func auditWindow(r *http.Request, now time.Time) (platform.AuditQuery, *kernel.Error) {
	values := r.URL.Query()
	to := now.UTC()
	if text := values.Get("to"); text != "" {
		parsed, err := time.Parse(time.RFC3339, text)
		if err != nil {
			return platform.AuditQuery{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
		}
		to = parsed.UTC()
	}
	from := to.AddDate(0, 0, -30)
	if text := values.Get("from"); text != "" {
		parsed, err := time.Parse(time.RFC3339, text)
		if err != nil {
			return platform.AuditQuery{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
		}
		from = parsed.UTC()
	}
	if !from.Before(to) || to.Sub(from) > maxAuditWindow {
		return platform.AuditQuery{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	return platform.AuditQuery{From: from, To: to}, nil
}

func auditLimitFrom(r *http.Request) (int, *kernel.Error) {
	limit := 100
	if text := r.URL.Query().Get("limit"); text != "" {
		parsed, err := strconv.Atoi(text)
		if err != nil || parsed < 1 || parsed > 200 {
			return 0, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
		}
		limit = parsed
	}
	return limit, nil
}

func auditFilterSummary(query platform.AuditQuery) string {
	parts := []string{"from=" + query.From.Format(time.RFC3339), "to=" + query.To.Format(time.RFC3339), "limit=" + strconv.Itoa(query.Limit)}
	for _, filter := range []struct{ name, value string }{
		{"member", query.Member}, {"app", query.App}, {"action", query.Action}, {"cursor", query.Cursor},
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

func personalReadFilterSummary(query platform.PersonalReadQuery) string {
	parts := []string{"from=" + query.From.Format(time.RFC3339), "to=" + query.To.Format(time.RFC3339), "limit=" + strconv.Itoa(query.Limit)}
	for _, filter := range []struct{ name, value string }{{"member", query.Member}, {"type", query.Type}, {"cursor", query.Cursor}} {
		if filter.value != "" {
			parts = append(parts, filter.name+"="+filter.value)
		}
	}
	return strings.Join(parts, " ")
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
