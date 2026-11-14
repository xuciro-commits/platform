package host

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sync"

	runtimekernel "platformserver/kernel"
	acceptedformat "platformserver/kernel/acceptedformat"
	"platformserver/kernel/records"
	"platformserver/platform"
	"platformserver/services/actions"
	automation "platformserver/services/automate/runtime"
	"platformserver/services/devops/releases"
	"platformserver/services/resources/authoring"
)

var snapshotBuild = sync.OnceValue(func() string { return CodeOf() })

func (t *Tenant) restoreRecords(saved map[string][]records.StoredState) (map[string][]records.StoredState, error) {
	return t.RestoreRecords(saved, build.CheckRunApplicationBindings)
}

func (t *Tenant) snapshotCompatibility() (runtimekernel.SnapshotCompatibility, error) {
	if t.startupSnapshot != nil {
		return *t.startupSnapshot, nil
	}
	return runtimekernel.Compatibility(t.apps)
}

func (t *Tenant) snapshotStorageKey(code string, raw []byte) (string, error) {
	compatibility, err := t.snapshotCompatibility()
	if err != nil {
		return "", err
	}
	return compatibility.StorageKey(code, raw)
}

func (t *Tenant) snapshots() runtimekernel.TenantSnapshots[automation.Task, actions.Endpoint] {
	return runtimekernel.TenantSnapshots[automation.Task, actions.Endpoint]{
		Compatibility: t.snapshotCompatibility, Build: snapshotBuild, Apps: t.apps, AppOf: t.App,
		Inputs:      func(app string) platform.AcceptedInputApp { return t.inputs[app] },
		Quarantined: t.quarantined,
		Pending:     func() bool { return len(t.events) > 0 },
		Scopes:      func() bool { return t.currentReadScopes },
		Detach:      t.detachSnapshotRead,
		Invalidate:  t.invalidateReadImage,
		CheckRecord: build.CheckRunApplicationBindings,
		Capture:     t.captureServices, Definitions: t.restoreDefinitions, Services: t.restoreServices, Operations: t.restoreOperations,
	}
}

func (t *Tenant) Snapshot(position func() int64) (json.RawMessage, int64, error) {
	return t.snapshots().Snapshot(&t.Sequence, position)
}

func (t *Tenant) snapshotRuntime(ctx context.Context, j Journals) (json.RawMessage, int64, error) {
	return t.snapshots().SnapshotRuntime(&t.Sequence, ctx, j)
}

func (t *Tenant) Restore(raw json.RawMessage) error {
	return t.snapshots().Restore(&t.Sequence, raw)
}

func (t *Tenant) detachSnapshotRead() {
	t.Projection.Current().Bind(nil)
	if store, ok := t.Projection.Current().Backend().(runtimekernel.AcceptedStateReader); ok && t.Projection.Current().Position() != nil {
		t.installRuntimeHistory(store, t.Projection.Current().Position())
	}
}

func (t *Tenant) captureServices(s *runtimekernel.TenantImage[automation.Task, actions.Endpoint]) (func(), error) {
	if err := s.CaptureEvidence(&t.Sequence); err != nil {
		return nil, err
	}
	releaseState := t.releases.SnapshotState()
	s.ReleaseRequests, s.ReleaseCandidates, s.ReleaseApplied = releaseState.ReleaseRequests, releaseState.ReleaseCandidates, releaseState.ReleaseApplied
	s.ActiveRelease, s.Sealed, s.ReleaseOrigins = releaseState.ActiveRelease, releaseState.Sealed, releaseState.ReleaseOrigins
	t.opsMu.Lock()
	s.Acted, s.Failed = t.acted, []string{}
	maps.Copy(s.Bindings, t.bindings.All())
	if err := s.CaptureOperations(&t.Sequence); err != nil {
		t.opsMu.Unlock()
		return nil, err
	}
	t.work.Lock()
	work, workErr := t.work.Snapshot()
	t.work.Unlock()
	s.Queues = work.Queues
	s.Failed = append(s.Failed, work.Failed...)
	s.Tasks = append(s.Tasks, work.Tasks...)
	s.Jobs = append(s.Jobs, work.Jobs...)
	if workErr != nil {
		t.opsMu.Unlock()
		return nil, workErr
	}
	s.Settings, s.Endpoints, s.Words = t.settings.Snapshot(), t.endpoints.Saved(), t.words.Clone()
	for _, x := range t.outbound.Select(nil) {
		s.Outbound = append(s.Outbound, acceptedformat.EffectState{Effect: x.Effect, Since: x.Since})
	}
	image := t.console.SnapshotState()
	s.HostLifecycle, s.Support, s.Migrations = image.Lifecycle, image.Support, image.Migrations
	s.HostRequests, s.RuntimeSelection, s.DisabledOwners = image.Requests, image.RuntimeSelection, image.DisabledOwners
	s.Staged = t.staged.Snapshot()
	return t.opsMu.Unlock, nil
}

func (t *Tenant) restoreDefinitions(s *runtimekernel.TenantImage[automation.Task, actions.Endpoint], held map[string][]records.StoredState) error {
	if err := s.RestoreEvidence(&t.Sequence); err != nil {
		return err
	}
	if len(held) > 0 || s.Runtime != nil || build.PublishedDefinitionsMissing(s.Records,
		func(ref platform.AssetRef) bool {
			return slices.ContainsFunc(t.definitions, func(d platform.Definition) bool { return d.Ref == ref && d.Source == "tenant" })
		},
		func(name string, version int) bool {
			owner, ok := t.procs.(platform.FlowDefinitionPresence)
			return ok && owner.HasDefinition(build.ID, name, version)
		}) {
		if err := t.reinstall(); err != nil {
			return err
		}
	}
	if len(held) > 0 {
		left, err := t.restoreRecords(held)
		if err != nil {
			return err
		}
		if len(left) > 0 {
			return fmt.Errorf("tenant %s: no entity type %s", t.ID, slices.Sorted(maps.Keys(left))[0])
		}
	}
	return nil
}

func (t *Tenant) restoreServices(s *runtimekernel.TenantImage[automation.Task, actions.Endpoint]) error {
	t.AuditLog.Restore(s.Audit, s.Deliveries)
	image := platform.HostControlImage{Lifecycle: s.HostLifecycle, Support: s.Support, Migrations: s.Migrations, Requests: s.HostRequests, RuntimeSelection: s.RuntimeSelection, DisabledOwners: s.DisabledOwners}
	if err := acceptedformat.ValidateHostImage(t.ID, image); err != nil {
		return err
	}
	t.console.Restore(image)
	if s.Staged != nil {
		t.staged.Restore(s.Staged)
	}
	return nil
}

func (t *Tenant) restoreOperations(s *runtimekernel.TenantImage[automation.Task, actions.Endpoint]) error {
	t.releases.Restore(releases.State{ReleaseRequests: s.ReleaseRequests, ReleaseCandidates: s.ReleaseCandidates, ReleaseApplied: s.ReleaseApplied, ActiveRelease: s.ActiveRelease, Sealed: s.Sealed, ReleaseOrigins: s.ReleaseOrigins})
	for protocol, provider := range s.Bindings {
		if !t.router().Rebind(protocol, provider) {
			return fmt.Errorf("tenant %s: %s is not a provider of %s", t.ID, provider, protocol)
		}
	}
	for _, job := range s.Jobs {
		if err := platform.CheckApplicationBindings(job.Applications); err != nil {
			return err
		}
	}
	for _, effect := range s.Outbound {
		if err := platform.CheckApplicationBindings(effect.Effect.Applications); err != nil {
			return err
		}
	}
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	t.acted = s.Acted
	if err := s.RestoreOperations(&t.Sequence); err != nil {
		return err
	}
	t.work.Lock()
	err := t.work.Restore(t.ID, automation.WorkState{Queues: s.Queues, Failed: s.Failed, Tasks: s.Tasks, Jobs: s.Jobs})
	t.work.Unlock()
	if err != nil {
		return err
	}
	t.settings.Restore(s.Settings)
	t.words.Restore(s.Words)
	t.i18n.Reset()
	t.endpoints.Restore(s.Endpoints)
	effects := make([]actions.Queued, 0, len(s.Outbound))
	for _, x := range s.Outbound {
		effects = append(effects, actions.Queued{Effect: x.Effect, Since: x.Since})
	}
	t.outbound.Restore(effects)
	return nil
}
