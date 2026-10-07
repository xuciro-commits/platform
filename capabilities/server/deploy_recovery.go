package platformserver

import (
	"context"
	"fmt"
	"log"
	"time"
)

func (t *Tenant) attachJournal(ctx context.Context, journal Journals) {
	t.Store = journal
	t.Record = func(e Entry) {
		if err := journal.Append(ctx, t.ID, e); err != nil {
			log.Fatalf("journal append: %v", err)
		}
	}
	id := t.ID
	t.AcceptResult = func(e Entry, key, hash string) ([]byte, error) {
		raw, err := journal.AppendAccepted(ctx, id, e, key, hash)
		if err != nil && err != errAcceptedConflict {
			log.Fatalf("accepted result append: %v", err)
		}
		return raw, err
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
	if d.Rebuild == nil {
		return fmt.Errorf("tenant %s has no recovery factory", id)
	}
	defer func() {
		if failure := recover(); failure != nil {
			err = fmt.Errorf("tenant %s recovery panicked: %v", id, failure)
		}
	}()
	fresh, err := d.Rebuild(id)
	if err != nil {
		return fmt.Errorf("compose tenant %s: %w", id, err)
	}
	if fresh == nil || fresh.ID != id || fresh.quarantined() {
		return fmt.Errorf("tenant %s recovery factory returned a different or damaged tenant", id)
	}
	candidate := registry.list()
	for i, t := range candidate {
		if t.ID == id {
			candidate[i] = fresh
		}
	}
	if CodeOf(candidate...) != code {
		return fmt.Errorf("tenant %s recovery factory has different app versions", id)
	}
	fresh.Files, fresh.Secrets, fresh.Outbound, fresh.AIClient = old.Files, old.Secrets, old.Outbound, old.AIClient
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
		if err := journal.RepairSnapshot(ctx, id, seq, code, state); err != nil {
			return fmt.Errorf("write repaired tenant %s checkpoint: %w", id, err)
		}
	}
	fresh.attachJournal(context.Background(), journal)
	if err := registry.replace(id, old, fresh); err != nil {
		return err
	}
	if projection != nil {
		flushProjection(context.Background(), registry, fresh, projection)
	}
	log.Printf("recovered tenant %s in place from %d journal entries", id, len(entries))
	return nil
}

func flushProjection(ctx context.Context, registry *tenantRegistry, tenant *Tenant, projection *Projection) {
	go func() {
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for range tick.C {
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
