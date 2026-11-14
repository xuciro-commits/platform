package host

import (
	"net/http"
	"net/url"
	"platformkernel/kernel"
	runtimekernel "platformserver/kernel"
	"platformserver/kernel/transport"
	"platformserver/platform"
	build "platformserver/services/resources/authoring"
	"platformserver/services/resources/monitoring"
	controlowner "platformserver/services/security/control"
	"time"
)

func (h *Host) routesMonitoring(rt *routes) {
	rt.authorized(transport.Route{Pattern: "GET /v1/audit", Summary: "Immutable accepted and security audit events, newest first, with retained coverage", Answer: platform.AuditPage{}, Query: []transport.Param{{Name: "from", Description: "Inclusive RFC 3339 start, default 30 days before to"}, {Name: "to", Description: "Exclusive RFC 3339 end, default now"}, {Name: "member", Description: "member identity"}, {Name: "app", Description: "app identity"}, {Name: "action", Description: "exact action schema or security event"}, {Name: "q", Description: "search member, app, action and target"}, {Name: "cursor", Description: "opaque cursor from the previous page"}, {Name: "limit", Description: "1-200, default 100"}}}, controlowner.PlatformApp, []string{controlowner.Admin, platform.Auditor}, observed(h, monitoring.ObservationReads.AuditPage))
	rt.authorized(transport.Route{Pattern: "GET /v1/personal-reads", Summary: "Immutable reads of personal data, newest first, with retained coverage", Answer: platform.PersonalReadPage{}, Query: []transport.Param{{Name: "from", Description: "Inclusive RFC 3339 start, default 30 days before to"}, {Name: "to", Description: "Exclusive RFC 3339 end, default now"}, {Name: "member", Description: "member identity"}, {Name: "type", Description: "record type"}, {Name: "cursor", Description: "opaque cursor from the previous page"}, {Name: "limit", Description: "1-200, default 100"}}}, controlowner.PlatformApp, []string{controlowner.Admin, platform.Auditor}, observed(h, monitoring.ObservationReads.PersonalReadPage))
	rt.recordRead(transport.Route{Pattern: "GET /v1/monitoring/views", Summary: "The caller's saved monitoring run filters", Answer: []platform.MonitoringView{}}, transport.Answering(func(_ *http.Request, m platform.Member, t *Tenant) (any, *kernel.Error) {
		return t.readFromApp(m, monitoring.ID, monitoring.ReadViews)
	}))
	rt.authorized(transport.Route{Pattern: "GET /v1/monitoring/log-policies", Summary: "Effective tenant and application log policies", Answer: []controlowner.LogPolicyView{}}, controlowner.PlatformApp, []string{controlowner.Admin}, transport.Answering(func(_ *http.Request, _ platform.Member, t *Tenant) ([]controlowner.LogPolicyView, *kernel.Error) {
		return t.LogPolicies(), nil
	}))
	rt.recordRead(transport.Route{Pattern: "GET /v1/monitoring/runs", Summary: "Current and retained runs visible through their original owners, with unfiltered overview counts and recovery actions", Answer: monitoring.MonitoringRunPage{}, Query: []transport.Param{{Name: "type", Description: "sync, build, automation, function or delivery; empty selects all"}, {Name: "status", Description: "ok, failed, pending, running, warning, paused, cancelled or unknown; empty selects all"}, {Name: "offset", Description: "Nonnegative run offset, default 0"}, {Name: "limit", Description: "1-100, default 50"}}}, transport.Answering(func(r *http.Request, m platform.Member, t *Tenant) (monitoring.MonitoringRunPage, *kernel.Error) {
		filter, refusal := monitoring.RunFilterFrom(r.URL.Query())
		if refusal != nil {
			return monitoring.MonitoringRunPage{}, refusal
		}
		return t.monitoringRuns(m, filter, h.Now())
	}))
	rt.handle(transport.Route{Pattern: "GET /v1/monitoring/logs", Summary: "Operational logs visible to the caller, newest first, with retention coverage", Answer: platform.ObservationPage{}, Query: []transport.Param{{Name: "from", Description: "Inclusive RFC 3339 start, default one hour before to"}, {Name: "to", Description: "Exclusive RFC 3339 end, default now"}, {Name: "level", Description: "debug, info, warn or error"}, {Name: "source", Description: "request, submit, run, worker, data or app"}, {Name: "app", Description: "app identity"}, {Name: "member", Description: "member identity; other members always read their own logs"}, {Name: "requestId", Description: "HTTP request identity"}, {Name: "traceId", Description: "W3C trace identity"}, {Name: "runId", Description: "owner run identity"}, {Name: "resource", Description: "resource identity"}, {Name: "q", Description: "search the redacted message and indexed attributes"}, {Name: "cursor", Description: "opaque cursor from the previous page"}, {Name: "limit", Description: "1-200, default 100"}}}, observed(h, monitoring.ObservationReads.Logs))
	rt.handle(transport.Route{Pattern: "GET /v1/monitoring/traces", Summary: "Request traces visible to the caller, grouped by W3C trace identity", Answer: platform.ObservationPage{}, Query: []transport.Param{{Name: "from", Description: "Inclusive RFC 3339 start"}, {Name: "to", Description: "Exclusive RFC 3339 end"}, {Name: "member", Description: "member identity"}, {Name: "app", Description: "application identity"}, {Name: "q", Description: "search the redacted message"}, {Name: "cursor", Description: "opaque cursor"}, {Name: "limit", Description: "1-200, default 100"}}}, observed(h, monitoring.ObservationReads.Traces))
	rt.handle(transport.Route{Pattern: "GET /v1/monitoring/metrics", Summary: "Fixed operational rollups for the selected time window", Answer: platform.ObservationSummary{}, Query: []transport.Param{{Name: "from", Description: "Inclusive RFC 3339 start"}, {Name: "to", Description: "Exclusive RFC 3339 end"}}}, observed(h, monitoring.ObservationReads.Metrics))
	rt.recordRead(transport.Route{Pattern: "GET /v1/monitoring/alerts", Summary: "Active automations with a notification effect visible to the caller", Answer: []monitoring.MonitoringAlert{}}, transport.Answering(func(_ *http.Request, m platform.Member, t *Tenant) ([]monitoring.MonitoringAlert, *kernel.Error) {
		coverage := monitoring.MonitoringCoverage{Source: "alerts", Basis: "current"}
		processes, refusal := monitoringRecords[build.Process](t, m, build.ProcessType, &coverage, h.Now())
		return build.MonitoringAlerts(processes), refusal
	}))
	rt.public(transport.Route{Pattern: "GET /readyz", Summary: "Whether this process can serve tenant reads from its journals and operational store"}, func(w http.ResponseWriter, r *http.Request) {
		for _, t := range h.currentTenants() {
			if err := t.StorageReady(r.Context()); err != nil {
				WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
				return
			}
		}
		WriteJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})
}

func observed[R any](h *Host, read func(monitoring.ObservationReads, url.Values) (R, *kernel.Error)) func(http.ResponseWriter, *http.Request, platform.Member, *Tenant) {
	return transport.Answering(func(r *http.Request, m platform.Member, t *Tenant) (R, *kernel.Error) {
		return read(observationReads(m, t, h.Now()), r.URL.Query())
	})
}

func observationReads(m platform.Member, t *Tenant, now time.Time) monitoring.ObservationReads {
	return monitoring.ObservationReads{
		Tenant: t.ID, Now: now, Member: m.ID,
		TenantWide: m.Holds(controlowner.PlatformApp, controlowner.Admin) || m.Holds(controlowner.PlatformApp, platform.Auditor),
		LevelRank:  runtimekernel.ObservationLevelRank,
		Query: func(q platform.ObservationQuery) (platform.ObservationPage, error) {
			return t.Log().QueryObservations(q)
		},
		Audit: func(q platform.AuditQuery) (platform.AuditPage, error) { return t.Log().QueryAuditEntries(q) },
		PersonalReads: func(q platform.PersonalReadQuery) (platform.PersonalReadPage, error) {
			return t.Log().QueryPersonalReads(q)
		},
		Summarize: func(member string, from, to time.Time) (platform.ObservationSummary, error) {
			return t.Log().SummarizeObservations(t.ID, member, from, to)
		},
		Remember: func(action, target string) {
			t.AuditLog.Remember(platform.AuditEntry{At: now, Member: m.ID, App: controlowner.PlatformApp, Action: action, Target: target})
		},
		Flush:          func() { t.FlushObservations() },
		Dropped:        func() int64 { return t.ObservationBuffer.Dropped() },
		RequestPercent: func() int { return t.logging().RequestPercent() },
	}
}
