package host

import (
	"context"
	"fmt"
	"log"
	runtimekernel "platformserver/kernel"
	"platformserver/kernel/records"
	persistence "platformserver/kernel/store"
	"platformserver/platform"
	controlowner "platformserver/services/security/control"
	"reflect"
	"time"
)

func (t *Tenant) attachJournal(ctx context.Context, journal Journals) {
	t.RuntimePosition = func() int64 { return journal.Position(t.ID) }
	t.Store = journal
	t.audit.Backfill()
	id := t.ID
	t.AcceptResult = func(e Entry, key, hash string) ([]byte, error) {
		return journal.AppendAccepted(ctx, id, e, key, hash)
	}
}

func (d *Deployment) tenantRestore(ctx context.Context, t *Tenant, j Journals, code string, restored map[string]int64, fresh map[string]bool) runtimekernel.TenantRestore {
	return runtimekernel.TenantRestore{
		ID: t.ID, Code: code, SnapshotEvery: d.SnapshotEvery, Journal: j,
		Quarantine: t.quarantine,
		Fault:      func() string { return t.Fault.Load().Reason },
		AuditEmpty: func() bool {
			page, err := t.Log().QueryAuditEntries(platform.AuditQuery{Tenant: t.ID, Limit: 1})
			return err == nil && len(page.Entries) == 0
		},
		BackfillAudit:      t.backfillAudit,
		RuntimeSnapshotKey: t.runtimeSnapshotKey,
		SnapshotKey:        t.snapshotKey,
		CheckCheckpoint:    t.checkSnapshotCheckpoint,
		RestoreRuntimeSnapshot: func(state []byte, seq, position int64, started time.Time) error {
			return t.restoreRuntimeSnapshot(ctx, j, state, seq, position, started)
		},
		CheckpointRuntime: func() (int64, error) { return t.checkpointRuntime(ctx, j, code) },
		RecoverSnapshot:   t.recoverSnapshot,
		RecoverEntries:    t.recoverEntries,
		StateDelta: func() (persistence.StateDelta, error) {
			t.Lane.Lock()
			defer t.Lane.Unlock()
			return t.runtimeStateDeltaLocked()
		},
		CloseTokenSecretWindows: func() {
			if t.identityOwner != nil {
				t.identityOwner.CloseTokenSecretWindows()
			}
		},
		CheckProcesses: func() error {
			if t.procs == nil {
				return nil
			}
			return t.procs.Check()
		},
		RecoveryComplete: t.flowArtifactRecoveryComplete,
		Migrate: func(prefix *persistence.StateCheckpoint) error {
			return t.MigrateRuntimeState(ctx, j, prefix, t.runtimeOwners())
		},
		AttachJournal: func() { t.attachJournal(ctx, j) },
		SetRestored:   func(position int64) { restored[t.ID] = position },
		SetFresh:      func(value bool) { fresh[t.ID] = value },
	}
}

func (d *Deployment) retryTenant(ctx context.Context, journal Journals, registry *controlowner.Registry[*Tenant], code, id string) (err error) {
	old := registry.Current(id)
	if old == nil || !old.quarantined() {
		return fmt.Errorf("tenant %s is not quarantined", id)
	}
	if d.Rebuild == nil && d.RebuildWithCapabilities == nil {
		return fmt.Errorf("tenant %s has no recovery factory", id)
	}
	defer func() {
		if failure := recover(); failure != nil {
			err = fmt.Errorf("tenant %s recovery panicked: %v", id, failure)
		}
	}()
	recoveryStarted := time.Now().UTC()
	selection := old.desiredRuntime()
	if missingRuntime(selection) {
		return fmt.Errorf("tenant %s needs a compatible provider selection before recovery", id)
	}
	fresh, err := d.composeRuntime(id, selection)
	if err != nil {
		return fmt.Errorf("compose tenant %s: %w", id, err)
	}
	if fresh == nil || fresh.ID != id || fresh.quarantined() {
		return fmt.Errorf("tenant %s recovery factory returned a different or damaged tenant", id)
	}
	projection, entries, err := runtimekernel.TenantRepair{
		ID: id, Code: code, SnapshotEvery: d.SnapshotEvery, Journal: journal,
		Compatible: func() error {
			before, err := old.snapshotCompatibility()
			if err != nil {
				return fmt.Errorf("tenant %s original composition: %w", id, err)
			}
			after, err := fresh.snapshotCompatibility()
			if err != nil || !reflect.DeepEqual(before, after) || old.services.KernelModule.StateFormat != fresh.services.KernelModule.StateFormat {
				return fmt.Errorf("tenant %s recovery factory has different app composition or restore formats: %v", id, err)
			}
			return nil
		},
		Prepare: func() {
			fresh.tenantPorts = old.tenantPorts
			fresh.Store = journal
		},
		Recover: fresh.recoverEntries,
		CheckProcesses: func() error {
			if fresh.procs == nil {
				return nil
			}
			return fresh.procs.Check()
		},
		Project: func() (*records.SQLProjection, error) {
			if pgPool, pg := journalPool(journal); d.Project && pg {
				return records.ProjectSQL(ctx, pgPool, fresh.ID, fresh.Projection, fresh.readRecords)
			}
			return nil, nil
		},
		Capture:          fresh.Snapshot,
		SnapshotKey:      fresh.snapshotKey,
		RecoveryComplete: fresh.flowArtifactRecoveryComplete,
		Migrate:          func() error { return fresh.migrateRuntimeState(ctx, journal) },
		Attach:           func() { fresh.attachJournal(context.Background(), journal) },
		Deactivate:       func() error { return fresh.deactivateOwners(fresh.desiredDisabledOwners()) },
		Replace:          func() error { return registry.Replace(id, old, fresh) },
	}.Run(ctx, recoveryStarted)
	if err != nil {
		return err
	}
	if projection != nil {
		d.runProjection(registry, fresh, projection)
	}
	log.Printf("recovered tenant %s in place from %d journal entries", id, entries)
	return nil
}

func (d *Deployment) runProjection(registry *controlowner.Registry[*Tenant], tenant *Tenant, projection *records.SQLProjection) {
	if d.background == nil || d.runtimeContext == nil {
		return
	}
	ctx := d.runtimeContext
	runtimekernel.Every(ctx, d.background, time.Second, func() bool {
		if registry.Current(tenant.ID) != tenant {
			return false
		}
		if !tenant.quarantined() {
			if err := projection.Flush(ctx); err != nil {
				log.Printf("projection %s: %v", tenant.ID, err)
			}
		}
		return true
	})
}

func newTenantRegistry(tenants []*Tenant) *controlowner.Registry[*Tenant] {
	return controlowner.NewRegistry(tenants, func(t *Tenant) string { return t.ID }, func(t *Tenant) bool { return t.quarantined() })
}
