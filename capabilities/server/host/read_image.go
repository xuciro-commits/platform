package host

import (
	"context"
	"errors"
	"log"
	"maps"
	"platformserver/services/resources/monitoring"
	"slices"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

	runtimekernel "platformserver/kernel"
	persistence "platformserver/kernel/store"
	"platformserver/kernel/transport"
	"platformserver/platform"
	"platformserver/services/actions"
	automation "platformserver/services/automate/runtime"
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
	t.change.mu.Lock()
	operations := t.change.seq + int64(len(t.change.pendingData))
	t.change.mu.Unlock()
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
		t.countReadImage(context.Background(), "unpublished")
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
	t.change.mu.Lock()
	pending := len(t.change.pendingData) > 0
	for owner := range t.change.pendingData {
		t.change.seq++
		t.change.data++
		if owner == "" {
			t.change.global++
		} else {
			if t.change.owners == nil {
				t.change.owners = map[string]int64{}
			}
			t.change.owners[owner]++
		}
	}
	t.change.pendingData = nil
	if t.change.wake != nil {
		close(t.change.wake)
		t.change.wake = nil
	}
	t.change.mu.Unlock()
	if pending {
		wakeWork()
	}
}

func (t *Tenant) countReadImage(ctx context.Context, outcome string) {
	monitoring.CountReadImage(ctx, t.ID, outcome)
}

func readImageState[T any](t *Tenant, state *Tenant, ctx context.Context, read func(*Tenant) (T, *kernel.Error)) (value T, refusal *kernel.Error, failure error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			if err, ok := recovered.(error); ok {
				failure = err
				var mismatch *persistence.StatePositionError
				if errors.As(err, &mismatch) && mismatch.Position < mismatch.Applied {
					t.quarantine(err)
				}
				var zero T
				value, refusal = zero, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
				return
			}
			panic(recovered)
		}
	}()
	view, err := state.forkReadState(ctx)
	if err != nil {
		return value, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The read scope could not be prepared: {reason}", err.Error()), err
	}
	defer func() {
		view.closeRecordRead()
		if err := view.readError(); err != nil {
			var zero T
			value, refusal, failure = zero, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}, err
		}
	}()
	view.openRecordRead(ctx)
	if refusal := view.recordReadRefusal(); refusal != nil {
		return value, refusal, view.readError()
	}
	value, refusal = read(view)
	return value, refusal, nil
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
		Count:    func(outcome string) { t.countReadImage(ctx, outcome) },
		Fallback: func() (T, *kernel.Error) { return readFromLane(t, ctx, read) },
	}.Run(ctx)
}

func (t *Tenant) forkReadState(ctx context.Context) (*Tenant, error) {
	view := t.newControlFork()
	view.ConnectorRoster = t.ConnectorRoster.Fork()
	view.settings.Restore(t.settings.Snapshot())
	view.words.Restore(t.words.Clone())
	view.endpoints.Restore(t.endpoints.Saved())
	var effects []actions.Queued
	for _, effect := range t.outbound.Select(nil) {
		effects = append(effects, actions.Queued{Effect: effect.Effect, Since: effect.Since})
	}
	view.outbound.Restore(effects)
	t.work.Lock()
	work, err := t.work.Snapshot()
	for _, job := range t.work.Jobs {
		if current := view.work.Job(job.ID); current != nil {
			current.NextRun = job.NextRun
		}
	}
	t.work.Unlock()
	if err != nil {
		return nil, err
	}
	if err := view.work.Install(t.ID, automation.WorkState{Queues: work.Queues, Failed: work.Failed, Tasks: work.Tasks, Jobs: work.Jobs}, func(app string) bool { return view.App(app) != nil }); err != nil {
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
	apps := make([]platform.App, len(t.apps))
	for i, app := range t.apps {
		ledger, err := app.AcceptedLedger().ForkRead()
		if err != nil {
			return nil, err
		}
		fork, err := app.Fork(ledger)
		if err != nil {
			return nil, err
		}
		apps[i] = fork
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
	t.work.Lock()
	view.work.Rotation = t.work.Rotation
	t.work.Unlock()
	view.staged = compute.NewChannel(t.ID, view.files)
	view.staged.Restore(t.staged.Snapshot())
	view.Claims = t.Claims.Fork(t.ID)
	view.DefinitionEpoch.Store(t.DefinitionEpoch.Load())
	view.runIndex.CopyGeneration(&t.runIndex)
	view.includeArchivedDefinitions = t.includeArchivedDefinitions
	view.i18n = transport.NewTranslator(func() []platform.App { return view.apps },
		func(app platform.App) platform.Manifest {
			return transport.ManifestOf(app, view.definitionsFingerprint())
		}, view.words.Clone, PlatformLanguages)
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
