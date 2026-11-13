package host

import records "platformserver/kernel/records"

import automation "platformserver/services/automate/runtime"

import (
	"fmt"
	"platformkernel/kernel"
	"platformserver/kernel/transport"
	"platformserver/services/ai/gateway"
	knowledgeindex "platformserver/services/ai/retrieval"
	"platformserver/services/data/blobs"
	"platformserver/services/data/transform"
	"platformserver/services/functions/runtime"
	"platformserver/services/resources/monitoring"

	runtimekernel "platformserver/kernel"
	"platformserver/services/devops/releases"
	"platformserver/services/resources/authoring"
	controlowner "platformserver/services/security/control"

	"platformserver/platform"
	"platformserver/services/actions"
	dataowner "platformserver/services/data"
	"platformserver/services/data/lineage"
	"sync"
	"sync/atomic"
	"time"
)

type Tenant struct {
	runtimekernel.Sequence
	agentEvaluator    platform.AgentEvaluator
	services          platform.RuntimeCapabilities
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
	observations      runtimekernel.ObservationBuffer
	runIndex          runtimekernel.RunIndexCache[[]monitoring.MonitoringCoverage]
	controlAnswer     func(platform.Effect, platform.Outcome, time.Time) *kernel.Error
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
	audit         runtimekernel.AuditLog
	events        []automation.QueuedEvent
	bindings      *actions.Bindings
	change        changes
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

func (t *Tenant) Audit() []platform.AuditEntry { return t.audit.All() }

func NewTenantWithCapabilities(id string, services platform.RuntimeCapabilities, apps ...platform.App) (*Tenant, error) {
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
	t := &Tenant{ID: id, ownerSet: owners, bindings: actions.NewBindings(nil), Works: kernel.NewWorks(), work: automation.NewWorkBoard(), index: &knowledgeindex.Index{},
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
	t.i18n = transport.NewTranslator(func() []platform.App { return t.apps },
		func(a platform.App) platform.Manifest { return transport.ManifestOf(a, t.definitionsFingerprint()) },
		t.words.Clone, PlatformLanguages)
	claim := func(name, owner string) error {
		if other := t.owner[name]; other != "" {
			return fmt.Errorf("tenant %s: %q is declared by %s and %s", id, name, other, owner)
		}
		t.owner[name] = owner
		return nil
	}
	for i, a := range apps {
		m := a.Manifest()
		if a.AcceptedLedger() == nil {
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
		var names []string
		for _, r := range m.Reads {
			names = append(names, "read:"+r)
		}
		for _, action := range m.Actions.All() {
			names = append(names, "action:"+action.Schema)
		}
		for input := range m.Inputs {
			names = append(names, "input:"+input)
		}
		for _, n := range names {
			if err := claim(n, m.ID); err != nil {
				return nil, err
			}
		}
	}
	roles, err := ownerRolesOf(t.ownerSet, func(string) bool { return true })
	if err != nil {
		return nil, fmt.Errorf("tenant %s has %w", id, err)
	}
	t.ownerRoles = roles
	for _, a := range apps {
		if len(a.Manifest().Agents) == 0 {
			continue
		}
		if t.agents == nil {
			return nil, fmt.Errorf("tenant %s: %s declares agents, and the tenant runs no agent app", id, a.Manifest().ID)
		}
		if err := t.agents.DeclareAgentApp(a); err != nil {
			return nil, fmt.Errorf("tenant %s: %v", id, err)
		}
	}
	for _, a := range apps {
		if len(a.Manifest().Flows) == 0 {
			continue
		}
		if t.procs == nil {
			return nil, fmt.Errorf("tenant %s: %s declares flows, and the tenant runs no flow app", id, a.Manifest().ID)
		}
		if err := t.procs.Declare(a); err != nil {
			return nil, fmt.Errorf("tenant %s: %v", id, err)
		}
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
		if err := t.checkRecordSetDeclarations(et.Info()); err != nil {
			return nil, fmt.Errorf("tenant %s object %s: %w", id, et.Info().Type, err)
		}
		if err := platform.CheckConstraints(et.Info(), t.Entity, true); err != nil {
			return nil, fmt.Errorf("tenant %s object %s: %w", id, et.Info().Type, err)
		}
	}
	if _, err := platform.NewAssetRegistry(t.assetOwners(t.OwnerActive)...); err != nil {
		return nil, fmt.Errorf("tenant %s: %v", id, err)
	}
	for _, app := range apps {
		if ready, ok := app.(platform.CompositionReady); ok {
			if err := ready.Ready(); err != nil {
				return nil, fmt.Errorf("tenant %s owner %s readiness: %w", id, app.Manifest().ID, err)
			}
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
