package kernel

import (
	"context"
	"fmt"

	persistence "platformserver/kernel/store"
	"platformserver/platform"
)

type RuntimeRecovery struct {
	History  func(reader AcceptedStateReader, position func() int64)
	Snapshot func(state []byte, seq int64) error
	Entries  func([]persistence.Entry) error
	Delta    func([]byte) (persistence.StateDelta, error)
	States   func(app string) platform.AcceptedStateApp
	Complete func() error
	Attach   func()
}

func (s *Sequence) RestoreRuntimeSnapshot(ctx context.Context, j persistence.Journals, state []byte, seq, current int64, r RuntimeRecovery) error {
	saved, err := ReadCheckpoint(state)
	if err != nil {
		return err
	}
	if saved == nil || saved.Format != SQLCheckpointFormat || saved.Position != seq || seq > current {
		return fmt.Errorf("tenant %s: SQL checkpoint format or position differs", s.ID)
	}
	snapshots, ok := j.(persistence.StateSnapshotStorage)
	if !ok {
		return fmt.Errorf("tenant %s: SQL checkpoint archive unavailable", s.ID)
	}
	store, ok := j.(persistence.RuntimeState)
	if !ok {
		return fmt.Errorf("tenant %s: SQL state unavailable", s.ID)
	}
	view, err := snapshots.BeginSnapshotStateRead(ctx, s.ID, seq, current)
	if err != nil {
		return err
	}
	defer view.Close()
	digest, err := view.Checkpoint(ctx, s.ID)
	if err != nil {
		return err
	}
	if digest != saved.Records {
		return fmt.Errorf("tenant %s: SQL checkpoint records differ", s.ID)
	}
	s.InstallRuntimeRecords(store, func() int64 { return seq })
	s.Projection.Current().Bind(view)
	defer s.Projection.Current().Bind(nil)
	r.History(view, func() int64 { return seq })
	if err = r.Snapshot(state, seq); err != nil {
		return err
	}
	r.History(view, func() int64 { return seq })
	entries, err := j.Entries(ctx, s.ID, seq)
	if err != nil {
		return err
	}
	if err = r.Entries(entries); err != nil {
		return err
	}
	if err = s.VerifyRuntimeTail(ctx, j, seq, current, entries, r.Delta, r.States); err != nil {
		return err
	}
	if err = r.Complete(); err != nil {
		return err
	}
	s.InstallRuntimeRecords(store, func() int64 { return j.Position(s.ID) })
	r.History(store, func() int64 { return j.Position(s.ID) })
	r.Attach()
	return nil
}
