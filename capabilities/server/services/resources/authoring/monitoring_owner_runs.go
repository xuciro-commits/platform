package build

import (
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
	dataowner "platformserver/services/data"
	"platformserver/services/resources/monitoring"
)

type OperationRun struct {
	Effect  platform.Effect
	Result  platform.OperationResult
	Refusal *kernel.Error
	Title   string
	Build   bool
	Invalid bool
}

type WorkRun struct {
	ID, Title, App, Kind, Status, State, RecordRef string
	Last, Due                                      time.Time
}

type OwnerRunSchemas struct {
	FlowRetry, WorkRetry, WorkType, EffectRetry, EffectType string
}

type OwnerRuns struct {
	Filter       monitoring.MonitoringRunFilter
	Now          time.Time
	Data         *dataowner.Owner
	Admits       func() *kernel.Error
	Definitions  func() ([]platform.Definition, error)
	Catalog      func() []platform.Action
	Readable     func(ref string) bool
	CanRetry     func(schema, typ, id string) bool
	Select       func(typ string) ([]any, bool, *kernel.Error)
	Operations   func() []OperationRun
	Work         func() ([]WorkRun, *kernel.Error)
	Effects      func() ([]platform.Effect, *kernel.Error)
	Schemas      OwnerRunSchemas
	Integrations func() ([]dataowner.IntegrationEffect, *kernel.Error)
}

func collectOwnerRuns[T any](x OwnerRuns, runs *monitoring.Runs, source, basis, limitation, typ string, add func([]T)) *kernel.Error {
	coverage := monitoring.MonitoringCoverage{Source: source, Basis: basis, Limitation: limitation}
	rows, refusal := monitoring.Records[T](func() ([]any, bool, *kernel.Error) { return x.Select(typ) }, &coverage)
	if refusal != nil {
		return refusal
	}
	add(rows)
	runs.Cover(coverage)
	return nil
}

func (x OwnerRuns) Page() (monitoring.MonitoringRunPage, *kernel.Error) {
	out := monitoring.MonitoringRunPage{Runs: []monitoring.MonitoringRun{}, Offset: x.Filter.Offset, Limit: x.Filter.Limit, Coverage: []monitoring.MonitoringCoverage{}}
	if refusal := x.Admits(); refusal != nil {
		return out, refusal
	}
	definitions, err := x.Definitions()
	if err != nil {
		return out, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	visible := map[platform.AssetRef]bool{}
	for _, definition := range definitions {
		visible[definition.Ref] = true
	}
	catalog := map[string]platform.Action{}
	for _, action := range x.Catalog() {
		catalog[action.Schema] = action
	}
	runs := monitoring.NewRuns(x.Filter, visible, x.Readable, func(run *monitoring.MonitoringRun, schema, typ, id, kind string) {
		action, found := catalog[schema]
		if !found || action.Target != typ || action.SchemaVersion == nil || run.Status != "failed" || !x.CanRetry(schema, typ, id) {
			return
		}
		run.Retryable, run.RetrySchema = true, schema
		run.RetryTarget = &monitoring.MonitoringRetryTarget{Type: typ, ID: id}
		run.RetrySchemaVersion, run.RetryKind = action.SchemaVersion, kind
	})
	coverage := monitoring.MonitoringCoverage{Source: "source", Basis: "latest", Limitation: "latest-only"}
	sources, refusal := monitoring.Records[dataowner.Source](func() ([]any, bool, *kernel.Error) { return x.Select(dataowner.SourceType) }, &coverage)
	if refusal != nil {
		return runs.Partial(), refusal
	}
	x.Data.SourceRuns(runs, sources, &coverage, x.Now)
	runs.Cover(coverage)
	if refusal := collectOwnerRuns(x, runs, "pipeline", "latest", "latest-only", dataowner.PipelineType, func(rows []dataowner.Pipeline) { dataowner.PipelineRuns(runs, rows) }); refusal != nil {
		return runs.Partial(), refusal
	}
	compilations := map[string]time.Time{}
	coverage = monitoring.MonitoringCoverage{Source: "compute", Basis: "retained", Readable: true, Limitation: "caller-only"}
	for _, operation := range x.Operations() {
		effect, result := operation.Effect, operation.Result
		if refused := operation.Refusal; refused != nil {
			if refused.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED && refused.Code != pb.ErrorCode_ERROR_CODE_NOT_FOUND {
				coverage.Limitation = "input-unavailable"
			}
			continue
		}
		if operation.Invalid {
			coverage.Limitation = "input-unavailable"
			continue
		}
		if operation.Build {
			compilations[effect.ID] = effect.At
			continue
		}
		runs.Add(monitoring.MonitoringRun{Type: "function", Source: "compute", ID: "compute:" + effect.ID, Title: operation.Title, ResourceRef: result.Ref, RecordRef: effect.Target, When: effect.At, Status: monitoring.ComputeStatus(result.State), OwnerStatus: result.State})
	}
	runs.Cover(coverage)
	for _, collect := range []func() *kernel.Error{
		func() *kernel.Error {
			return collectOwnerRuns(x, runs, "code", "latest", "latest-only", CodeType, func(rows []Code) { CodeRuns(runs, rows, compilations) })
		},
		func() *kernel.Error {
			return collectOwnerRuns(x, runs, "projection", "latest", "latest-only", dataowner.DatasetType, func(rows []dataowner.Dataset) { dataowner.DatasetRuns(runs, rows) })
		},
		func() *kernel.Error {
			return collectOwnerRuns(x, runs, "function", "retained", "", FunctionCallType, func(rows []FunctionRun) { FunctionRuns(runs, rows) })
		},
		func() *kernel.Error {
			return collectOwnerRuns(x, runs, "flow", "retained", "permission-filtered", platform.FlowInstanceType, func(rows []platform.FlowInstance) { runs.Flows(rows, x.Schemas.FlowRetry) })
		},
		func() *kernel.Error {
			return collectOwnerRuns(x, runs, "agent", "retained", "permission-filtered", platform.RunType, runs.Agents)
		},
	} {
		if refusal := collect(); refusal != nil {
			return runs.Partial(), refusal
		}
	}
	coverage = monitoring.MonitoringCoverage{Source: "work", Basis: "current"}
	tasks, refusal := x.Work()
	if refusal != nil && refusal.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED && refusal.Code != pb.ErrorCode_ERROR_CODE_NOT_FOUND {
		return runs.Partial(), refusal
	}
	if refusal == nil {
		coverage.Readable = true
		for _, task := range tasks {
			kind := "automation"
			if task.Kind == "delivery" {
				kind = "delivery"
			} else if task.Kind != "job" {
				continue
			}
			run := monitoring.MonitoringRun{Type: kind, Source: "work", ID: "work:" + task.ID, Title: task.Title, ResourceRef: &platform.AssetRef{App: task.App, Kind: platform.AssetApp, Name: task.App}, RecordRef: task.RecordRef, When: task.Last, Status: task.Status, OwnerStatus: task.State}
			if run.When.IsZero() {
				run.When = task.Due
			}
			if task.Kind == "job" || task.State == "failed" {
				runs.Retry(&run, x.Schemas.WorkRetry, x.Schemas.WorkType, task.ID, map[bool]string{true: "rerun", false: "retry"}[task.Kind == "job"])
			}
			runs.Add(run)
		}
	} else {
		coverage.Limitation = "permission-filtered"
	}
	runs.Cover(coverage)
	effects := map[string]bool{}
	coverage = monitoring.MonitoringCoverage{Source: "effect", Basis: "retained", Limitation: "retained-window"}
	retained, refusal := x.Effects()
	if refusal != nil && refusal.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED && refusal.Code != pb.ErrorCode_ERROR_CODE_NOT_FOUND {
		return runs.Partial(), refusal
	}
	if refusal == nil {
		coverage.Readable = true
		effects = runs.Effects(retained, acceptedformat.OperationEndpoint, ID, x.Schemas.EffectRetry, x.Schemas.EffectType)
	} else {
		coverage.Limitation = "permission-filtered"
	}
	runs.Cover(coverage)
	coverage = monitoring.MonitoringCoverage{Source: "integration-effect", Basis: "retained", Limitation: "metadata-only"}
	integrations, refusal := x.Integrations()
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
			if effect.Event != ID+"/dispatch" {
				runs.Retry(&run, x.Schemas.EffectRetry, x.Schemas.EffectType, effect.ID, "retry")
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
