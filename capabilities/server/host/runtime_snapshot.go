package host

import (
	"context"
	"fmt"
	"platformserver/platform"
	controlowner "platformserver/services/security/control"
	"time"

	runtimekernel "platformserver/kernel"
)

func (t *Tenant) restoreRuntimeSnapshot(ctx context.Context, j Journals, state []byte, seq, current int64, started time.Time) error {
	return t.RestoreRuntimeSnapshot(ctx, j, state, seq, current, runtimekernel.RuntimeRecovery{
		History:  t.installRuntimeHistory,
		Snapshot: t.recoverSnapshot,
		Entries:  t.recoverEntries,
		Delta:    storageDelta,
		States:   func(app string) platform.AcceptedStateApp { return t.states[app] },
		Complete: func() error {
			if console, ok := t.App(controlowner.PlatformApp).(platform.IdentityRecovery); ok {
				console.CloseTokenSecretWindows()
			}
			if t.procs != nil {
				if err := t.procs.Check(); err != nil {
					return fmt.Errorf("process recovery: %w", err)
				}
			}
			t.flowArtifactRecoveryComplete(started)
			return nil
		},
		Attach: func() {
			t.attachJournal(ctx, j)
			t.enableCurrentReadScopes()
		},
	})
}

func (t *Tenant) checkpointRuntime(ctx context.Context, j Journals, code string) (int64, error) {
	return t.snapshots().Checkpoint(&t.Sequence, ctx, j, code)
}
