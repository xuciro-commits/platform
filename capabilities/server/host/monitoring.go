package host

import (
	"cmp"
	"encoding/json"
	runtimekernel "platformserver/kernel"
	"platformserver/kernel/protocol"
	automation "platformserver/services/automate/runtime"
	dataowner "platformserver/services/data"
	compute "platformserver/services/functions/runtime"
	"platformserver/services/resources/monitoring"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
	"platformserver/services/actions"
	build "platformserver/services/resources/authoring"
	controlowner "platformserver/services/security/control"
)

func (t *Tenant) monitoringRuns(m platform.Member, filter monitoring.MonitoringRunFilter, now time.Time) (monitoring.MonitoringRunPage, *platform.Error) {
	m = t.authorizationMember(m)
	return monitoring.RunIndex{
		Tenant: t.ID, Scope: m.ID,
		Query:   func(q protocol.RunIndexQuery) (protocol.RunIndexPage, error) { return t.Log().QueryRunIndex(q) },
		Replace: func(entries []protocol.RunIndexEntry) error { return t.Log().ReplaceRunIndex(t.ID, m.ID, entries) },
		Refresh: func(rebuild func() ([]monitoring.MonitoringCoverage, bool)) []monitoring.MonitoringCoverage {
			return t.runIndex.Refresh(m.ID, now, rebuild)
		},
		FromOwners: func(filter monitoring.MonitoringRunFilter) (monitoring.MonitoringRunPage, *platform.Error) {
			return t.monitoringRunsFromOwners(m, filter, now)
		},
		Observe: func(run monitoring.MonitoringRun) {
			t.observeOwnerRun(now, "run", run.ID, monitoring.RunResourceApp(run), run.RecordRef, run.Status, run.Title, run.OwnerStatus)
		},
	}.Runs(filter)
}

func monitoringRecords[T any](t *Tenant, m platform.Member, typ string, coverage *monitoring.MonitoringCoverage, now time.Time) ([]T, *platform.Error) {
	return monitoring.Records[T](func() ([]any, bool, *platform.Error) {
		page, refusal := t.objectOwner(t.Projection).SelectionFrom(t.Projection, m, typ, platform.Query{Sort: []string{"id"}, Archived: true}, now)
		return page.Records, page.Partial, refusal
	}, coverage)
}

func (t *Tenant) monitoringRunsFromOwners(m platform.Member, filter monitoring.MonitoringRunFilter, now time.Time) (monitoring.MonitoringRunPage, *platform.Error) {
	m = t.authorizationMember(m)
	return build.OwnerRuns{Filter: filter, Now: now, Data: t.DataOwner(),
		Admits:      func() *platform.Error { return t.Admits(m) },
		Definitions: func() ([]platform.Definition, error) { return t.DefinitionsFor(m) },
		Catalog:     func() []platform.Action { return t.Catalog(m) },
		Readable:    func(ref string) bool { return t.readable(m, ref, now) },
		CanRetry: func(schema, typ, id string) bool {
			c := platform.NewCaller(runtime{t: t, scoped: true}, m, "", false, false)
			decision := t.authorizeFrom(t.Projection, c, platform.AuthzRequest{Principal: m.ID, Tenant: m.Tenant, Kind: "action", Action: schema, Type: typ, Record: id}, now, false)
			return decision.Allow && t.delegatedBound(m, &pb.Submission{Schema: &pb.SchemaRef{Name: schema}, Target: &pb.EntityRef{Type: typ, Id: id}}) == nil
		},
		Select: func(typ string) ([]any, bool, *platform.Error) {
			page, refusal := t.objectOwner(t.Projection).SelectionFrom(t.Projection, m, typ, platform.Query{Sort: []string{"id"}, Archived: true}, now)
			return page.Records, page.Partial, refusal
		},
		Operations: func() []build.OperationRun {
			var operations []build.OperationRun
			for _, queued := range t.outbound.Select(func(x actions.Queued) bool { return x.Endpoint == acceptedformat.OperationEndpoint }) {
				result, refused := t.operationResult(platform.NewCaller(runtime{t: t, scoped: true}, m, controlowner.PlatformApp, false, false), queued.Effect.ID)
				operation := build.OperationRun{Effect: queued.Effect, Result: result, Refusal: refused}
				var binding compute.OperationBinding
				if json.Unmarshal([]byte(queued.Effect.Body), &binding) != nil {
					operation.Invalid = true
				}
				operation.Build, operation.Title = binding.Build != nil, cmp.Or(binding.Definition.Title, binding.Definition.Name)
				operations = append(operations, operation)
			}
			return operations
		},
		Work: func() ([]build.WorkRun, *platform.Error) {
			value, refusal := t.read(m, "work")
			if refusal != nil {
				return nil, refusal
			}
			var tasks []build.WorkRun
			for _, task := range value.([]automation.Task) {
				run := build.WorkRun{ID: task.ID, Title: task.Title, App: task.App, Kind: task.Kind, Status: task.Status, State: task.State, Last: task.Last, Due: task.Due}
				if task.Kind == "delivery" && task.Event != nil {
					run.RecordRef = runtimekernel.Target(task.Event.Record.GetSubmission())
				}
				tasks = append(tasks, run)
			}
			return tasks, nil
		},
		Effects: func() ([]platform.Effect, *platform.Error) {
			value, refusal := t.read(m, "effects")
			if refusal != nil {
				return nil, refusal
			}
			return value.([]platform.Effect), nil
		},
		Schemas:      build.OwnerRunSchemas{FlowRetry: automation.SchemaFlowRetry, WorkRetry: controlowner.SchemaWorkRetry, WorkType: controlowner.WorkType, EffectRetry: controlowner.SchemaEffectRetry, EffectType: controlowner.EffectType},
		Integrations: func() ([]dataowner.IntegrationEffect, *platform.Error) { return t.integrationEffects(m, now) },
	}.Page()
}

func (t *Tenant) tenantUsage(now time.Time) protocol.TenantUsage {
	origin := t
	if t.readOrigin != nil {
		origin = t.readOrigin
	}
	pos := t.Projection.Current().Applied()
	if pos <= 0 {
		if t.SnapshotPosition > 0 {
			pos = t.SnapshotPosition
		} else if origin.RuntimePosition != nil {
			pos = origin.RuntimePosition()
		}
	}
	var entries func(int64) ([]monitoring.JournalSlice, error)
	if origin.JournalEntries != nil {
		entries = func(after int64) ([]monitoring.JournalSlice, error) {
			raw, err := origin.JournalEntries(t.readContext(), after)
			out := make([]monitoring.JournalSlice, len(raw))
			for i, item := range raw {
				out[i] = monitoring.JournalSlice{At: item.At, App: item.App, Body: item.Body}
			}
			return out, err
		}
	}
	in := monitoring.UsageInput{Tenant: t.ID, Now: now, Records: t.Projection, Position: pos, Entries: entries}
	if t.Models != nil {
		in.AIUsage = t.Models.Usage()
	}
	if t.PackageOwner != nil {
		in.Packages = t.PackageOwner.InstalledPackages()
	}
	return monitoring.ProjectUsage(origin.Monitoring, in)
}
