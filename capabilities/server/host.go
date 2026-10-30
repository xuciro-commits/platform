package platformserver

import records "platformserver/kernel/records"

import automation "platformserver/engine/automation"

import (
	"context"
	"fmt"
	"net/http"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	computeowner "platformserver/engine/compute"
	"platformserver/engine/integrations"
	knowledgeindex "platformserver/engine/knowledge"
	"platformserver/engine/resources"
	"platformserver/kernel/transport"

	runtimekernel "platformserver/kernel"
	controlowner "platformserver/kernel/control"
	"platformserver/studio/build"
	"platformserver/studio/releases"

	"platformserver/journal"
	"platformserver/platform"
	"sync"
	"sync/atomic"
	"time"
)

type Tenant struct {
	agentEvaluator        platform.AgentEvaluator
	services              platform.RuntimeCapabilities
	disabledOwners        map[string]bool
	systemDefinitions     atomic.Pointer[systemDefinitionProjection]
	systemDefinitionState systemDefinitionState
	installingSet         bool
	currentReadScopes     bool
	recordReadError       error
	startupSnapshot       *snapshotCompatibility
	runtimePosition       func() int64
	snapshotPosition      int64
	simulation            bool
	simulationTrace       func(platform.DecisionTrace)
	observationWindows    map[string]observationWindowAttempt
	confirmationReads     statusReadSchedule
	dispatchReads         statusReadSchedule
	controlAnswer         func(platform.Effect, platform.Outcome, time.Time) *kernel.Error
	identity              platform.IdentityDirectory
	claims                platform.Facts
	claimSchemas          platform.SchemaRegistry
	entityIDs             platform.Identity
	ID                    string
	Record                func(Entry)
	AcceptResult          func(Entry, string, string) ([]byte, error)
	Store                 Store
	memStore              journal.Memory
	knowledge             platform.Glossary
	index                 knowledgeindex.Index
	i18n                  transport.Translator
	agentRun              string
	mu                    runtimekernel.Lane
	views                 transport.ViewCache
	defGen                atomic.Uint64
	streamMu              sync.Mutex
	fault                 atomic.Pointer[tenantFault]
	apps                  []platform.App
	appIndex              map[string]int
	committed             runtimekernel.Committed
	releases              releases.Store
	console               controlowner.HostState
	staged                computeowner.Channel
	definitions           []platform.Definition
	owner                 map[string]platform.App
	flowArtifacts         flowArtifactGC
	audit                 runtimekernel.AuditLog
	events                []automation.QueuedEvent
	bindings              map[string]binding
	observers             []platform.Observer
	change                changes
	linker                platform.Linker
	directory             platform.Directory
	hops                  int
	acted                 int
	works                 platform.Works
	opsMu                 sync.Mutex
	work                  automation.WorkBoard
	Quota                 int
	quota                 automation.Quota
	breakers              integrations.Breakers
	connectors            connectorRoster
	notices               runtimekernel.NoticeBoard
	settings              controlowner.Settings
	words                 controlowner.Words
	sequences             runtimekernel.Sequences
	endpoints             []*Endpoint
	outbound              []*effect
	Secrets               func(name string) ([]byte, bool)
	Outbound              func(req *http.Request, allowPrivate bool) (*http.Response, error)
	AIClient              func(req *http.Request) (*http.Response, error)
	ai                    models
	records               *records.Store
	tasks                 platform.Tasks
	procs                 platform.Processes
	listeners             []string
	agents                platform.AgentRuntime
	ctx                   context.Context
	Files                 platform.FileStore
	ComputeWorker         platform.WasmWorker
	Compiler              platform.CodeCompiler
	cancels               computeowner.Cancels
	memFiles              resources.MemoryFiles
	uploads               resources.Uploads
}

func (t *Tenant) Audit() []platform.AuditEntry { return t.audit.All() }

func NewTenantWithCapabilities(id string, services platform.RuntimeCapabilities, apps ...platform.App) (*Tenant, error) {
	if services.BindOwner == nil {
		return nil, fmt.Errorf("tenant composition requires owner bindings")
	}
	if err := bindAIInferencePolicy(&services); err != nil {
		return nil, err
	}
	if err := services.Check(); err != nil {
		return nil, err
	}
	t := &Tenant{ID: id, apps: apps, appIndex: map[string]int{}, owner: map[string]platform.App{}, bindings: map[string]binding{}, works: services.Kernel.Works(), work: automation.NewWorkBoard(),
		connectors: newConnectorRoster(services.Kernel), records: records.NewStore()}
	t.mu.BindReadScope(t.beginRecordRead, t.closeRecordRead)
	for i, app := range apps {
		id := app.Manifest().ID
		if _, exists := t.appIndex[id]; exists || id == "" {
			return nil, fmt.Errorf("tenant %s: owner identity %q is empty or repeated", t.ID, id)
		}
		t.appIndex[id] = i
	}
	t.services = services
	t.claimSchemas = services.Kernel.Schemas([]*pb.SchemaRef{{Name: "integration.claim", Version: 1}})
	t.claims = services.Kernel.Facts(t.claimSchemas)
	t.entityIDs = services.Kernel.Identity(nil)
	if services.NewAgentEvaluator != nil {
		t.agentEvaluator = services.NewAgentEvaluator()
	}
	for _, app := range apps {
		if owner, ok := app.(interface{ AcceptedLedger() *platform.Ledger }); ok {
			if err := owner.AcceptedLedger().BindKernel(services.Kernel); err != nil {
				return nil, fmt.Errorf("bind kernel for %s: %w", app.Manifest().ID, err)
			}
		}
	}
	t.records.SetRules(services.Rules)
	t.staged = computeowner.NewChannel(id, t.files)
	t.i18n = transport.NewTranslator(func() []platform.App { return t.apps },
		func(a platform.App) platform.Manifest { return transport.ManifestOf(a, t.definitionsFingerprint()) },
		t.words.Clone, PlatformLanguages)
	claim := func(name string, a platform.App) error {
		if other := t.owner[name]; other != nil {
			return fmt.Errorf("tenant %s: %q is declared by %s and %s", id, name, other.Manifest().ID, a.Manifest().ID)
		}
		t.owner[name] = a
		return nil
	}
	for i, a := range apps {
		m := a.Manifest()
		if a.AcceptedLedger() == nil {
			return nil, fmt.Errorf("tenant %s: app %s has no decision ledger", id, m.ID)
		}
		if err := t.bind(i, a); err != nil {
			return nil, err
		}
		(hostView{t: t, app: a}).bindOwner(a)
		if x, ok := a.(platform.Observer); ok {
			t.observers = append(t.observers, x)
		}
		if x, ok := a.(platform.Linker); ok {
			if t.linker != nil {
				return nil, fmt.Errorf("tenant %s has two link owners", id)
			}
			t.linker = x
		}
		if x, ok := a.(platform.Directory); ok {
			if t.directory != nil {
				return nil, fmt.Errorf("tenant %s has two directory owners", id)
			}
			t.directory = x
		}
		if identity, ok := a.(platform.IdentityDirectory); ok {
			if t.identity != nil {
				return nil, fmt.Errorf("tenant %s has two identity owners", id)
			}
			t.identity = identity
		}
		if x, ok := a.(models); ok {
			if t.ai != nil {
				return nil, fmt.Errorf("tenant %s has two model owners", id)
			}
			t.ai = x
		}
		if x, ok := a.(platform.Tasks); ok {
			if t.tasks != nil {
				return nil, fmt.Errorf("tenant %s has two task owners", id)
			}
			t.tasks = x
		}
		if x, ok := a.(platform.Processes); ok {
			if t.procs != nil {
				return nil, fmt.Errorf("tenant %s has two process owners", id)
			}
			t.procs = x
		}
		if _, ok := a.(platform.Listener); ok {
			t.listeners = append(t.listeners, m.ID)
		}
		if x, ok := a.(platform.AgentRuntime); ok {
			if t.agents != nil {
				return nil, fmt.Errorf("tenant %s has two agent owners", id)
			}
			t.agents = x
		}
		if x, ok := a.(platform.Glossary); ok {
			if t.knowledge != nil {
				return nil, fmt.Errorf("tenant %s has two glossary owners", id)
			}
			t.knowledge = x
		}
		_, subscriber := a.(platform.Subscriber)
		if err := build.CheckSubscriptions(m, subscriber); err != nil {
			return nil, fmt.Errorf("tenant %s: %w", id, err)
		}
		_, runsJobs := a.(platform.Runner)
		if err := build.CheckManifest(a.Manifest(), runsJobs); err != nil {
			return nil, fmt.Errorf("tenant %s: %s: %v", id, m.ID, err)
		}
		if err := t.records.Declare(a); err != nil {
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
			if err := claim(n, a); err != nil {
				return nil, err
			}
		}
	}
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
	{
		manifests := make([]platform.Manifest, len(apps))
		infos := map[string]platform.EntityInfo{}
		for k, a := range apps {
			manifests[k] = a.Manifest()
		}
		for _, et := range t.records.SortedTypes() {
			infos[et.Info().Type] = et.Info()
		}
		if err := platform.CheckInterfaces(manifests, infos); err != nil {
			return nil, fmt.Errorf("tenant %s: %v", id, err)
		}
	}
	if err := t.registerDefinitions(); err != nil {
		return nil, fmt.Errorf("tenant %s: %v", id, err)
	}
	for _, et := range t.records.SortedTypes() {
		if err := t.checkRecordSetDeclarations(et.Info()); err != nil {
			return nil, fmt.Errorf("tenant %s object %s: %w", id, et.Info().Type, err)
		}
		if err := platform.CheckConstraints(et.Info(), t.entity, true); err != nil {
			return nil, fmt.Errorf("tenant %s object %s: %w", id, et.Info().Type, err)
		}
	}
	var assetOwners []platform.AssetLifecycleOwner
	for _, app := range apps {
		if owner, ok := app.(platform.AssetLifecycleOwner); ok {
			assetOwners = append(assetOwners, owner)
		}
	}
	if _, err := platform.NewAssetRegistry(assetOwners...); err != nil {
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

func (t *Tenant) app(id string) platform.App {
	i, found := t.appIndex[id]
	if !found {
		return nil
	}
	return t.apps[i]
}

func (t *Tenant) caller(m platform.Member, app platform.App, replaying bool) platform.Caller {
	return platform.NewCaller(runtime{t}, m, app.Manifest().ID, replaying, false)
}
