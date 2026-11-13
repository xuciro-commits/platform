package monitoring

import (
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

const maxObservationWindow = 30 * 24 * time.Hour
const maxAuditWindow = 400 * 24 * time.Hour

func invalidArgument() *kernel.Error {
	return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
}

func AuditQuery(values url.Values, tenant string, now time.Time) (platform.AuditQuery, *kernel.Error) {
	query, refusal := auditWindow(values, now)
	if refusal != nil {
		return platform.AuditQuery{}, refusal
	}
	query.Tenant = tenant
	query.Member = values.Get("member")
	query.App = values.Get("app")
	query.Action = values.Get("action")
	query.Search = values.Get("q")
	query.Cursor = values.Get("cursor")
	limit, refusal := pageLimit(values)
	if refusal != nil {
		return platform.AuditQuery{}, refusal
	}
	query.Limit = limit
	return query, nil
}

func PersonalReadQuery(values url.Values, tenant string, now time.Time) (platform.PersonalReadQuery, *kernel.Error) {
	window, refusal := auditWindow(values, now)
	if refusal != nil {
		return platform.PersonalReadQuery{}, refusal
	}
	limit, refusal := pageLimit(values)
	if refusal != nil {
		return platform.PersonalReadQuery{}, refusal
	}
	return platform.PersonalReadQuery{
		Tenant: tenant, From: window.From, To: window.To,
		Member: values.Get("member"), Type: values.Get("type"),
		Cursor: values.Get("cursor"), Limit: limit,
	}, nil
}

func auditWindow(values url.Values, now time.Time) (platform.AuditQuery, *kernel.Error) {
	to := now.UTC()
	if text := values.Get("to"); text != "" {
		parsed, err := time.Parse(time.RFC3339, text)
		if err != nil {
			return platform.AuditQuery{}, invalidArgument()
		}
		to = parsed.UTC()
	}
	from := to.AddDate(0, 0, -30)
	if text := values.Get("from"); text != "" {
		parsed, err := time.Parse(time.RFC3339, text)
		if err != nil {
			return platform.AuditQuery{}, invalidArgument()
		}
		from = parsed.UTC()
	}
	if !from.Before(to) || to.Sub(from) > maxAuditWindow {
		return platform.AuditQuery{}, invalidArgument()
	}
	return platform.AuditQuery{From: from, To: to}, nil
}

func pageLimit(values url.Values) (int, *kernel.Error) {
	limit := 100
	if text := values.Get("limit"); text != "" {
		parsed, err := strconv.Atoi(text)
		if err != nil || parsed < 1 || parsed > 200 {
			return 0, invalidArgument()
		}
		limit = parsed
	}
	return limit, nil
}

type filter struct{ name, value string }

func filterSummary(from, to time.Time, limit int, search string, filters ...filter) string {
	parts := []string{"from=" + from.Format(time.RFC3339), "to=" + to.Format(time.RFC3339), "limit=" + strconv.Itoa(limit)}
	for _, f := range filters {
		if f.value != "" {
			parts = append(parts, f.name+"="+f.value)
		}
	}
	if search != "" {
		parts = append(parts, "q=set")
	}
	return strings.Join(parts, " ")
}

func AuditFilterSummary(query platform.AuditQuery) string {
	return filterSummary(query.From, query.To, query.Limit, query.Search,
		filter{"member", query.Member}, filter{"app", query.App}, filter{"action", query.Action}, filter{"cursor", query.Cursor})
}

func PersonalReadFilterSummary(query platform.PersonalReadQuery) string {
	return filterSummary(query.From, query.To, query.Limit, "",
		filter{"member", query.Member}, filter{"type", query.Type}, filter{"cursor", query.Cursor})
}

func ObservationFilterSummary(query platform.ObservationQuery) string {
	return filterSummary(query.From, query.To, query.Limit, query.Search,
		filter{"level", query.Level}, filter{"source", query.Source}, filter{"app", query.App}, filter{"member", query.Member},
		filter{"requestId", query.RequestID}, filter{"traceId", query.TraceID}, filter{"runId", query.RunID}, filter{"resource", query.Resource})
}

func ObservationQuery(values url.Values, tenant string, now time.Time, levelRank func(string) int) (platform.ObservationQuery, *kernel.Error) {
	to := now.UTC()
	if text := values.Get("to"); text != "" {
		parsed, err := time.Parse(time.RFC3339, text)
		if err != nil {
			return platform.ObservationQuery{}, invalidArgument()
		}
		to = parsed.UTC()
	}
	from := to.Add(-time.Hour)
	if text := values.Get("from"); text != "" {
		parsed, err := time.Parse(time.RFC3339, text)
		if err != nil {
			return platform.ObservationQuery{}, invalidArgument()
		}
		from = parsed.UTC()
	}
	if !from.Before(to) || to.Sub(from) > maxObservationWindow {
		return platform.ObservationQuery{}, invalidArgument()
	}
	limit, refusal := pageLimit(values)
	if refusal != nil {
		return platform.ObservationQuery{}, refusal
	}
	level := values.Get("level")
	if level != "" && levelRank(level) < 0 {
		return platform.ObservationQuery{}, invalidArgument()
	}
	source := values.Get("source")
	if source != "" && !slices.Contains([]string{"request", "submit", "run", "worker", "data", "app"}, source) {
		return platform.ObservationQuery{}, invalidArgument()
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

func RunFilterFrom(values url.Values) (MonitoringRunFilter, *kernel.Error) {
	filter := MonitoringRunFilter{Type: values.Get("type"), Status: values.Get("status"), Limit: 50}
	for key, dest := range map[string]*int{"offset": &filter.Offset, "limit": &filter.Limit} {
		if values.Has(key) {
			value, err := strconv.Atoi(values.Get(key))
			if err != nil {
				return filter, invalidArgument()
			}
			*dest = value
		}
	}
	if !filter.Valid() {
		return filter, invalidArgument()
	}
	return filter, nil
}

func QueryFailure(err error, cursor, cursorMarker, unavailable string) *kernel.Error {
	if cursor != "" && strings.Contains(err.Error(), cursorMarker) {
		return invalidArgument()
	}
	return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNSPECIFIED, Message: unavailable}
}

type ObservationReads struct {
	Tenant         string
	Now            time.Time
	TenantWide     bool
	Member         string
	LevelRank      func(string) int
	Query          func(platform.ObservationQuery) (platform.ObservationPage, error)
	Summarize      func(member string, from, to time.Time) (platform.ObservationSummary, error)
	Remember       func(action, target string)
	Flush          func()
	Dropped        func() int64
	RequestPercent func() int
}

func (o ObservationReads) Logs(values url.Values) (platform.ObservationPage, *kernel.Error) {
	query, refusal := ObservationQuery(values, o.Tenant, o.Now, o.LevelRank)
	if refusal != nil {
		return platform.ObservationPage{}, refusal
	}
	if !o.TenantWide {
		query.Member = o.Member
	}
	o.Remember("platform.log.read", ObservationFilterSummary(query))
	o.Flush()
	page, err := o.Query(query)
	if err != nil {
		return page, QueryFailure(err, query.Cursor, "invalid observation cursor", "Operational logs are unavailable")
	}
	if !o.TenantWide {
		page.Coverage.Filtered = true
	}
	page.Coverage.Dropped = o.Dropped()
	if (query.Source == "" || query.Source == "request") && o.RequestPercent() < 100 {
		page.Coverage.Sampled = true
	}
	return page, nil
}

func (o ObservationReads) Traces(values url.Values) (platform.ObservationPage, *kernel.Error) {
	query, refusal := ObservationQuery(values, o.Tenant, o.Now, o.LevelRank)
	if refusal != nil {
		return platform.ObservationPage{}, refusal
	}
	query.Source = "request"
	query.Level = ""
	if !o.TenantWide {
		query.Member = o.Member
	}
	o.Remember("platform.trace.read", ObservationFilterSummary(query))
	page, err := o.Query(query)
	if err != nil {
		return page, QueryFailure(err, "", "", "Traces are unavailable")
	}
	page.Coverage.Dropped = o.Dropped()
	if o.RequestPercent() < 100 {
		page.Coverage.Sampled = true
	}
	return page, nil
}

func (o ObservationReads) Metrics(values url.Values) (platform.ObservationSummary, *kernel.Error) {
	query, refusal := ObservationQuery(values, o.Tenant, o.Now, o.LevelRank)
	if refusal != nil {
		return platform.ObservationSummary{}, refusal
	}
	if !o.TenantWide {
		query.Member = o.Member
	}
	summary, err := o.Summarize(query.Member, query.From, query.To)
	if err != nil {
		return summary, QueryFailure(err, "", "", "Metrics are unavailable")
	}
	summary.Dropped = o.Dropped()
	o.Remember("platform.metrics.read", ObservationFilterSummary(query))
	return summary, nil
}
