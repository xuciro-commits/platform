package host

import (
	"context"
	"log"
	"maps"
	"platformserver/services/resources/monitoring"
	"slices"

	"platformkernel/kernel"

	runtimekernel "platformserver/kernel"
	"platformserver/kernel/transport"
	"platformserver/services/data/lineage"
	releaseowner "platformserver/services/devops/releases"
	"platformserver/services/functions/runtime"
	"platformserver/services/resources/authoring"
)

type tenantReadImage struct {
	state       *Tenant
	epoch       uint64
	work        uint64
	definitions uint64
	records     uint64
	resources   uint64
	operations  int64
	position    int64
	next        chan struct{}
}

func (t *Tenant) viewCache() *transport.ViewCache {
	if t.readViews != nil {
		return t.readViews
	}
	return &t.views
}

func (t *Tenant) invalidateReadImage() {
	if image := t.readImage.Swap(nil); image != nil {
		close(image.next)
	}
}

func (t *Tenant) endLane() {
	defer t.closeRecordRead()
	if t.readError() != nil || t.quarantined() {
		return
	}
	operations := t.change.Operations()
	epoch, work, definitions, position := t.StateEpoch.Load(), t.WorkEpoch.Load(), t.DefinitionEpoch.Load(), t.Projection.Current().Applied()
	t.Projection.Lock()
	generation := t.Projection.Generation()
	resources := t.Projection.LiveVersion(build.ResourceType)
	t.Projection.Unlock()
	if previous := t.readImage.Load(); previous != nil && previous.epoch == epoch && previous.work == work && previous.definitions == definitions && previous.records == generation && previous.operations == operations && previous.position == position {
		return
	}
	state, err := t.forkReadState(context.Background())
	if err != nil {
		monitoring.CountReadImage(context.Background(), t.ID, "unpublished")
		log.Printf("tenant %s: publish read image: %v", t.ID, err)
		return
	}
	if previous := t.readImage.Load(); previous != nil && previous.records == generation && previous.definitions == definitions {
		state.markings.Image = previous.state.markings.Image
	}
	state.readScope.definitions = releaseowner.NewDefinitionPlanCache(definitions, resources)
	if previous := t.readImage.Load(); previous != nil && previous.definitions == definitions && previous.resources == resources && maps.Equal(previous.state.disabledOwners, state.disabledOwners) {
		state.readScope.definitions = previous.state.readScope.definitions
	}
	image := &tenantReadImage{state: state, epoch: epoch, work: work, definitions: definitions, records: generation, resources: resources, operations: operations, position: position, next: make(chan struct{})}
	if previous := t.readImage.Swap(image); previous != nil {
		close(previous.next)
	}
	if t.change.Flush() {
		wakeWork()
	}
}

func readView() runtimekernel.ReadView[*Tenant] {
	return runtimekernel.ReadView[*Tenant]{Fork: (*Tenant).forkReadState, Open: (*Tenant).openRecordRead, Close: (*Tenant).closeRecordRead,
		Failure: (*Tenant).readError, Refusal: (*Tenant).recordReadRefusal}
}

func readImageState[T any](t *Tenant, state *Tenant, ctx context.Context, read func(*Tenant) (T, *kernel.Error)) (value T, refusal *kernel.Error, failure error) {
	defer runtimekernel.RecoverImageRead(t.quarantine, &value, &refusal, &failure)
	return readInView(state, ctx, read)
}

func readInView[T any](state *Tenant, ctx context.Context, read func(*Tenant) (T, *kernel.Error)) (T, *kernel.Error, error) {
	return runtimekernel.ReadInView(readView(), state, ctx, read)
}

func readFromImage[T any](t *Tenant, ctx context.Context, read func(*Tenant) (T, *kernel.Error)) (T, *kernel.Error) {
	return runtimekernel.ImageRead[T]{
		Quarantined: t.quarantined,
		Load: func() (func() (T, *kernel.Error, error), <-chan struct{}) {
			image := t.readImage.Load()
			if image == nil {
				return nil, nil
			}
			return func() (T, *kernel.Error, error) { return readImageState(t, image.state, ctx, read) }, image.next
		},
		Count:    func(outcome string) { monitoring.CountReadImage(ctx, t.ID, outcome) },
		Fallback: func() (T, *kernel.Error) { return readFromLane(t, ctx, read) },
	}.Run(ctx)
}

func (t *Tenant) forkReadState(ctx context.Context) (*Tenant, error) {
	view := t.newControlFork()
	view.ConnectorRoster = t.ConnectorRoster.Fork()
	view.settings.Restore(t.settings.Snapshot())
	view.words.Restore(t.words.Clone())
	view.endpoints.Restore(t.endpoints.Saved())
	t.outbound.CopyTo(&view.outbound)
	if err := t.work.ForkInto(t.ID, &view.work, func(app string) bool { return view.App(app) != nil }); err != nil {
		return nil, err
	}
	view.readScope = &recordReadScope{ctx: ctx}
	view.readOrigin = t
	if t.readOrigin != nil {
		view.readScope.definitions = t.readScope.definitions
		view.readOrigin = t.readOrigin
		view.readViews = t.viewCache()
		view.markings.Image = t.markings.Image
	} else {
		view.readViews = &view.views
		view.markings.Image = &lineage.ImageMarkings{}
	}
	view.currentReadScopes = t.currentReadScopes
	view.Projection = t.Projection.ForkRecords()
	view.Projection.Current().Detach()
	view.definitions = slices.Clone(t.definitions)
	view.disabledOwners = maps.Clone(t.disabledOwners)
	apps, err := runtimekernel.ForkApps(t.apps)
	if err != nil {
		return nil, err
	}
	view.ownerSet = t.with(apps)
	roles, err := ownerRolesOf(view.ownerSet, view.OwnerActive)
	if err != nil {
		return nil, err
	}
	view.ownerRoles = roles
	view.Committed = t.Committed.Fork()
	view.releases.Restore(t.releases.SnapshotState())
	view.console.Restore(t.console.SnapshotState())
	view.Sequences.Restore(t.Sequences.Clone())
	notices, sequence := t.Notices.State()
	view.Notices.Restore(notices, sequence)
	view.tenantPorts = t.tenantPorts
	view.Files = t.files()
	view.index = t.index
	if t.services.NewAgentEvaluator != nil {
		view.agentEvaluator = t.services.NewAgentEvaluator()
	}
	view.breakers = t.breakers.Fork()
	view.work.CopyRotation(&t.work)
	view.staged = compute.NewChannel(t.ID, view.files)
	view.staged.Restore(t.staged.Snapshot())
	view.Claims = t.Claims.Fork(t.ID)
	view.DefinitionEpoch.Store(t.DefinitionEpoch.Load())
	view.runIndex.CopyGeneration(&t.runIndex)
	view.includeArchivedDefinitions = t.includeArchivedDefinitions
	view.i18n = view.translator()
	view.i18n.InstallApplications(view.definitions)
	view.AuditLog = t.AuditLog.Fork()
	view.AuditLog.SetSink(view.readOrigin.AuditLog.Remember, view.readOrigin.AuditLog.ReadPersonal)
	view.ObservationBuffer.CopyCounts(&t.ObservationBuffer)
	view.LastSlowHold.Store(t.LastSlowHold.Load())
	for _, app := range view.apps {
		(hostView{t: view, app: app}).bindOwner(app)
	}
	if backend, ok := view.Projection.Current().Backend().(runtimekernel.AcceptedStateReader); ok {
		view.installRuntimeHistory(backend, func() int64 { return view.Projection.Current().Applied() })
	}
	return view, nil
}
