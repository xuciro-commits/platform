package host

import (
	"context"
	"fmt"
	"maps"
	"slices"

	runtimekernel "platformserver/kernel"
	"platformserver/kernel/transport"
	"platformserver/platform"
	computeowner "platformserver/services/functions/runtime"
)

func (t *Tenant) forkReadState(ctx context.Context) (*Tenant, error) {
	control, err := t.captureControl()
	if err != nil {
		return nil, err
	}
	view, err := t.forkControl(control)
	if err != nil {
		return nil, err
	}
	view.readScope = &recordReadScope{ctx: ctx}
	view.readOrigin = t
	if t.readOrigin != nil {
		view.readOrigin = t.readOrigin
	}
	view.currentReadScopes = t.currentReadScopes
	view.records = t.records.ForkRecords()
	view.records.Current().Detach()
	view.definitions = slices.Clone(t.definitions)
	view.disabledOwners = maps.Clone(t.disabledOwners)
	view.apps = make([]platform.App, len(t.apps))
	view.owner = make(map[string]platform.App, len(t.owner))
	for i, app := range t.apps {
		owner, ok := app.(platform.ReadForkApp)
		if !ok {
			return nil, fmt.Errorf("owner %s has no read fork", app.Manifest().ID)
		}
		ledger, err := app.AcceptedLedger().ForkRead()
		if err != nil {
			return nil, err
		}
		fork, err := owner.ForkRead(ledger)
		if err != nil {
			return nil, err
		}
		view.apps[i] = fork
		if identity, ok := fork.(platform.IdentityDirectory); ok {
			view.identity = identity
		}
		if directory, ok := fork.(platform.Directory); ok {
			view.directory = directory
		}
		if linker, ok := fork.(platform.Linker); ok {
			view.linker = linker
		}
		if tasks, ok := fork.(platform.Tasks); ok {
			view.tasks = tasks
		}
		if processes, ok := fork.(platform.Processes); ok {
			view.procs = processes
		}
		if agents, ok := fork.(platform.AgentRuntime); ok {
			view.agents = agents
		}
		if model, ok := fork.(models); ok {
			view.ai = model
		}
		if knowledge, ok := fork.(platform.Glossary); ok {
			view.knowledge = knowledge
		}
	}
	for key, owner := range t.owner {
		view.owner[key] = view.app(owner.Manifest().ID)
	}
	for protocol, binding := range view.bindings {
		binding.provider = view.app(binding.provider.Manifest().ID)
		view.bindings[protocol] = binding
	}
	view.committed = t.committed.Fork()
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
	view.claimSchemas = t.claimSchemas
	view.claims = t.services.Kernel.Facts(t.claimSchemas)
	view.claims.Restore(t.ID, t.claims.Records(t.ID))
	view.entityIDs = t.services.Kernel.Identity(nil)
	view.entityIDs.Restore(t.entityIDs.State())
	view.systemDefinitions.Store(t.systemDefinitions.Load())
	view.systemDefinitionState.source.Store(t.systemDefinitionState.source.Load())
	view.systemDefinitionState.published.Store(t.systemDefinitionState.published.Load())
	view.systemDefinitionState.applied = t.systemDefinitionState.applied
	view.defGen.Store(t.defGen.Load())
	view.runIndexGen.Store(t.runIndexGen.Load())
	view.includeArchivedDefinitions = t.includeArchivedDefinitions
	view.i18n = transport.NewTranslator(func() []platform.App { return view.apps },
		func(app platform.App) platform.Manifest {
			return transport.ManifestOf(app, view.definitionsFingerprint())
		}, view.words.Clone, PlatformLanguages)
	view.i18n.InstallApplications(view.definitions)
	view.audit = t.audit.Fork()
	view.audit.SetSink(t.audit.Remember, t.audit.ReadPersonal)
	view.observationDropped.Store(t.observationDropped.Load())
	view.observationWritten.Store(t.observationWritten.Load())
	view.observationFailed.Store(t.observationFailed.Load())
	view.observationPurged.Store(t.observationPurged.Load())
	view.observationLastWrite.Store(t.observationLastWrite.Load())
	view.observationLastPurge.Store(t.observationLastPurge.Load())
	view.laneLastSlow.Store(t.laneLastSlow.Load())
	for _, app := range view.apps {
		(hostView{t: view, app: app}).bindOwner(app)
	}
	if backend, ok := view.records.Current().Backend().(runtimekernel.AcceptedStateReader); ok {
		view.installRuntimeHistory(backend, func() int64 { return view.records.Current().Applied() })
	}
	return view, nil
}
