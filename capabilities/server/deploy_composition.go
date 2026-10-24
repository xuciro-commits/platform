package platformserver

import (
	"context"
	"fmt"
	"maps"
	"reflect"
)

func (d *Deployment) providers() RuntimeProviders {
	if d.RuntimeProviders != nil {
		return *d.RuntimeProviders
	}
	return DefaultRuntimeProviders()
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

// Provider changes activate at process startup, after all old writers and
// resources have stopped. The original journal stays the only state authority.
func (d *Deployment) restoreRuntimeSelections(ctx context.Context, journal Journals, code string, tenants []*Tenant) error {
	for index, old := range tenants {
		if old.quarantined() {
			continue
		}
		selection := old.desiredRuntime()
		if missingRuntime(selection) {
			continue
		}
		if maps.Equal(selection, old.services.Selection()) {
			if err := old.deactivateOwners(old.desiredDisabledOwners()); err != nil {
				old.quarantine(err)
			}
			continue
		}
		if !old.console.suspended() {
			old.quarantine(fmt.Errorf("provider change requires maintenance"))
			continue
		}
		fresh, err := d.composeRuntime(old.ID, selection)
		if err != nil {
			old.quarantine(err)
			continue
		}
		before, err := old.snapshotCompatibility()
		after, afterErr := fresh.snapshotCompatibility()
		if err != nil || afterErr != nil || !reflect.DeepEqual(before, after) || old.services.KernelModule.StateFormat != fresh.services.KernelModule.StateFormat {
			old.quarantine(fmt.Errorf("replacement has incompatible owner or kernel restore formats"))
			continue
		}
		fresh.Files, fresh.Secrets, fresh.Outbound, fresh.AIClient = old.Files, old.Secrets, old.Outbound, old.AIClient
		entries, err := journal.Entries(ctx, old.ID, 0)
		if err != nil {
			return err
		}
		if err = fresh.recoverEntries(entries); err != nil {
			old.quarantine(err)
			continue
		}
		if err = fresh.migrateRuntimeState(ctx, journal); err != nil {
			old.quarantine(err)
			continue
		}
		fresh.attachJournal(ctx, journal)
		if err := fresh.deactivateOwners(fresh.desiredDisabledOwners()); err != nil {
			old.quarantine(err)
			continue
		}
		tenants[index] = fresh
	}
	return nil
}
