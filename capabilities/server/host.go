package platformserver

import records "platformserver/kernel/records"

import automation "platformserver/engine/automation"

import (
	"context"
	"fmt"
	"net/http"
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

// Delivery is one event handed to one subscriber.

// Tenant runs a tenant's apps: routing by name, one ordered journal, and calls
// between apps only through the protocols they provide and consume.
type Tenant struct {
	agentEvaluator        platform.AgentEvaluator
	services              platform.RuntimeCapabilities
	disabledOwners        map[string]bool
	systemDefinitions     atomic.Pointer[systemDefinitionProjection]
	systemDefinitionState systemDefinitionState
	installingSet         bool // private owner images validate cross-references as one complete set
	currentReadScopes     bool
	recordReadError       error
	startupSnapshot       *snapshotCompatibility
	runtimePosition       func() int64
	snapshotPosition      int64                        // last captured/restored checkpoint; zero-position diagnostics preserve it
	simulation            bool                         // isolated builder execution, never authoritative state
	simulationTrace       func(platform.DecisionTrace) // private trial observation; never snapshotted or installed live
	observationWindows    map[string]observationWindowAttempt
	confirmationReads     statusReadSchedule
	dispatchReads         statusReadSchedule
	// Private control views route callbacks back into their owning decision.
	controlAnswer func(platform.Effect, platform.Outcome, time.Time) *kernel.Error
	identity      platform.IdentityDirectory
	ID            string
	// Record is the legacy local recording callback. Durable deployments use
	// AcceptResult so storage failures can be returned before applying state.
	Record func(Entry)
	// AcceptResult persists a supported top-level decision or owned-work
	// attempt before any authoritative changes or intents are visible.
	AcceptResult func(Entry, string, string) ([]byte, error)
	// Store keeps what is derived outside the journal: vectors and transcripts;
	// without one they stay in memory.
	Store         Store
	memStore      journal.Memory       // the Store without one
	knowledge     platform.Glossary    // the knowledge app: documents are searched, terms read
	index         knowledgeindex.Index // passages cut from documents and knowledge fields
	i18n          transport.Translator // says declarations and messages in a language
	agentRun      string               // the run whose agent is submitting, under mu: its effects name it
	mu            runtimekernel.Lane
	views         transport.ViewCache         // catalog, entities and definitions per rights signature
	defGen        atomic.Uint64               // moves when the installed definitions do
	streamMu      sync.Mutex                  // one outside-lane source intake pass per tenant
	fault         atomic.Pointer[tenantFault] // recovery failure stops this tenant without stopping its neighbors
	apps          []platform.App
	appIndex      map[string]int          // immutable composition identity; private views replace owners in the same positions
	committed     runtimekernel.Committed // answers by idempotency key (kernel/tenant_state.go)
	releases      releases.Store          // studio/releases/release_retention.go
	console       controlowner.HostState  // the host console's lifecycle, support sessions, migrations (host_control.go)
	staged        computeowner.Channel    // the per-call result channel (compute_channel.go)
	definitions   []platform.Definition   // installed code assets; member views are derived on read
	owner         map[string]platform.App // "action:", "read:" and "input:" names → app
	flowArtifacts flowArtifactGC
	// audit holds accepted top-level inputs, newest last, rebuilt by replay; its
	// own lock, because reads run inside other apps' submissions.
	audit     runtimekernel.AuditLog   // recent inputs, deliveries, personal reads (kernel/tenant_diagnostics.go)
	events    []automation.QueuedEvent // published during the current input, queued after it
	bindings  map[string]binding
	observers []platform.Observer // the platform's views derived from events, inside the input
	change    changes             // what clients follow to refetch (F-32)
	linker    platform.Linker     // serves Caller.Link and Caller.Links
	directory platform.Directory  // the organisation, when composed
	hops      int                 // of the event being handled, for the events it causes
	acted     int                 // decisions published and notifications given, ever
	works     platform.Works
	// opsMu guards what reads and the runner share: queues, connectors,
	// notifications and settings (operations.go). It is never held while t.mu is taken.
	opsMu sync.Mutex
	work  automation.WorkBoard // queues, failed deliveries and jobs (work_board.go)
	// Quota is the attempts of owned work each app may make in a minute; 0: no limit.
	Quota      int
	quota      automation.Quota          // attempts per app per minute and the round-robin turn (quota.go)
	breakers   integrations.Breakers     // per endpoint and AI provider (volatile)
	connectors connectorRoster           // registry, descriptor index, last refusals (connector_roster.go)
	notices    runtimekernel.NoticeBoard // notifications (kernel/tenant_notifications.go)
	settings   controlowner.Settings     // "<app>/<name>" → value (settings.go)
	words      controlowner.Words        // the tenant's own translations (words.go)
	sequences  runtimekernel.Sequences   // number counters
	endpoints  []*Endpoint
	outbound   []*effect
	// Secrets resolves a secret's name (default: PLATFORM_SECRETS_DIR, then
	// PLATFORM_SECRET_<NAME>); Outbound sends an effect's request (default: a
	// client refusing private addresses). Tests replace both.
	Secrets  func(name string) ([]byte, bool)
	Outbound func(req *http.Request, allowPrivate bool) (*http.Response, error)
	// AIClient sends model calls (default: a client refusing private addresses
	// unless the provider is local); tests replace it.
	AIClient  func(req *http.Request) (*http.Response, error)
	ai        models                // the AI app: the models the host calls
	records   *records.Store        // the apps' entity records
	tasks     platform.Tasks        // serves Caller.Assign: the work app
	procs     platform.Processes    // the flow app
	listeners []string              // platform apps given other apps' events as owned work (platform.Listener)
	agents    platform.AgentRuntime // AI agents
	ctx       context.Context       // the span of the work being done under mu (telemetry.go)
	// Files keeps file bytes; nil: memory, for development and tests.
	Files         platform.FileStore
	ComputeWorker platform.WasmWorker
	Compiler      platform.CodeCompiler
	cancels       computeowner.Cancels // running operations' cancel handles
	memFiles      resources.MemoryFiles
	uploads       resources.Uploads // hashes uploaded and not yet attached (uploads.go)
}

// AuditEntry is one accepted input: who, when, through which app, what.

// Audit is the tenant's recent accepted inputs, oldest first.
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
		// Roles the host's own apps take, by the interfaces they implement.
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
	for _, a := range apps { // agents, once every app is composed; flows may give them steps
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
	for _, a := range apps { // flows, once every app is composed
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
	{ // interfaces, once every entity is described (A2)
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
