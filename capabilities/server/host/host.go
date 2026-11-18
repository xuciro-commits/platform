package host

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	runtimekernel "platformserver/kernel"
	runtimeprotocol "platformserver/kernel/protocol"
	records "platformserver/kernel/records"
	"platformserver/kernel/transport"
	"platformserver/platform"
	"platformserver/services/actions"
	"platformserver/services/ai/gateway"
	knowledgeindex "platformserver/services/ai/retrieval"
	automation "platformserver/services/automate/runtime"
	dataowner "platformserver/services/data"
	"platformserver/services/data/blobs"
	"platformserver/services/data/lineage"
	"platformserver/services/data/transform"
	"platformserver/services/devops/releases"
	"platformserver/services/functions/runtime"
	"platformserver/services/resources/authoring"
	"platformserver/services/resources/monitoring"

	controlowner "platformserver/services/security/control"
)

type Tenant struct {
	runtimekernel.Sequence
	agentEvaluator    platform.AgentEvaluator
	services          runtimeprotocol.RuntimeCapabilities
	disabledOwners    map[string]bool
	installingSet     bool
	currentReadScopes bool
	readScope         *recordReadScope
	readOrigin        *Tenant
	readImage         atomic.Pointer[tenantReadImage]
	preparedResult    *preparedAccepted
	startupSnapshot   *runtimekernel.SnapshotCompatibility
	simulation        bool
	simulationTrace   func(platform.DecisionTrace)
	schedules         dataowner.ReadSchedules
	runIndex          runtimekernel.RunIndexCache[[]monitoring.MonitoringCoverage]
	controlAnswer     func(platform.Effect, platform.Outcome, time.Time) *platform.Error
	ownerRoles
	index        *knowledgeindex.Index
	i18n         transport.Translator
	views        transport.ViewCache
	readViews    *transport.ViewCache
	streamMu     sync.Mutex
	lineageCache lineage.Cache[lineage.Host]
	markings     lineage.MarkingCache[*records.Store]
	ownerSet
	releases      releases.Store
	console       controlowner.HostState
	staged        compute.Channel
	definitions   []platform.Definition
	flowArtifacts automation.FrameCollector
	events        []automation.QueuedEvent
	bindings      *actions.Bindings
	change        runtimekernel.ChangeFeed
	hops          int
	acted         int
	opsMu         sync.Mutex
	work          automation.WorkBoard
	breakers      integrations.Breakers
	settings      controlowner.Settings
	words         controlowner.Words
	endpoints     actions.Endpoints
	outbound      actions.Outbox
	tenantPorts
	cancels  compute.Cancels
	memFiles resources.MemoryFiles
	uploads  resources.Uploads

	includeArchivedDefinitions bool
}

func (t *Tenant) Audit() []platform.AuditEntry { return t.AuditLog.All() }

func NewTenantWithCapabilities(id string, services runtimeprotocol.RuntimeCapabilities, apps ...platform.App) (*Tenant, error) {
	if services.BindOwner == nil {
		return nil, fmt.Errorf("tenant composition requires owner bindings")
	}
	if err := aigateway.BindPolicy(&services); err != nil {
		return nil, err
	}
	if err := services.Check(); err != nil {
		return nil, err
	}
	owners, err := newOwnerSet(apps)
	if err != nil {
		return nil, fmt.Errorf("tenant %s: %w", id, err)
	}
	t := &Tenant{ID: id, ownerSet: owners, bindings: actions.NewBindings(nil), Works: runtimeprotocol.NewWorks(), work: automation.NewWorkBoard(), index: &knowledgeindex.Index{}, releases: releases.NewStore(PublicationResolver()),
		ConnectorRoster: runtimekernel.NewConnectorRoster(id), Projection: records.NewStore()}
	t.bindAuditStore()
	t.Lane.BindReadScope(t.beginRecordRead, t.endLane)
	t.observeLane()
	t.services = services
	t.Claims = runtimekernel.NewSourceClaims()
	if services.NewAgentEvaluator != nil {
		t.agentEvaluator = services.NewAgentEvaluator()
	}
	t.Projection.SetRules(services.Rules)
	t.staged = compute.NewChannel(id, t.files)
	t.i18n = t.translator()
	for i, a := range apps {
		m := a.Manifest()
		if a.Authority() == nil {
			return nil, fmt.Errorf("tenant %s: app %s has no decision ledger", id, m.ID)
		}
		if err := t.bindings.Bind(apps[:i], a); err != nil {
			return nil, fmt.Errorf("tenant %s: %w", id, err)
		}
		(hostView{t: t, app: a}).bindOwner(a)
		_, subscriber := a.(platform.Subscriber)
		if err := build.CheckSubscriptions(m, subscriber); err != nil {
			return nil, fmt.Errorf("tenant %s: %w", id, err)
		}
		_, runsJobs := a.(platform.Runner)
		if err := build.CheckManifest(a.Manifest(), runsJobs); err != nil {
			return nil, fmt.Errorf("tenant %s: %s: %v", id, m.ID, err)
		}
		if err := t.Projection.Declare(a); err != nil {
			return nil, fmt.Errorf("tenant %s: %s: %v", id, m.ID, err)
		}
		for _, j := range m.Jobs {
			t.work.AddJob(&automation.Task{ScheduledTask: platform.ScheduledTask{ID: "job:" + m.ID + "/" + j.Name, Kind: "job", App: m.ID, Title: j.Title, State: "scheduled"}, Job: j})
		}
		if err := runtimekernel.ClaimNames(t.owner, id, m); err != nil {
			return nil, err
		}
	}
	roles, err := ownerRolesOf(t.ownerSet, func(string) bool { return true })
	if err != nil {
		return nil, fmt.Errorf("tenant %s has %w", id, err)
	}
	t.ownerRoles = roles
	if err := runtimekernel.DeclareOwned(id, apps, func(m platform.Manifest) bool { return len(m.Agents) > 0 }, t.Agents, "agent", func(r platform.AgentRuntime, a platform.App) error { return r.DeclareAgentApp(a) }); err != nil {
		return nil, err
	}
	if err := runtimekernel.DeclareOwned(id, apps, func(m platform.Manifest) bool { return len(m.Flows) > 0 }, t.Processes, "flow", func(r automation.FlowProcesses, a platform.App) error { return r.Declare(a) }); err != nil {
		return nil, err
	}
	if err := t.registerDefinitions(); err != nil {
		return nil, fmt.Errorf("tenant %s: %v", id, err)
	}
	{
		infos := map[string]platform.EntityInfo{}
		for _, et := range t.Projection.SortedTypes() {
			infos[et.Info().Type] = et.Info()
		}
		if err := platform.CheckInterfaceRegistry((hostView{t: t}).Interfaces(), infos); err != nil {
			return nil, fmt.Errorf("tenant %s: %v", id, err)
		}
	}
	for _, et := range t.Projection.SortedTypes() {
		if err := t.objectOwner(t.Projection).CheckRecordSetDeclarations(et.Info()); err != nil {
			return nil, fmt.Errorf("tenant %s object %s: %w", id, et.Info().Type, err)
		}
		if err := runtimeprotocol.CheckConstraints(et.Info(), t.Entity, true); err != nil {
			return nil, fmt.Errorf("tenant %s object %s: %w", id, et.Info().Type, err)
		}
	}
	if _, err := PublicationResolver().NewAssetRegistry(t.assetOwners(t.OwnerActive)...); err != nil {
		return nil, fmt.Errorf("tenant %s: %v", id, err)
	}
	if t.IdentityOwner != nil {
		if err := t.IdentityOwner.(platform.CompositionReady).Ready(); err != nil {
			return nil, fmt.Errorf("tenant %s owner %s readiness: %w", id, controlowner.PlatformApp, err)
		}
	}

	compatibility, err := t.snapshotCompatibility()
	if err != nil {
		return nil, fmt.Errorf("tenant %s snapshot composition: %w", id, err)
	}
	t.startupSnapshot = &compatibility
	return t, nil
}

func (t *Tenant) caller(m platform.Member, app platform.App, replaying bool) platform.Caller {
	return platform.NewCaller(runtime{t: t, scoped: true}, m, app.Manifest().ID, replaying, false)
}

func (t *Tenant) translator() transport.Translator {
	return transport.NewTranslator(func() []platform.App { return t.apps },
		func(a platform.App) platform.Manifest { return transport.ManifestOf(a, t.definitionsFingerprint()) },
		t.words.Clone, PlatformLanguages)
}
