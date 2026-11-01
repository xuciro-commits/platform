package platformserver

import (
	"cmp"
	"encoding/json"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/engine/automation"
	"platformserver/kernel/acceptedformat"
	controlowner "platformserver/kernel/control"
	"platformserver/platform"
	"platformserver/studio/build"
)

type MonitoringRun struct {
	Type               string                  `json:"type" enum:"sync,build,automation,function,delivery"`
	ID                 string                  `json:"id"`
	Source             string                  `json:"source" enum:"source,pipeline,code,projection,function,compute,work,effect"`
	Title              string                  `json:"title"`
	ResourceRef        *platform.AssetRef      `json:"resourceRef,omitempty"`
	RecordRef          string                  `json:"recordRef,omitempty"`
	When               time.Time               `json:"when,omitzero"`
	Status             string                  `json:"status" enum:"ok,failed,pending,running,warning,paused,cancelled,unknown"`
	OwnerStatus        string                  `json:"ownerStatus,omitempty"`
	Retryable          bool                    `json:"retryable"`
	RetrySchema        string                  `json:"retrySchema,omitempty"`
	RetryTarget        *MonitoringRetryTarget  `json:"retryTarget,omitempty"`
	RetrySchemaVersion *platform.SchemaVersion `json:"retrySchemaVersion,omitempty"`
	RetryKind          string                  `json:"retryKind,omitempty" enum:"retry,rerun"`
}

type MonitoringRetryTarget struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type MonitoringCoverage struct {
	Source     string `json:"source"`
	Readable   bool   `json:"readable"`
	Basis      string `json:"basis" enum:"latest,current,retained"`
	Limitation string `json:"limitation,omitempty"`
}

type MonitoringOverview struct {
	FailedRuns       int `json:"failedRuns"`
	DelayedSyncs     int `json:"delayedSyncs"`
	FailedDeliveries int `json:"failedDeliveries"`
}

type MonitoringRunPage struct {
	Runs     []MonitoringRun      `json:"runs"`
	Total    int                  `json:"total"`
	Offset   int                  `json:"offset"`
	Limit    int                  `json:"limit"`
	Overview MonitoringOverview   `json:"overview"`
	Coverage []MonitoringCoverage `json:"coverage"`
}

type MonitoringRunFilter struct {
	Type, Status  string
	Offset, Limit int
}

func (f MonitoringRunFilter) valid() bool {
	return f.Offset >= 0 && f.Limit >= 1 && f.Limit <= 100 &&
		(f.Type == "" || slices.Contains([]string{"sync", "build", "automation", "function", "delivery"}, f.Type)) &&
		(f.Status == "" || slices.Contains([]string{"ok", "failed", "pending", "running", "warning", "paused", "cancelled", "unknown"}, f.Status))
}

func monitoringRecords[T any](t *Tenant, m platform.Member, typ string, coverage *MonitoringCoverage, now time.Time) ([]T, *kernel.Error) {
	page, refusal := t.recordSelectionFrom(t.records, m, typ, platform.Query{Sort: []string{"id"}, Archived: true}, now)
	if refusal != nil {
		if refusal.Code == pb.ErrorCode_ERROR_CODE_POLICY_DENIED || refusal.Code == pb.ErrorCode_ERROR_CODE_NOT_FOUND {
			coverage.Limitation = "permission-filtered"
			return nil, nil
		}
		return nil, refusal
	}
	coverage.Readable = true
	if page.Partial {
		coverage.Limitation = "read-limit"
	}
	rows := make([]T, 0, len(page.Records))
	for _, row := range page.Records {
		rows = append(rows, row.(T))
	}
	return rows, nil
}

func (t *Tenant) monitoringRuns(m platform.Member, filter MonitoringRunFilter, now time.Time) (MonitoringRunPage, *kernel.Error) {
	out := MonitoringRunPage{Runs: []MonitoringRun{}, Offset: filter.Offset, Limit: filter.Limit, Coverage: []MonitoringCoverage{}}
	m = t.authorizationMember(m)
	if refusal := t.admits(m); refusal != nil {
		return out, refusal
	}
	definitions, err := t.definitionsFor(m)
	if err != nil {
		return out, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	visible := map[platform.AssetRef]bool{}
	for _, definition := range definitions {
		visible[definition.Ref] = true
	}
	readable := map[string]bool{}
	appendRun := func(run MonitoringRun) {
		if run.Title == "" {
			run.Title = run.ID
		}
		if run.ResourceRef != nil && !visible[*run.ResourceRef] {
			run.ResourceRef = nil
		}
		if run.RecordRef != "" {
			allowed, known := readable[run.RecordRef]
			if !known {
				allowed = t.readable(m, run.RecordRef, now)
				readable[run.RecordRef] = allowed
			}
			if !allowed {
				run.RecordRef = ""
			}
		}
		if run.Status == "failed" {
			out.Overview.FailedRuns++
			if run.Type == "delivery" {
				out.Overview.FailedDeliveries++
			}
		}
		if (filter.Type == "" || run.Type == filter.Type) && (filter.Status == "" || run.Status == filter.Status) {
			out.Runs = append(out.Runs, run)
		}
	}
	actions := map[string]platform.Action{}
	for _, action := range t.Catalog(m) {
		actions[action.Schema] = action
	}
	retry := func(run *MonitoringRun, schema, typ, id, kind string) {
		action, found := actions[schema]
		if !found || action.Target != typ || action.SchemaVersion == nil || run.Status != "failed" {
			return
		}
		c := platform.NewCaller(runtime{t}, m, "", false, false)
		decision := t.authorizeFrom(t.records, c, platform.AuthzRequest{Principal: m.ID, Tenant: m.Tenant, Kind: "action", Action: schema, Type: typ, Record: id}, now, false)
		if !decision.Allow || t.delegatedBound(m, &pb.Submission{Schema: &pb.SchemaRef{Name: schema}, Target: &pb.EntityRef{Type: typ, Id: id}}) != nil {
			return
		}
		run.Retryable, run.RetrySchema = true, schema
		run.RetryTarget = &MonitoringRetryTarget{Type: typ, ID: id}
		run.RetrySchemaVersion, run.RetryKind = action.SchemaVersion, kind
	}
	coverage := MonitoringCoverage{Source: "source", Basis: "latest", Limitation: "latest-only"}
	sources, refusal := monitoringRecords[build.Source](t, m, build.SourceType, &coverage, now)
	if refusal != nil {
		return out, refusal
	}
	for _, source := range sources {
		if source.Last == nil && !source.Requested {
			continue
		}
		run := MonitoringRun{Type: "sync", Source: "source", ID: "source:" + source.ID, Title: source.Title, ResourceRef: &platform.AssetRef{App: build.ID, Kind: platform.AssetSource, Name: source.Name}, RecordRef: build.SourceType + "/" + source.ID, Status: "unknown", OwnerStatus: source.State, When: source.Changed.At}
		if source.Last != nil {
			run.When, run.Status = source.Last.At, "ok"
			if source.Last.Error != "" || source.Last.Failed > 0 {
				run.Status = "failed"
			}
		}
		if source.Requested || source.Last != nil && source.Last.Execution != nil && source.Last.Execution.Pending {
			run.Status = "pending"
		}
		if !source.Archived && source.State == "published" && !source.Stream {
			retry(&run, build.SourceType+".pull", build.SourceType, source.ID, "rerun")
			fixed, fixedErr := t.app(build.ID).(build.IntegrationDefinitions).ExecutionSource(source)
			if fixedErr != nil {
				coverage.Limitation = "definition-unavailable"
			} else if every, parseErr := time.ParseDuration(fixed.Every); parseErr == nil && every > 0 && source.Last != nil && now.After(source.Last.At.Add(every)) {
				out.Overview.DelayedSyncs++
			}
		}
		appendRun(run)
	}
	out.Coverage = append(out.Coverage, coverage)
	coverage = MonitoringCoverage{Source: "pipeline", Basis: "latest", Limitation: "latest-only"}
	pipelines, refusal := monitoringRecords[build.Pipeline](t, m, build.PipelineType, &coverage, now)
	if refusal != nil {
		return out, refusal
	}
	for _, pipeline := range pipelines {
		if pipeline.Last == nil && !pipeline.Requested {
			continue
		}
		run := MonitoringRun{Type: "build", Source: "pipeline", ID: "pipeline:" + pipeline.ID, Title: pipeline.Title, ResourceRef: &platform.AssetRef{App: build.ID, Kind: platform.AssetPipeline, Name: pipeline.Name}, RecordRef: build.PipelineType + "/" + pipeline.ID, When: pipeline.Changed.At, Status: "unknown", OwnerStatus: pipeline.State}
		if pipeline.Last != nil {
			run.When, run.Status = pipeline.Last.At, "ok"
			if pipeline.Last.Error != "" || pipeline.Last.Failed > 0 {
				run.Status = "failed"
			} else if pipeline.Last.Quarantined > 0 {
				run.Status = "warning"
			}
		}
		if pipeline.Requested || pipeline.Last != nil && pipeline.Last.Execution != nil && pipeline.Last.Execution.Pending {
			run.Status = "pending"
		}
		if !pipeline.Archived && pipeline.State == "published" {
			retry(&run, build.PipelineType+".run", build.PipelineType, pipeline.ID, "rerun")
		}
		appendRun(run)
	}
	out.Coverage = append(out.Coverage, coverage)
	t.opsMu.Lock()
	operations := []platform.Effect{}
	for _, effect := range t.outbound {
		if effect.Endpoint == acceptedformat.OperationEndpoint {
			operations = append(operations, effect.Effect)
		}
	}
	t.opsMu.Unlock()
	compilations := map[string]time.Time{}
	coverage = MonitoringCoverage{Source: "compute", Basis: "retained", Readable: true, Limitation: "caller-only"}
	for _, effect := range operations {
		result, refused := t.operationResult(platform.NewCaller(runtime{t}, m, controlowner.PlatformApp, false, false), effect.ID)
		if refused != nil {
			if refused.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED && refused.Code != pb.ErrorCode_ERROR_CODE_NOT_FOUND {
				coverage.Limitation = "input-unavailable"
			}
			continue
		}
		var binding operationBinding
		if json.Unmarshal([]byte(effect.Body), &binding) != nil {
			coverage.Limitation = "input-unavailable"
			continue
		}
		if binding.Build != nil {
			compilations[effect.ID] = effect.At
			continue
		}
		status := "unknown"
		switch result.State {
		case "completed":
			status = "ok"
		case "failed", "pending", "running":
			status = result.State
		case "cancelled":
			status = "cancelled"
		}
		appendRun(MonitoringRun{Type: "function", Source: "compute", ID: "compute:" + effect.ID, Title: cmp.Or(binding.Definition.Title, binding.Definition.Name), ResourceRef: result.Ref, RecordRef: effect.Target, When: effect.At, Status: status, OwnerStatus: result.State})
	}
	out.Coverage = append(out.Coverage, coverage)
	coverage = MonitoringCoverage{Source: "code", Basis: "latest", Limitation: "latest-only"}
	codes, refusal := monitoringRecords[build.Code](t, m, build.CodeType, &coverage, now)
	if refusal != nil {
		return out, refusal
	}
	for _, code := range codes {
		if code.Call == "" {
			continue
		}
		run := MonitoringRun{Type: "build", Source: "code", ID: "code:" + code.ID, Title: code.Title, ResourceRef: &platform.AssetRef{App: build.ID, Kind: platform.AssetCompute, Name: code.Name}, RecordRef: build.CodeType + "/" + code.ID, When: code.Changed.At, Status: "unknown", OwnerStatus: code.State}
		if at, known := compilations[code.Call]; known {
			run.When = at
		}
		switch code.State {
		case "compiling":
			run.Status = "pending"
		case "compiled", "published":
			run.Status = "ok"
		case "failed":
			run.Status = "failed"
			if !code.Archived {
				retry(&run, build.SchemaCodeCompile, build.CodeType, code.ID, "rerun")
			}
		}
		appendRun(run)
	}
	out.Coverage = append(out.Coverage, coverage)
	coverage = MonitoringCoverage{Source: "projection", Basis: "latest", Limitation: "latest-only"}
	datasets, refusal := monitoringRecords[build.Dataset](t, m, build.DatasetType, &coverage, now)
	if refusal != nil {
		return out, refusal
	}
	for _, dataset := range datasets {
		for _, projection := range dataset.Projections {
			status := "ok"
			if projection.Paused != "" || projection.Refused > 0 {
				status = "failed"
			}
			appendRun(MonitoringRun{Type: "build", Source: "projection", ID: "projection:" + dataset.ID + ":" + projection.Object, Title: cmp.Or(dataset.Title, dataset.ID), ResourceRef: &platform.AssetRef{App: build.ID, Kind: platform.AssetDataset, Name: dataset.Name}, RecordRef: build.DatasetType + "/" + dataset.ID, When: projection.At, Status: status})
		}
	}
	out.Coverage = append(out.Coverage, coverage)
	coverage = MonitoringCoverage{Source: "function", Basis: "retained"}
	functions, refusal := monitoringRecords[build.FunctionRun](t, m, build.FunctionCallType, &coverage, now)
	if refusal != nil {
		return out, refusal
	}
	for _, function := range functions {
		status := "unknown"
		switch function.State {
		case "pending":
			status = "pending"
		case "ready":
			status = "ok"
		case "rejected":
			status = "failed"
		}
		appendRun(MonitoringRun{Type: "function", Source: "function", ID: "function:" + function.ID, Title: function.Function, ResourceRef: &platform.AssetRef{App: cmp.Or(function.App, build.ID), Kind: platform.AssetFunction, Name: function.Function}, RecordRef: build.FunctionCallType + "/" + function.ID, When: function.Created.At, Status: status, OwnerStatus: function.State})
	}
	out.Coverage = append(out.Coverage, coverage)
	coverage = MonitoringCoverage{Source: "work", Basis: "current"}
	value, refusal := t.read(m, "work")
	if refusal != nil && refusal.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED && refusal.Code != pb.ErrorCode_ERROR_CODE_NOT_FOUND {
		return out, refusal
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
			run := MonitoringRun{Type: kind, Source: "work", ID: "work:" + task.ID, Title: task.Title, ResourceRef: &platform.AssetRef{App: task.App, Kind: platform.AssetApp, Name: task.App}, When: task.Last, Status: task.Status, OwnerStatus: task.State}
			if run.When.IsZero() {
				run.When = task.Due
			}
			if task.Kind == "delivery" && task.Event != nil {
				run.RecordRef = target(task.Event.Record.GetSubmission())
			}
			if task.Kind == "job" || task.State == "failed" {
				retry(&run, controlowner.SchemaWorkRetry, controlowner.WorkType, task.ID, map[bool]string{true: "rerun", false: "retry"}[task.Kind == "job"])
			}
			appendRun(run)
		}
	} else {
		coverage.Limitation = "permission-filtered"
	}
	out.Coverage = append(out.Coverage, coverage)
	effects := map[string]bool{}
	coverage = MonitoringCoverage{Source: "effect", Basis: "retained", Limitation: "retained-window"}
	value, refusal = t.read(m, "effects")
	if refusal != nil && refusal.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED && refusal.Code != pb.ErrorCode_ERROR_CODE_NOT_FOUND {
		return out, refusal
	}
	if refusal == nil {
		coverage.Readable = true
		for _, effect := range value.([]platform.Effect) {
			if effect.Endpoint == acceptedformat.OperationEndpoint {
				continue
			}
			effects[effect.ID] = true
			run := MonitoringRun{Type: "delivery", Source: "effect", ID: "effect:" + effect.ID, Title: effect.Event, RecordRef: effect.Target, When: effect.At, Status: monitoringEffectStatus(effect.State), OwnerStatus: effect.State}
			if !(effect.App == build.ID && effect.Event == build.ID+"/dispatch") {
				retry(&run, controlowner.SchemaEffectRetry, controlowner.EffectType, effect.ID, "retry")
			}
			appendRun(run)
		}
	} else {
		coverage.Limitation = "permission-filtered"
	}
	out.Coverage = append(out.Coverage, coverage)
	coverage = MonitoringCoverage{Source: "integration-effect", Basis: "retained", Limitation: "metadata-only"}
	integrations, refusal := t.integrationEffects(m, now)
	if refusal != nil && refusal.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
		return out, refusal
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
			run := MonitoringRun{Type: "delivery", Source: "effect", ID: "effect:" + effect.ID, Title: effect.Event, When: at, Status: monitoringEffectStatus(effect.State), OwnerStatus: effect.State}
			if effect.Event != build.ID+"/dispatch" {
				retry(&run, controlowner.SchemaEffectRetry, controlowner.EffectType, effect.ID, "retry")
			}
			appendRun(run)
		}
	} else {
		coverage.Limitation = "permission-filtered"
	}
	out.Coverage = append(out.Coverage, coverage,
		MonitoringCoverage{Source: "delivery-attempts", Basis: "retained", Limitation: "not-included"},
		MonitoringCoverage{Source: "flow", Basis: "retained", Limitation: "not-included"},
		MonitoringCoverage{Source: "agent", Basis: "retained", Limitation: "not-included"})
	slices.SortFunc(out.Runs, func(a, b MonitoringRun) int {
		if order := b.When.Compare(a.When); order != 0 {
			return order
		}
		return strings.Compare(a.ID, b.ID)
	})
	out.Total = len(out.Runs)
	start := min(filter.Offset, out.Total)
	out.Runs = out.Runs[start : start+min(filter.Limit, out.Total-start)]
	return out, nil
}

func monitoringEffectStatus(state string) string {
	switch state {
	case "delivered":
		return "ok"
	case "failed", "rejected":
		return "failed"
	case "pending", "retrying":
		return "pending"
	case "held":
		return "paused"
	case "discarded", "compensated":
		return "cancelled"
	default:
		return "unknown"
	}
}
