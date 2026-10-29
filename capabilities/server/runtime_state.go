package platformserver

import (
	"context"
	"encoding/json"
	"fmt"

	runtimekernel "platformserver/kernel"

	"platformserver/kernel/records"

	"google.golang.org/protobuf/proto"

	"platformserver/journal"
	"platformserver/platform"
)

// migrateRuntimeState is called before serving this tenant. Its mutex pauses
// writes while the original in-memory state is copied and compared at one
// journal position. Only then are SQL-backed reads installed and history freed.
func (t *Tenant) migrateRuntimeState(ctx context.Context, j Journals) error {
	return t.migrateRuntimeStateFrom(ctx, j, nil)
}

func (t *Tenant) migrateRuntimeStateFrom(ctx context.Context, j Journals, prefix *journal.StateCheckpoint) error {
	store, ok := j.(journal.RuntimeState)
	if !ok {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	d, err := t.runtimeStateDeltaLocked()
	if err != nil {
		return err
	}
	if upgrade, ok := j.(journal.RuntimeStateUpgrade); ok {
		needed, upgradeErr := upgrade.StateNeedsUpgrade(ctx, t.ID)
		if upgradeErr != nil {
			return upgradeErr
		}
		if needed {
			position := j.Position(t.ID)
			if prefix != nil {
				position = prefix.Position
			}
			images, values, readErr := runtimekernel.LegacyStatePreimages(ctx, j, t.ID, position)
			if readErr != nil {
				return readErr
			}
			if prefix != nil {
				prefix.Delta.LegacyImages, prefix.Delta.LegacyValues = images, values
			} else {
				d.LegacyImages, d.LegacyValues = images, values
			}
		}
	}
	if prefix != nil {
		recovery, ok := j.(journal.RuntimeStateRecovery)
		if !ok {
			return fmt.Errorf("tenant %s state store cannot recover a retained suffix", t.ID)
		}
		if err := recovery.RestoreState(ctx, t.ID, j.Position(t.ID), *prefix, d); err != nil {
			return err
		}
	} else if err := store.InitializeState(ctx, t.ID, j.Position(t.ID), d); err != nil {
		return err
	}
	// The migration compared every original image and history at this position.
	// Keep SQL as the current-record source instead of decoding a second copy.
	t.installRuntimeRecords(store, func() int64 { return j.Position(t.ID) })
	t.installRuntimeHistory(store, func() int64 { return j.Position(t.ID) })
	t.enableCurrentReadScopes()
	return nil
}

// The snapshot archive and the current SQL view share the original history
// readers. During tail recovery the archive remains the fixed prefix while
// accepted application adds its original, privately held replacements.
func (t *Tenant) installRuntimeHistory(store runtimekernel.AcceptedStateReader, position func() int64) {
	for _, a := range t.apps {
		a.AcceptedLedger().UseChangeArchive(runtimekernel.NewChangeArchive(store, t.ID, a.Manifest().ID))
		if owner, ok := a.(platform.AcceptedHistoryApp); ok {
			id := a.Manifest().ID
			owner.UseAcceptedHistory(func() (json.RawMessage, error) {
				return store.StateValue(context.Background(), t.ID, "app-state", id, -1)
			})
		}
	}
	t.committed.UseStorage(store, t.ID, position)
}

// installRuntimeRecords is called only after migration or recovery established
// agreement with the durable SQL position. Fetched rows stay in the request's
// private work set rather than a second tenant-wide current-state authority.
func (t *Tenant) installRuntimeRecords(store journal.RuntimeState, position func() int64) {
	t.records.Lock()
	defer t.records.Unlock()
	t.records.Current().Configure(store, t.ID, position(), position)
	t.records.UseCurrentRows()
}

// The tenant lock fixes definitions and all owner state at the same replay position.
func (t *Tenant) runtimeStateDeltaLocked() (journal.StateDelta, error) {
	d := journal.StateDelta{}
	if err := func() error {
		t.records.Lock()
		defer t.records.Unlock()
		for typ, et := range t.records.Types() {
			for id, r := range et.Rows().All() {
				image, err := r.Image()
				if err != nil {
					return err
				}
				state := records.StoredState{Value: image, Format: r.Format, Projection: r.Projection, Extraction: r.Extraction}
				head, err := r.HistoryDigest()
				if err != nil {
					return err
				}
				rr := journal.StateRecord{Type: typ, ID: id, Image: jsonOf(state), HistoryBase: -1, HistoryHead: head}
				for _, h := range r.RecordHistory() {
					rr.History = append(rr.History, jsonOf(h))
				}
				d.Records = append(d.Records, rr)
				d.Unique = append(d.Unique, journal.StateUnique{Type: typ, ID: id, Keys: t.records.UniqueKeys(typ, id)})
			}
		}
		return nil
	}(); err != nil {
		return journal.StateDelta{}, err
	}
	for _, a := range t.apps {
		for index, r := range a.AcceptedLedger().RecordsFor(t.ID) {
			image, err := proto.MarshalOptions{Deterministic: true}.Marshal(r)
			if err != nil {
				return journal.StateDelta{}, err
			}
			d.Changes = append(d.Changes, journal.StateChange{App: a.Manifest().ID, ID: r.GetChangeId(), Key: r.GetSubmission().GetIdempotencyKey(), Index: index + 1, Image: image})
		}
	}
	for _, a := range t.apps {
		if owner, ok := a.(platform.AcceptedStateApp); ok {
			raw, err := owner.AcceptedState()
			if err != nil {
				return journal.StateDelta{}, err
			}
			d.Values = append(d.Values, journal.StateValue{Owner: "app-state", Key: a.Manifest().ID, Image: raw})
		}
	}
	t.committed.StorageValues(&d)
	for name, n := range t.sequences.Clone() {
		d.Values = append(d.Values, journal.StateValue{Owner: "sequence", Key: name, Image: jsonOf(n)})
	}
	return d, nil
}
