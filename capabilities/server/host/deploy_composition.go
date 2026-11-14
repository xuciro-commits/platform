package host

import platform "platformserver/platform"

import (
	"context"
	"fmt"
	"maps"
	runtimekernel "platformserver/kernel"
	"platformserver/services/security"
	"reflect"
)

func (d *Deployment) providers() platform.RuntimeProviders {
	if d.RuntimeProviders != nil {
		return *d.RuntimeProviders
	}
	return platform.DefaultRuntimeProviders(security.Standard{})
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
