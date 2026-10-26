package platformserver

import (
	"context"
	"fmt"
	"log"
	"reflect"
	"time"
)

func (t *Tenant) attachJournal(ctx context.Context, journal Journals) {
	t.runtimePosition = func() int64 { return journal.Position(t.ID) }
	t.Store = journal
	id := t.ID
	t.AcceptResult = func(e Entry, key, hash string) ([]byte, error) {
		return journal.AppendAccepted(ctx, id, e, key, hash)
	}
}

// retryTenant reconstructs all authority from the journal, not from the
// possibly half-applied in-memory tenant. A damaged snapshot can be bypassed
// because the full journal is retained; a damaged journal cannot be bypassed.
// No new generation is visible to HTTP, work or metrics until it is checked.
func (d *Deployment) retryTenant(ctx context.Context, journal Journals, registry *tenantRegistry, code, id string) (err error) {
	old := registry.current(id)
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
	before, err := old.snapshotCompatibility()
	if err != nil {
		return fmt.Errorf("tenant %s original composition: %w", id, err)
	}
	after, err := fresh.snapshotCompatibility()
	if err != nil || !reflect.DeepEqual(before, after) || old.services.KernelModule.StateFormat != fresh.services.KernelModule.StateFormat {
		return fmt.Errorf("tenant %s recovery factory has different app composition or restore formats: %v", id, err)
	}
	fresh.Files, fresh.Secrets, fresh.Outbound, fresh.AIClient = old.Files, old.Secrets, old.Outbound, old.AIClient
	fresh.systemDefinitionState.source.Store(old.systemDefinitionState.source.Load())
	entries, err := journal.Entries(ctx, id, 0)
	if err != nil {
		return fmt.Errorf("read tenant %s from the beginning: %w", id, err)
	}
	if err := fresh.recoverEntries(entries); err != nil {
		return fmt.Errorf("recover tenant %s: %w", id, err)
	}
	if fresh.procs != nil {
		if err := fresh.procs.Check(); err != nil {
			return fmt.Errorf("process recovery for tenant %s: %w", id, err)
		}
	}
	var projection *Projection
	if pgPool, pg := journalPool(journal); d.Project && pg {
		projection, err = Project(ctx, pgPool, fresh)
		if err != nil {
			return fmt.Errorf("rebuild tenant %s projection: %w", id, err)
		}
	}
	if d.SnapshotEvery > 0 {
		state, seq, err := fresh.Snapshot(func() int64 { return journal.Position(id) })
		if err != nil {
			return fmt.Errorf("capture repaired tenant %s: %w", id, err)
		}
		key, err := fresh.snapshotKey(code)
		if err != nil {
			return fmt.Errorf("snapshot format %s: %w", id, err)
		}
		if err := journal.RepairSnapshot(ctx, id, seq, key, state); err != nil {
			return fmt.Errorf("write repaired tenant %s checkpoint: %w", id, err)
		}
	}
	fresh.flowArtifactRecoveryComplete(recoveryStarted)
	if err := fresh.migrateRuntimeState(ctx, journal); err != nil {
		return fmt.Errorf("recover tenant %s runtime state: %w", id, err)
	}
	fresh.attachJournal(context.Background(), journal)
	if err := fresh.deactivateOwners(fresh.desiredDisabledOwners()); err != nil {
		return fmt.Errorf("restore tenant %s module selection: %w", id, err)
	}
	if err := registry.replace(id, old, fresh); err != nil {
		return err
	}
	(&Host{}).bindSystemDefinitions(registry.list())
	if projection != nil {
		d.runProjection(registry, fresh, projection)
	}
	log.Printf("recovered tenant %s in place from %d journal entries", id, len(entries))
	return nil
}

func (d *Deployment) runProjection(registry *tenantRegistry, tenant *Tenant, projection *Projection) {
	if d.background == nil || d.runtimeContext == nil {
		return
	}
	ctx, background := d.runtimeContext, d.background
	background.Add(1)
	go func() {
		defer background.Done()
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
			if registry.current(tenant.ID) != tenant {
				return
			}
			if tenant.quarantined() {
				continue
			}
			if err := projection.Flush(ctx); err != nil {
				log.Printf("projection %s: %v", tenant.ID, err)
			}
		}
	}()
}
