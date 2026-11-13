package host

import (
	"cmp"
	"encoding/json"
	runtimekernel "platformserver/kernel"
	dataowner "platformserver/services/data"
	"platformserver/services/functions/runtime"
	"platformserver/services/resources/monitoring"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
	"platformserver/services/actions"
	"platformserver/services/automate/runtime"
	"platformserver/services/resources/authoring"
	controlowner "platformserver/services/security/control"
)

func (t *Tenant) monitoringRuns(m platform.Member, filter monitoring.MonitoringRunFilter, now time.Time) (monitoring.MonitoringRunPage, *kernel.Error) {
	m = t.authorizationMember(m)
	return monitoring.RunIndex{
		Tenant: t.ID, Scope: m.ID,
		Query:   func(q platform.RunIndexQuery) (platform.RunIndexPage, error) { return t.Log().QueryRunIndex(q) },
		Replace: func(entries []platform.RunIndexEntry) error { return t.Log().ReplaceRunIndex(t.ID, m.ID, entries) },
		Refresh: func(rebuild func() ([]monitoring.MonitoringCoverage, bool)) []monitoring.MonitoringCoverage {
			return t.runIndex.Refresh(m.ID, now, rebuild)
		},
		FromOwners: func(filter monitoring.MonitoringRunFilter) (monitoring.MonitoringRunPage, *kernel.Error) {
			return t.monitoringRunsFromOwners(m, filter, now)
		},
		Observe: func(run monitoring.MonitoringRun) {
			t.observeOwnerRun(now, "run", run.ID, monitoring.RunResourceApp(run), run.RecordRef, run.Status, run.Title, run.OwnerStatus)
		},
	}.Runs(filter)
}

func monitoringRecords[T any](t *Tenant, m platform.Member, typ string, coverage *monitoring.MonitoringCoverage, now time.Time) ([]T, *kernel.Error) {
	return monitoring.Records[T](func() ([]any, bool, *kernel.Error) {
		page, refusal := t.recordSelectionFrom(t.Projection, m, typ, platform.Query{Sort: []string{"id"}, Archived: true}, now)
		return page.Records, page.Partial, refusal
	}, coverage)
}

func collectRuns[T any](t *Tenant, m platform.Member, runs *monitoring.Runs, now time.Time, source, basis, limitation, typ string, add func([]T)) *kernel.Error {
	coverage := monitoring.MonitoringCoverage{Source: source, Basis: basis, Limitation: limitation}
	rows, refusal := monitoringRecords[T](t, m, typ, &coverage, now)
	if refusal != nil {
		return refusal
	}
	add(rows)
	runs.Cover(coverage)
	return nil
}

func (t *Tenant) monitoringRunsFromOwners(m platform.Member, filter monitoring.MonitoringRunFilter, now time.Time) (monitoring.MonitoringRunPage, *kernel.Error) {
	out := monitoring.MonitoringRunPage{Runs: []monitoring.MonitoringRun{}, Offset: filter.Offset, Limit: filter.Limit, Coverage: []monitoring.MonitoringCoverage{}}
	m = t.authorizationMember(m)
	if refusal := t.Admits(m); refusal != nil {
		return out, refusal
	}
	definitions, err := t.DefinitionsFor(m)
	if err != nil {
		return out, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	visible := map[platform.AssetRef]bool{}
	for _, definition := range definitions {
		visible[definition.Ref] = true
	}
	catalog := map[string]platform.Action{}
	for _, action := range t.Catalog(m) {
		catalog[action.Schema] = action
	}
	runs := monitoring.NewRuns(filter, visible, func(ref string) bool { return t.readable(m, ref, now) }, func(run *monitoring.MonitoringRun, schema, typ, id, kind string) {
		action, found := catalog[schema]
		if !found || action.Target != typ || action.SchemaVersion == nil || run.Status != "failed" {
			return
		}
		c := platform.NewCaller(runtime{t: t, scoped: true}, m, "", false, false)
		decision := t.authorizeFrom(t.Projection, c, platform.AuthzRequest{Principal: m.ID, Tenant: m.Tenant, Kind: "action", Action: schema, Type: typ, Record: id}, now, false)
		if !decision.Allow || t.delegatedBound(m, &pb.Submission{Schema: &pb.SchemaRef{Name: schema}, Target: &pb.EntityRef{Type: typ, Id: id}}) != nil {
			return
		}
		run.Retryable, run.RetrySchema = true, schema
		run.RetryTarget = &monitoring.MonitoringRetryTarget{Type: typ, ID: id}
		run.RetrySchemaVersion, run.RetryKind = action.SchemaVersion, kind
	})
	coverage := monitoring.MonitoringCoverage{Source: "source", Basis: "latest", Limitation: "latest-only"}
	sources, refusal := monitoringRecords[dataowner.Source](t, m, dataowner.SourceType, &coverage, now)
	if refusal != nil {
		return runs.Partial(), refusal
	}
	t.DataOwner().SourceRuns(runs, sources, &coverage, now)
	runs.Cover(coverage)
	if refusal := collectRuns(t, m, runs, now, "pipeline", "latest", "latest-only", dataowner.PipelineType, func(rows []dataowner.Pipeline) { dataowner.PipelineRuns(runs, rows) }); refusal != nil {
		return runs.Partial(), refusal
	}
	compilations := map[string]time.Time{}
	coverage = monitoring.MonitoringCoverage{Source: "compute", Basis: "retained", Readable: true, Limitation: "caller-only"}
	for _, queued := range t.outbound.Select(func(x actions.Queued) bool { return x.Endpoint == acceptedformat.OperationEndpoint }) {
		effect := queued.Effect
		result, refused := t.operationResult(platform.NewCaller(runtime{t: t, scoped: true}, m, controlowner.PlatformApp, false, false), effect.ID)
		if refused != nil {
			if refused.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED && refused.Code != pb.ErrorCode_ERROR_CODE_NOT_FOUND {
				coverage.Limitation = "input-unavailable"
			}
			continue
		}
		var binding compute.OperationBinding
		if json.Unmarshal([]byte(effect.Body), &binding) != nil {
			coverage.Limitation = "input-unavailable"
			continue
		}
		if binding.Build != nil {
			compilations[effect.ID] = effect.At
			continue
		}
		runs.Add(monitoring.MonitoringRun{Type: "function", Source: "compute", ID: "compute:" + effect.ID, Title: cmp.Or(binding.Definition.Title, binding.Definition.Name), ResourceRef: result.Ref, RecordRef: effect.Target, When: effect.At, Status: monitoring.ComputeStatus(result.State), OwnerStatus: result.State})
	}
	runs.Cover(coverage)
	for _, collect := range []func() *kernel.Error{
		func() *kernel.Error {
			return collectRuns(t, m, runs, now, "code", "latest", "latest-only", build.CodeType, func(rows []build.Code) { build.CodeRuns(runs, rows, compilations) })
		},
		func() *kernel.Error {
			return collectRuns(t, m, runs, now, "projection", "latest", "latest-only", dataowner.DatasetType, func(rows []dataowner.Dataset) { dataowner.DatasetRuns(runs, rows) })
		},
		func() *kernel.Error {
			return collectRuns(t, m, runs, now, "function", "retained", "", build.FunctionCallType, func(rows []build.FunctionRun) { build.FunctionRuns(runs, rows) })
		},
		func() *kernel.Error {
			return collectRuns(t, m, runs, now, "flow", "retained", "permission-filtered", platform.FlowInstanceType, func(rows []platform.FlowInstance) { runs.Flows(rows, automation.SchemaFlowRetry) })
		},
		func() *kernel.Error {
			return collectRuns(t, m, runs, now, "agent", "retained", "permission-filtered", platform.RunType, runs.Agents)
		},
	} {
		if refusal := collect(); refusal != nil {
			return runs.Partial(), refusal
		}
	}
	coverage = monitoring.MonitoringCoverage{Source: "work", Basis: "current"}
	value, refusal := t.read(m, "work")
	if refusal != nil && refusal.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED && refusal.Code != pb.ErrorCode_ERROR_CODE_NOT_FOUND {
		return runs.Partial(), refusal
	}
	if refusal == nil {
		coverage.Readable = true
		for _, task := range value.([]automation.Task) {
			kind := "automation"
			if task.Kind == "delivery" {
				kind = "delivery"
			} else if task.Kind != "job" {
				continue
			}
			run := monitoring.MonitoringRun{Type: kind, Source: "work", ID: "work:" + task.ID, Title: task.Title, ResourceRef: &platform.AssetRef{App: task.App, Kind: platform.AssetApp, Name: task.App}, When: task.Last, Status: task.Status, OwnerStatus: task.State}
			if run.When.IsZero() {
				run.When = task.Due
			}
			if task.Kind == "delivery" && task.Event != nil {
				run.RecordRef = runtimekernel.Target(task.Event.Record.GetSubmission())
			}
			if task.Kind == "job" || task.State == "failed" {
				runs.Retry(&run, controlowner.SchemaWorkRetry, controlowner.WorkType, task.ID, map[bool]string{true: "rerun", false: "retry"}[task.Kind == "job"])
			}
			runs.Add(run)
		}
	} else {
		coverage.Limitation = "permission-filtered"
	}
	runs.Cover(coverage)
	effects := map[string]bool{}
	coverage = monitoring.MonitoringCoverage{Source: "effect", Basis: "retained", Limitation: "retained-window"}
	value, refusal = t.read(m, "effects")
	if refusal != nil && refusal.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED && refusal.Code != pb.ErrorCode_ERROR_CODE_NOT_FOUND {
		return runs.Partial(), refusal
	}
	if refusal == nil {
		coverage.Readable = true
		effects = runs.Effects(value.([]platform.Effect), acceptedformat.OperationEndpoint, build.ID, controlowner.SchemaEffectRetry, controlowner.EffectType)
	} else {
		coverage.Limitation = "permission-filtered"
	}
	runs.Cover(coverage)
	coverage = monitoring.MonitoringCoverage{Source: "integration-effect", Basis: "retained", Limitation: "metadata-only"}
	integrations, refusal := t.integrationEffects(m, now)
	if refusal != nil && refusal.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
		return runs.Partial(), refusal
	}
	if refusal == nil {
		coverage.Readable = true
		for _, effect := range integrations {
			if effects[effect.ID] {
				continue
			}
			at := effect.Last
			if at.IsZero() {
				at = effect.Due
			}
			run := monitoring.MonitoringRun{Type: "delivery", Source: "effect", ID: "effect:" + effect.ID, Title: effect.Event, When: at, Status: monitoring.EffectStatus(effect.State), OwnerStatus: effect.State}
			if effect.Event != build.ID+"/dispatch" {
				runs.Retry(&run, controlowner.SchemaEffectRetry, controlowner.EffectType, effect.ID, "retry")
			}
			runs.Add(run)
		}
	} else {
		coverage.Limitation = "permission-filtered"
	}
	runs.Cover(coverage,
		monitoring.MonitoringCoverage{Source: "delivery-attempts", Basis: "retained", Limitation: "not-included"})
	return runs.Page(), nil
}
