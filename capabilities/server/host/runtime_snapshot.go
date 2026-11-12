package host

import runtimekernel "platformserver/kernel"

import (
	"context"
	"fmt"
	"platformserver/platform"
	controlowner "platformserver/services/security/control"
	"time"

	persistence "platformserver/kernel/store"
)

func (t *Tenant) restoreRuntimeSnapshot(ctx context.Context, j Journals, state []byte, seq, current int64, started time.Time) error {
	saved, err := runtimekernel.ReadCheckpoint(state)
	if err != nil {
		return err
	}
	if saved == nil || saved.Format != runtimekernel.SQLCheckpointFormat || saved.Position != seq || seq > current {
		return fmt.Errorf("tenant %s: SQL checkpoint format or position differs", t.ID)
	}
	snapshots, ok := j.(persistence.StateSnapshotStorage)
	if !ok {
		return fmt.Errorf("tenant %s: SQL checkpoint archive unavailable", t.ID)
	}
	store, ok := j.(persistence.RuntimeState)
	if !ok {
		return fmt.Errorf("tenant %s: SQL state unavailable", t.ID)
	}
	view, err := snapshots.BeginSnapshotStateRead(ctx, t.ID, seq, current)
	if err != nil {
		return err
	}
	defer view.Close()
	digest, err := view.Checkpoint(ctx, t.ID)
	if err != nil {
		return err
	}
	if digest != saved.Records {
		return fmt.Errorf("tenant %s: SQL checkpoint records differ", t.ID)
	}
	t.InstallRuntimeRecords(store, func() int64 { return seq })
	t.Projection.Current().Bind(view)
	defer t.Projection.Current().Bind(nil)
	t.installRuntimeHistory(view, func() int64 { return seq })
	if err = t.recoverSnapshot(state, seq); err != nil {
		return err
	}
	t.installRuntimeHistory(view, func() int64 { return seq })
	entries, err := j.Entries(ctx, t.ID, seq)
	if err != nil {
		return err
	}
	if err = t.recoverEntries(entries); err != nil {
		return err
	}
	if err = t.VerifyRuntimeTail(ctx, j, seq, current, entries, storageDelta, func(app string) platform.AcceptedStateApp { return t.states[app] }); err != nil {
		return err
	}
	if console, ok := t.App(controlowner.PlatformApp).(platform.IdentityRecovery); ok {
		console.CloseTokenSecretWindows()
	}
	if t.procs != nil {
		if err = t.procs.Check(); err != nil {
			return fmt.Errorf("process recovery: %w", err)
		}
	}
	t.flowArtifactRecoveryComplete(started)
	t.InstallRuntimeRecords(store, func() int64 { return j.Position(t.ID) })
	t.installRuntimeHistory(store, func() int64 { return j.Position(t.ID) })
	t.attachJournal(ctx, j)
	t.enableCurrentReadScopes()
	return nil
}

func (t *Tenant) checkpointRuntime(ctx context.Context, j Journals, code string) (int64, error) {
	state, seq, err := t.snapshotRuntime(ctx, j)
	if err != nil {
		return 0, err
	}
	key, err := t.snapshotStorageKey(code, state)
	if err != nil {
		return 0, err
	}
	if err = runtimekernel.SaveCheckpoint(ctx, j, t.ID, seq, key, state); err != nil {
		return 0, err
	}
	return seq, nil
}
