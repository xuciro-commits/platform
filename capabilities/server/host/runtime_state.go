package host

import (
	"context"
	"encoding/json"
	"fmt"

	runtimekernel "platformserver/kernel"

	"platformserver/kernel/records"

	"google.golang.org/protobuf/proto"

	persistence "platformserver/kernel/store"
	"platformserver/platform"
)

func (t *Tenant) migrateRuntimeState(ctx context.Context, j Journals) error {
	return t.migrateRuntimeStateFrom(ctx, j, nil)
}

func (t *Tenant) migrateRuntimeStateFrom(ctx context.Context, j Journals, prefix *persistence.StateCheckpoint) error {
	store, ok := j.(persistence.RuntimeState)
	if !ok {
		return nil
	}
	if err := t.restoreLegacySeeds(ctx, store); err != nil {
		return err
	}
	needsUpgrade := false
	if upgrade, ok := j.(persistence.RuntimeStateUpgrade); ok {
		var err error
		needsUpgrade, err = upgrade.StateNeedsUpgrade(ctx, t.ID)
		if err != nil {
			return err
		}
	}
	var view persistence.StateReadView
	if recovery, ok := j.(persistence.RuntimeStateRecovery); ok {
		position, active, err := recovery.StatePosition(ctx, t.ID)
		if err != nil {
			return err
		}
		if active && !needsUpgrade && prefix == nil && position == j.Position(t.ID) {
			t.installRuntimeRecords(store, func() int64 { return j.Position(t.ID) })
			t.installRuntimeHistory(store, func() int64 { return j.Position(t.ID) })
			t.enableCurrentReadScopes()
			return nil
		}
		if active {
			t.records.Lock()
			t.records.Current().Configure(store, t.ID, position, func() int64 { return j.Position(t.ID) })
			t.records.UseCurrentRows()
			t.records.Unlock()
			if views, ok := store.(persistence.StateReadViews); ok {
				view, err = views.BeginStateRead(ctx, t.ID, position)
				if err != nil {
					return err
				}
				t.records.Current().Bind(view)
				defer func() {
					t.records.Current().Bind(nil)
					_ = view.Close()
				}()
			}
		}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	d, err := t.runtimeStateDeltaLocked()
	if err != nil {
		return err
	}
	if needsUpgrade {
		position := j.Position(t.ID)
		if prefix != nil {
			position = prefix.Position
		}
		images, values, readErr := LegacyStatePreimages(ctx, j, t.ID, position)
		if readErr != nil {
			return readErr
		}
		if prefix != nil {
			prefix.Delta.LegacyImages, prefix.Delta.LegacyValues = images, values
		} else {
			d.LegacyImages, d.LegacyValues = images, values
		}
	}
	if prefix != nil {
		recovery, ok := j.(persistence.RuntimeStateRecovery)
		if !ok {
			return fmt.Errorf("tenant %s state store cannot recover a retained suffix", t.ID)
		}
		if err := recovery.RestoreState(ctx, t.ID, j.Position(t.ID), *prefix, d); err != nil {
			return err
		}
	} else if err := store.InitializeState(ctx, t.ID, j.Position(t.ID), d); err != nil {
		return err
	}
	t.installRuntimeRecords(store, func() int64 { return j.Position(t.ID) })
	t.installRuntimeHistory(store, func() int64 { return j.Position(t.ID) })
	t.enableCurrentReadScopes()
	return nil
}

func (t *Tenant) installRuntimeHistory(store runtimekernel.AcceptedStateReader, position func() int64) {
	ctx := context.Background()
	if t.records.Current().View() != nil {
		ctx = t.records.Current().Context()
	}
	for _, a := range t.apps {
		a.AcceptedLedger().UseChangeArchive(runtimekernel.NewChangeArchive(ctx, store, t.ID, a.Manifest().ID))
		if owner, ok := a.(platform.AcceptedHistoryApp); ok {
			id := a.Manifest().ID
			owner.UseAcceptedHistory(func() (json.RawMessage, error) {
				return store.StateValue(ctx, t.ID, "app-state", id, -1)
			})
		}
	}
	t.committed.UseStorage(ctx, store, t.ID, position)
}

func (t *Tenant) installRuntimeRecords(store persistence.RuntimeState, position func() int64) {
	t.records.Lock()
	defer t.records.Unlock()
	t.records.Current().Configure(store, t.ID, position(), position)
	t.records.UseCurrentRows()
}

func (t *Tenant) runtimeStateDeltaLocked() (persistence.StateDelta, error) {
	d := persistence.StateDelta{}
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
				rr := persistence.StateRecord{Type: typ, ID: id, Image: jsonOf(state), HistoryBase: -1, HistoryHead: head}
				for _, h := range r.RecordHistory() {
					rr.History = append(rr.History, jsonOf(h))
				}
				d.Records = append(d.Records, rr)
				d.Unique = append(d.Unique, persistence.StateUnique{Type: typ, ID: id, Keys: t.records.UniqueKeys(typ, id)})
			}
		}
		return nil
	}(); err != nil {
		return persistence.StateDelta{}, err
	}
	for _, a := range t.apps {
		for index, r := range a.AcceptedLedger().RecordsFor(t.ID) {
			image, err := proto.MarshalOptions{Deterministic: true}.Marshal(r)
			if err != nil {
				return persistence.StateDelta{}, err
			}
			d.Changes = append(d.Changes, persistence.StateChange{App: a.Manifest().ID, ID: r.GetChangeId(), Key: r.GetSubmission().GetIdempotencyKey(), Index: index + 1, Image: image})
		}
	}
	for _, a := range t.apps {
		if owner, ok := a.(platform.AcceptedStateApp); ok {
			raw, err := owner.AcceptedState()
			if err != nil {
				return persistence.StateDelta{}, err
			}
			d.Values = append(d.Values, persistence.StateValue{Owner: "app-state", Key: a.Manifest().ID, Image: raw})
		}
	}
	t.committed.StorageValues(&d)
	for name, n := range t.sequences.Clone() {
		d.Values = append(d.Values, persistence.StateValue{Owner: "sequence", Key: name, Image: jsonOf(n)})
	}
	return d, nil
}
