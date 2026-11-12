package kernel

import (
	"context"
	"fmt"

	"google.golang.org/protobuf/proto"
	"platformserver/kernel/records"
	persistence "platformserver/kernel/store"
	"platformserver/platform"
)

type RuntimeOwners struct {
	Apps      []platform.App
	States    func(string) platform.AcceptedStateApp
	Sequences func() map[string]int
	Ready     func()
}

func (s *Sequence) MigrateRuntimeState(ctx context.Context, j persistence.Journals, prefix *persistence.StateCheckpoint, o RuntimeOwners) error {
	store, ok := j.(persistence.RuntimeState)
	if !ok {
		return nil
	}
	var view persistence.StateReadView
	if recovery, ok := j.(persistence.RuntimeStateRecovery); ok {
		position, active, err := recovery.StatePosition(ctx, s.ID)
		if err != nil {
			return err
		}
		if active && prefix == nil && position == j.Position(s.ID) {
			s.InstallRuntimeRecords(store, func() int64 { return j.Position(s.ID) })
			s.InstallRuntimeHistory(store, func() int64 { return j.Position(s.ID) }, o.Apps)
			o.Ready()
			return nil
		}
		if active {
			s.Projection.Lock()
			s.Projection.Current().Configure(store, s.ID, position, func() int64 { return j.Position(s.ID) })
			s.Projection.UseCurrentRows()
			s.Projection.Unlock()
			if views, ok := store.(persistence.StateReadViews); ok {
				view, err = views.BeginStateRead(ctx, s.ID, position)
				if err != nil {
					return err
				}
				s.Projection.Current().Bind(view)
				defer func() {
					s.Projection.Current().Bind(nil)
					_ = view.Close()
				}()
			}
		}
	}
	s.Lane.Lock()
	defer s.Lane.Unlock()
	d, err := s.RuntimeStateDelta(o)
	if err != nil {
		return err
	}
	if prefix != nil {
		recovery, ok := j.(persistence.RuntimeStateRecovery)
		if !ok {
			return fmt.Errorf("tenant %s state store cannot recover a retained suffix", s.ID)
		}
		if err := recovery.RestoreState(ctx, s.ID, j.Position(s.ID), *prefix, d); err != nil {
			return err
		}
	} else if err := store.InitializeState(ctx, s.ID, j.Position(s.ID), d); err != nil {
		return err
	}
	s.InstallRuntimeRecords(store, func() int64 { return j.Position(s.ID) })
	s.InstallRuntimeHistory(store, func() int64 { return j.Position(s.ID) }, o.Apps)
	o.Ready()
	return nil
}

func (s *Sequence) InstallRuntimeHistory(store AcceptedStateReader, position func() int64, apps []platform.App) {
	ctx := context.Background()
	if s.Projection.Current().View() != nil {
		ctx = s.Projection.Current().Context()
	}
	for _, a := range apps {
		a.AcceptedLedger().UseChangeArchive(NewChangeArchive(ctx, store, s.ID, a.Manifest().ID))
	}
	s.Committed.UseStorage(ctx, store, s.ID, position)
}

func (s *Sequence) InstallRuntimeRecords(store persistence.RuntimeState, position func() int64) {
	s.Projection.Lock()
	defer s.Projection.Unlock()
	s.Projection.Current().Configure(store, s.ID, position(), position)
	s.Projection.UseCurrentRows()
}

func (s *Sequence) RuntimeStateDelta(o RuntimeOwners) (persistence.StateDelta, error) {
	d := persistence.StateDelta{}
	if err := func() error {
		s.Projection.Lock()
		defer s.Projection.Unlock()
		for typ, et := range s.Projection.Types() {
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
				rr := persistence.StateRecord{Type: typ, ID: id, Image: storedJSON(state), HistoryBase: -1, HistoryHead: head}
				for _, h := range r.RecordHistory() {
					rr.History = append(rr.History, storedJSON(h))
				}
				d.Records = append(d.Records, rr)
				d.Unique = append(d.Unique, persistence.StateUnique{Type: typ, ID: id, Keys: s.Projection.UniqueKeys(typ, id)})
			}
		}
		return nil
	}(); err != nil {
		return persistence.StateDelta{}, err
	}
	for _, a := range o.Apps {
		for index, r := range a.AcceptedLedger().RecordsFor(s.ID) {
			image, err := proto.MarshalOptions{Deterministic: true}.Marshal(r)
			if err != nil {
				return persistence.StateDelta{}, err
			}
			d.Changes = append(d.Changes, persistence.StateChange{App: a.Manifest().ID, ID: r.GetChangeId(), Key: r.GetSubmission().GetIdempotencyKey(), Index: index + 1, Image: image})
		}
	}
	for _, a := range o.Apps {
		if owner := o.States(a.Manifest().ID); owner != nil {
			raw, err := owner.AcceptedState()
			if err != nil {
				return persistence.StateDelta{}, err
			}
			d.Values = append(d.Values, persistence.StateValue{Owner: "app-state", Key: a.Manifest().ID, Image: raw})
		}
	}
	s.Committed.StorageValues(&d)
	for name, n := range o.Sequences() {
		d.Values = append(d.Values, persistence.StateValue{Owner: "sequence", Key: name, Image: storedJSON(n)})
	}
	return d, nil
}
