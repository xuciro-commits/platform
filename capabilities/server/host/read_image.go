package host

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

	runtimekernel "platformserver/kernel"
	persistence "platformserver/kernel/store"
	"platformserver/kernel/transport"
	"platformserver/platform"
	"platformserver/services/data/lineage"
	computeowner "platformserver/services/functions/runtime"
)

type tenantReadImage struct {
	state       *Tenant
	epoch       uint64
	work        uint64
	definitions uint64
	records     uint64
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
	t.Projection.Unlock()
	if previous := t.readImage.Load(); previous != nil && previous.epoch == epoch && previous.work == work && previous.definitions == definitions && previous.records == generation && previous.operations == operations && previous.position == position {
		return
	}
	state, err := t.forkReadState(context.Background())
	if err != nil {
		t.quarantine(fmt.Errorf("publish read image: %w", err))
		return
	}
	if previous := t.readImage.Load(); previous != nil && previous.records == generation && previous.definitions == definitions {
		state.markings.Image = previous.state.markings.Image
	}
	image := &tenantReadImage{state: state, epoch: epoch, work: work, definitions: definitions, records: generation, operations: operations, position: position, next: make(chan struct{})}
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
	meters.readImages.Add(ctx, 1, metric.WithAttributes(attribute.String("platform.tenant", t.ID), attribute.String("platform.outcome", outcome)))
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

func readFromImage[T any](t *Tenant, ctx context.Context, read func(*Tenant) (T, *kernel.Error)) (value T, refusal *kernel.Error) {
	for attempt := 0; attempt < 3; attempt++ {
		if ctx.Err() != nil || t.quarantined() {
			return value, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
		image := t.readImage.Load()
		if image == nil {
			break
		}
		value, refusal, err := readImageState(t, image.state, ctx, read)
		if err == nil {
			return value, refusal
		}
		var mismatch *persistence.StatePositionError
		if !errors.As(err, &mismatch) || mismatch.Position <= mismatch.Applied {
			if mismatch != nil {
				t.countReadImage(ctx, "quarantined")
			}
			return value, refusal
		}
		if attempt == 2 {
			break
		}
		t.countReadImage(ctx, "retried")
		timer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return value, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		case <-image.next:
			timer.Stop()
		case <-timer.C:
		}
	}
	t.countReadImage(ctx, "fallback")
	return readFromLane(t, ctx, read)
}

func (t *Tenant) forkReadState(ctx context.Context) (*Tenant, error) {
	control, err := t.captureControl()
	if err != nil {
		return nil, err
	}
	view, err := t.forkControl(control)
	if err != nil {
		return nil, err
	}
	for _, job := range t.work.Jobs {
		if current := view.work.Job(job.ID); current != nil {
			current.NextRun = job.NextRun
		}
	}
	view.readScope = &recordReadScope{ctx: ctx}
	view.readOrigin = t
	if t.readOrigin != nil {
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
	view.apps = make([]platform.App, len(t.apps))
	view.owner = make(map[string]platform.App, len(t.owner))
	for i, app := range t.apps {
		ledger, err := app.AcceptedLedger().ForkRead()
		if err != nil {
			return nil, err
		}
		fork, err := app.Fork(ledger)
		if err != nil {
			return nil, err
		}
		view.apps[i] = fork
	}
	roles, err := ownerRolesOf(view.apps, view.ownerActive)
	if err != nil {
		return nil, err
	}
	view.ownerRoles = roles
	for key, owner := range t.owner {
		view.owner[key] = view.app(owner.Manifest().ID)
	}
	for protocol, binding := range view.bindings {
		binding.provider = view.app(binding.provider.Manifest().ID)
		view.bindings[protocol] = binding
	}
	view.Committed = t.Committed.Fork()
	view.releases.Restore(t.releases.SnapshotState())
	view.console.Restore(t.console.SnapshotState())
	view.sequences.Restore(t.sequences.Clone())
	notices, sequence := t.notices.State()
	view.notices.Restore(notices, sequence)
	view.Files, view.Secrets = t.files(), t.Secrets
	view.Outbound, view.AIClient = t.Outbound, t.AIClient
	view.ComputeWorker, view.Compiler = t.ComputeWorker, t.Compiler
	view.Quota, view.index = t.Quota, t.index
	if t.services.NewAgentEvaluator != nil {
		view.agentEvaluator = t.services.NewAgentEvaluator()
	}
	view.breakers = t.breakers.Fork()
	t.opsMu.Lock()
	view.quota = t.quota.Fork()
	t.opsMu.Unlock()
	view.staged = computeowner.NewChannel(t.ID, view.files)
	view.staged.Restore(t.staged.Snapshot())
	view.claims = t.claims.Fork(t.ID)
	view.DefinitionEpoch.Store(t.DefinitionEpoch.Load())
	view.runIndex.CopyGeneration(&t.runIndex)
	view.includeArchivedDefinitions = t.includeArchivedDefinitions
	view.i18n = transport.NewTranslator(func() []platform.App { return view.apps },
		func(app platform.App) platform.Manifest {
			return transport.ManifestOf(app, view.definitionsFingerprint())
		}, view.words.Clone, PlatformLanguages)
	view.i18n.InstallApplications(view.definitions)
	view.audit = t.audit.Fork()
	view.audit.SetSink(view.readOrigin.audit.Remember, view.readOrigin.audit.ReadPersonal)
	view.observations.CopyCounts(&t.observations)
	view.LastSlowHold.Store(t.LastSlowHold.Load())
	for _, app := range view.apps {
		(hostView{t: view, app: app}).bindOwner(app)
	}
	if backend, ok := view.Projection.Current().Backend().(runtimekernel.AcceptedStateReader); ok {
		view.installRuntimeHistory(backend, func() int64 { return view.Projection.Current().Applied() })
	}
	return view, nil
}
