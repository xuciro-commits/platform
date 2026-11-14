package host

import (
	"context"
	"platformserver/platform"
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
		Complete: func() error { return t.recoveryComplete(started) },
		Attach: func() {
			t.AttachJournal(ctx, j)
			t.enableCurrentReadScopes()
		},
	})
}
