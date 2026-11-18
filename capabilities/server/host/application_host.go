package host

import automation "platformserver/services/automate/runtime"

import (
	"context"
	"encoding/json"
	"platformserver/kernel/protocol"
	"platformserver/services/functions/runtime"
	"platformserver/services/objects"
	"platformserver/services/resources/monitoring"
	controlowner "platformserver/services/security/control"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	acceptedformat "platformserver/kernel/acceptedformat"
	"platformserver/kernel/records"
	"platformserver/platform"
	"platformserver/services/actions"
	appowner "platformserver/services/apps"
	"slices"
	"time"
)

func (t *Tenant) Applications(m platform.Member) []protocol.ApplicationView {
	return appowner.Applications(t.Definitions(m))
}
func (t *Tenant) PageView(m platform.Member, app, owner, page string) (protocol.PageView, *platform.Error) {
	return appowner.PageView(t.Definitions(m), t.Entities(m), t.Catalog(m), app, owner, page)
}
func (t *Tenant) ApplicationRuns(m platform.Member, ref platform.AssetRef, offset, limit int, now time.Time, filters ...protocol.ApplicationRunFilter) (protocol.ApplicationRunPage, *platform.Error) {
	return recordRead(t, context.Background(), func(t *Tenant) (protocol.ApplicationRunPage, *platform.Error) {
		return t.applicationContext().ScopedRuns(m, ref, offset, limit, now, filters...)
	})
}

func (t *Tenant) ApplicationRuntimeOf(m platform.Member, ref platform.AssetRef, now time.Time) (protocol.ApplicationRuntime, *platform.Error) {
	if t.quarantined() {
		return t.applicationContext().Runtime(m, ref, now)
	}
	return recordRead(t, context.Background(), func(t *Tenant) (protocol.ApplicationRuntime, *platform.Error) {
		return t.applicationContext().Runtime(m, ref, now)
	})
}
func (t *Tenant) bindApplications(resource platform.AssetRef, release, dependencies string, jobOwner ...string) ([]platform.AssetBinding, error) {
	return t.applicationContext().Bind(resource, release, dependencies, jobOwner...)
}
func (h hostView) ApplicationBindings(resource platform.AssetRef, release, dependencies string) ([]platform.AssetBinding, error) {
	return h.t.bindApplications(resource, release, dependencies)
}
func (t *Tenant) ApplicationRecordDiagnostic(m platform.Member, ref platform.AssetRef, typ, id string, now time.Time) (protocol.ApplicationDiagnostic, *platform.Error) {
	return recordRead(t, context.Background(), func(t *Tenant) (protocol.ApplicationDiagnostic, *platform.Error) {
		return t.applicationContext().RecordDiagnostic(m, ref, typ, id, now)
	})
}

func (t *Tenant) applicationContext() appowner.Context {
	return appowner.Context{Tenant: t.ID, Resolver: PublicationResolver(), Admit: t.Admits, Definitions: t.Definitions, Entity: t.Entity, App: t.App,
		Assets: func() ([]platform.ReleaseAsset, error) { return t.releaseAssetsLocked(nil, false) }, Candidate: t.releases.Candidate,
		Candidates: func() map[string]json.RawMessage { return t.releases.Candidates() }, CloseCandidate: t.candidateWithBindings,
		Rows: func(m platform.Member, typ string, q platform.Query, at time.Time) ([]any, *platform.Error) {
			page, err := t.objectOwner(t.Projection).SelectionFrom(t.Projection, m, typ, q, at)
			return page.Records, err
		},
		Computations: func(m platform.Member) []appowner.Computation {
			effects := []platform.Effect{}
			for _, x := range t.outbound.Select(func(x actions.Queued) bool { return x.Endpoint == acceptedformat.OperationEndpoint }) {
				effects = append(effects, x.Effect)
			}
			out := []appowner.Computation{}
			for _, effect := range effects {
				var binding compute.OperationBinding
				if json.Unmarshal([]byte(effect.Body), &binding) != nil || binding.Build != nil {
					continue
				}
				result, err := t.operationResult(platform.NewCaller(runtime{t: t}, m, controlowner.PlatformApp, false, false), effect.ID)
				if err != nil {
					continue
				}
				out = append(out, appowner.Computation{Effect: effect, Ref: platform.AssetRef{App: effect.App, Kind: platform.AssetCompute, Name: binding.Definition.Name}, Title: binding.Definition.Title, Call: binding.Call, Result: result})
			}
			return out
		}, Effects: func(m platform.Member) ([]platform.Effect, *platform.Error) {
			value, err := t.read(m, "effects")
			effects, _ := value.([]platform.Effect)
			effects = slices.DeleteFunc(effects, func(effect platform.Effect) bool { return effect.Endpoint == acceptedformat.OperationEndpoint })
			return effects, err
		},
		Jobs: func(m platform.Member) ([]appowner.Job, *platform.Error) {
			value, err := t.read(m, "work")
			tasks, _ := value.([]automation.Task)
			out := []appowner.Job{}
			for _, task := range tasks {
				out = append(out, appowner.Job{Applications: task.Applications, ID: task.ID, Kind: task.Kind, App: task.App, Title: task.Title, State: task.State, Error: task.Error, Last: task.Last, Withheld: task.Withheld})
			}
			return out, err
		},
		Readable: t.readable, Quarantined: t.quarantined, RecoveryAllowed: func(m platform.Member) *platform.Error {
			c := platform.NewCaller(runtime{t: t}, m, controlowner.PlatformApp, false, false)
			return authorizationRefusal(t.authorizeNative(c, "application recovery", "read", []string{controlowner.Admin}, true))
		},
		Quota: func(m platform.Member, at time.Time) *platform.AIQuota {
			if t.Models == nil {
				return nil
			}
			quota := t.Models.Quota(m, t.limits(), at)
			return &quota
		},
		Refusals: t.Committed.AllRefusals, Answers: t.Committed.AllAnswers,
		AuthorizationMember: t.authorizationMember,
		RefreshRunIndex: func(key string, now time.Time, rebuild func() bool) {
			t.runIndex.Refresh(key, now, func() ([]monitoring.MonitoringCoverage, bool) { return nil, rebuild() })
		},
		ReplaceRunIndex: func(key string, entries []protocol.RunIndexEntry) error {
			return t.Log().ReplaceRunIndex(t.ID, key, entries)
		},
		QueryRunIndex: func(scope string) ([]protocol.RunIndexEntry, error) {
			indexed, err := t.Log().QueryRunIndex(protocol.RunIndexQuery{Tenant: t.ID, Scope: scope, Offset: 0, Limit: 1000})
			return indexed.Entries, err
		},
		RuntimeOf: t.ApplicationRuntimeOf,
		RunsPage:  t.ApplicationRuns,
		RecordHistory: func(m platform.Member, typ, id string, now time.Time) ([]records.RecordChange, *platform.Error) {
			view, refusal := t.objectOwner(t.Projection).RecordDetail(m, typ, id, now, objects.RecordSectionHistory)
			if refusal != nil {
				return nil, refusal
			}
			return view.History, nil
		},
		RecordOwner:     func(typ string) string { return t.Projection.Type(typ).Info().App },
		Receipts:        func(owner string) []*pb.ChangeRecord { return t.App(owner).Authority().RecordsFor(t.ID) },
		Decision:        func(key string) *platform.DecisionDefinition { return t.Committed.DecisionDefinition(key) },
		SchemaVersion:   func(key string) (platform.SchemaVersion, bool) { return t.Committed.SchemaVersion(key) },
		VisibleEvidence: t.visibleObservationEvidence,
		AI:              t.applicationAILocked, AIUsage: t.applicationAIUsage, RecordCount: func(m platform.Member, typ string, at time.Time) (int, *platform.Error) {
			page, err := t.Records(t.readContext(), m, typ, platform.Query{Limit: 1}, at)
			return page.Total, err
		},
	}
}
