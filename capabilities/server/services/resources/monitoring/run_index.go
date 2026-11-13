package monitoring

import (
	"encoding/json"
	"fmt"
	"slices"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type RunIndex struct {
	Tenant     string
	Scope      string
	Query      func(platform.RunIndexQuery) (platform.RunIndexPage, error)
	Replace    func(entries []platform.RunIndexEntry) error
	Refresh    func(rebuild func() ([]MonitoringCoverage, bool)) []MonitoringCoverage
	FromOwners func(filter MonitoringRunFilter) (MonitoringRunPage, *kernel.Error)
	Observe    func(run MonitoringRun)
}

func (x RunIndex) Runs(filter MonitoringRunFilter) (MonitoringRunPage, *kernel.Error) {
	var refusal *kernel.Error
	coverage := slices.Clone(x.Refresh(func() ([]MonitoringCoverage, bool) {
		coverage, failed := x.rebuild()
		refusal = failed
		return coverage, failed == nil
	}))
	if refusal != nil {
		return x.fallback(filter)
	}
	indexed, err := x.Query(platform.RunIndexQuery{Tenant: x.Tenant, Scope: x.Scope, Offset: 0, Limit: 1000})
	if err != nil {
		return x.fallback(filter)
	}
	return FromIndex(indexed.Entries, filter, coverage), nil
}

func (x RunIndex) fallback(filter MonitoringRunFilter) (MonitoringRunPage, *kernel.Error) {
	page, refusal := x.FromOwners(filter)
	if refusal != nil {
		return page, refusal
	}
	page.Coverage = append(page.Coverage, MonitoringCoverage{Source: "run-index", Basis: "latest", Limitation: "index-unavailable"})
	return page, nil
}

func (x RunIndex) rebuild() ([]MonitoringCoverage, *kernel.Error) {
	previous, _ := x.Query(platform.RunIndexQuery{Tenant: x.Tenant, Scope: x.Scope, Offset: 0, Limit: 1000})
	previousStatus := map[string]string{}
	for _, entry := range previous.Entries {
		previousStatus[entry.ID] = entry.Status
	}
	var runs []MonitoringRun
	var coverage []MonitoringCoverage
	for offset := 0; ; {
		page, refusal := x.FromOwners(MonitoringRunFilter{Offset: offset, Limit: 100})
		if refusal != nil {
			return nil, refusal
		}
		coverage = page.Coverage
		runs = append(runs, page.Runs...)
		offset += len(page.Runs)
		if len(page.Runs) == 0 || offset >= page.Total || offset >= 10000 {
			break
		}
	}
	entries := make([]platform.RunIndexEntry, 0, len(runs))
	for _, run := range runs {
		if len(previous.Entries) > 0 && (run.Source == "flow" || run.Source == "agent" || run.Source == "function" || run.Source == "compute" || run.Source == "projection" || run.Source == "code" || run.Source == "effect") &&
			(run.Status == "failed" || run.Status == "cancelled") && previousStatus[run.ID] != run.Status {
			x.Observe(run)
		}
		raw, err := json.Marshal(run)
		if err != nil {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT, Message: fmt.Sprintf("run index: %v", err)}
		}
		entries = append(entries, platform.RunIndexEntry{ID: run.ID, Scope: x.Scope, Type: run.Type, Source: run.Source, App: RunResourceApp(run), Status: run.Status, At: run.When, Payload: raw})
	}
	if err := x.Replace(entries); err != nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNSPECIFIED, Message: err.Error()}
	}
	return coverage, nil
}

func Records[T any](read func() ([]any, bool, *kernel.Error), coverage *MonitoringCoverage) ([]T, *kernel.Error) {
	records, partial, refusal := read()
	if refusal != nil {
		if refusal.Code == pb.ErrorCode_ERROR_CODE_POLICY_DENIED || refusal.Code == pb.ErrorCode_ERROR_CODE_NOT_FOUND {
			coverage.Limitation = "permission-filtered"
			return nil, nil
		}
		return nil, refusal
	}
	coverage.Readable = true
	if partial {
		coverage.Limitation = "read-limit"
	}
	rows := make([]T, 0, len(records))
	for _, row := range records {
		rows = append(rows, row.(T))
	}
	return rows, nil
}

func ComputeStatus(state string) string {
	switch state {
	case "completed":
		return "ok"
	case "failed", "pending", "running", "cancelled":
		return state
	}
	return "unknown"
}

func (r *Runs) Effects(effects []platform.Effect, skipEndpoint, buildApp, retrySchema, retryType string) map[string]bool {
	seen := map[string]bool{}
	for _, effect := range effects {
		if effect.Endpoint == skipEndpoint {
			continue
		}
		seen[effect.ID] = true
		run := MonitoringRun{Type: "delivery", Source: "effect", ID: "effect:" + effect.ID, Title: effect.Event, RecordRef: effect.Target, When: effect.At, Status: EffectStatus(effect.State), OwnerStatus: effect.State}
		if !(effect.App == buildApp && effect.Event == buildApp+"/dispatch") {
			r.Retry(&run, retrySchema, retryType, effect.ID, "retry")
		}
		r.Add(run)
	}
	return seen
}
