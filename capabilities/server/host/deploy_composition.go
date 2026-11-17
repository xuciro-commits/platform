package host

import (
	"context"
	"fmt"
	"maps"
	"path/filepath"

	runtimekernel "platformserver/kernel"
	runtimeprotocol "platformserver/kernel/protocol"
	persistence "platformserver/kernel/store"
	"platformserver/services/security"
	"reflect"
)

func (d *Deployment) providers() runtimeprotocol.RuntimeProviders {
	if d.RuntimeProviders == nil {
		return runtimeprotocol.DefaultRuntimeProviders(security.Standard{})
	}
	return *d.RuntimeProviders
}

func (d *Deployment) storageModule() runtimeprotocol.CapabilityModule {
	if d.Profile == "lightweight" || d.Database != "" || d.Storage != nil {
		return d.journalProvider().Module
	}
	return runtimeprotocol.MemoryStorageModule()
}

func PostgresProvider(address string) persistence.JournalProvider {
	return persistence.JournalProvider{Module: runtimeprotocol.CapabilityModule{ID: "platform.storage.postgres", Title: "PostgreSQL storage", Version: "1", Scope: "deployment", Replacement: "restart", StateFormat: "platform.sql-state.v1", Provides: []runtimeprotocol.CapabilityContract{{ID: "platform.storage", Version: "1", Semantics: "1"}}}, Durable: true, AtomicCurrentState: true, FixedReadViews: true, Open: func(ctx context.Context) (Journals, error) { return OpenJournal(ctx, address) }}
}

func FileProvider(directory string) persistence.JournalProvider {
	return persistence.JournalProvider{Module: runtimeprotocol.CapabilityModule{ID: "platform.storage.file", Title: "Local journal storage", Version: "1", Scope: "deployment", Replacement: "restart", StateFormat: "platform.file-persistence.v1", Provides: []runtimeprotocol.CapabilityContract{{ID: "platform.storage", Version: "1", Semantics: "1"}}}, Durable: true, Open: func(context.Context) (Journals, error) { return OpenFileJournal(directory) }}
}

func (d *Deployment) journalProvider() persistence.JournalProvider {
	if d.Storage != nil {
		return *d.Storage
	}
	if d.Profile == "lightweight" {
		return FileProvider(filepath.Join(d.Data, "journal"))
	}
	return PostgresProvider(d.Database)
}

func (d *Deployment) openJournals(ctx context.Context) (Journals, error) {
	return d.journalProvider().OpenChecked(ctx, d.Profile == "lightweight")
}

func (d *Deployment) composeRuntime(id string, selection map[string]string) (*Tenant, error) {
	services, err := d.providers().Resolve(selection)
	if err != nil {
		return nil, err
	}
	services.StorageModule = d.storageModule()
	var tenant *Tenant
	if d.RebuildWithCapabilities != nil {
		tenant, err = d.RebuildWithCapabilities(id, services)
	} else if d.Rebuild != nil {
		tenant, err = d.Rebuild(id)
	} else {
		return nil, fmt.Errorf("no tenant factory")
	}
	if err != nil {
		return nil, err
	}
	if tenant == nil || tenant.ID != id || !maps.Equal(tenant.services.Selection(), selection) {
		return nil, fmt.Errorf("tenant factory did not construct the selected runtime")
	}
	tenant.services.StorageModule = services.StorageModule
	d.bindPackageIndex(tenant)
	return tenant, nil
}

func (d *Deployment) restoreRuntimeSelections(ctx context.Context, journal Journals, tenants []*Tenant) error {
	for index, old := range tenants {
		selection := old.desiredRuntime()
		if old.quarantined() || missingRuntime(selection) {
			continue
		}
		var fresh *Tenant
		replaced, err := runtimekernel.SelectionRestore{
			ID: old.ID, Journal: journal, Selection: selection, Current: old.services.Selection(),
			Suspended: old.console.Suspended, Quarantine: old.quarantine,
			Deactivate: func() error { return old.deactivateOwners(old.desiredDisabledOwners()) },
			Compose: func() (runtimekernel.SelectionReplacement, error) {
				var err error
				if fresh, err = d.composeRuntime(old.ID, selection); err != nil {
					return runtimekernel.SelectionReplacement{}, err
				}
				return runtimekernel.SelectionReplacement{
					Compatible: func() bool {
						before, err := old.snapshotCompatibility()
						after, afterErr := fresh.snapshotCompatibility()
						return err == nil && afterErr == nil && reflect.DeepEqual(before, after) && old.services.KernelModule.StateFormat == fresh.services.KernelModule.StateFormat
					},
					Adopt:      func() { fresh.tenantPorts = old.tenantPorts },
					Recover:    fresh.recoverEntries,
					Migrate:    func() error { return fresh.MigrateRuntimeState(ctx, journal, nil, fresh.runtimeOwners()) },
					Attach:     func() { fresh.AttachJournal(ctx, journal) },
					Deactivate: func() error { return fresh.deactivateOwners(fresh.desiredDisabledOwners()) },
				}, nil
			},
		}.Run(ctx)
		if err != nil {
			return err
		}
		if replaced {
			tenants[index] = fresh
		}
	}
	return nil
}
