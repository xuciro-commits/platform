package host

import (
	"context"
	runtimekernel "platformserver/kernel"
	persistence "platformserver/kernel/store"
	"platformserver/platform"
	aistate "platformserver/services/ai/state"
	"time"
)

func (t *Tenant) runtimeOwners() runtimekernel.RuntimeOwners {
	return runtimekernel.RuntimeOwners{Apps: t.apps, Sequences: t.Sequences.Clone, Ready: t.enableCurrentReadScopes,
		States: func(app string) platform.AcceptedStateApp { return t.states[app] }}
}

func (t *Tenant) installRuntimeHistory(store runtimekernel.AcceptedStateReader, position func() int64) {
	t.InstallRuntimeHistory(store, position, t.apps)
}

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

type (
	Entry      = persistence.Entry
	Transcript = persistence.Transcript
	Store      = persistence.Store
	Journals   = persistence.Journals
)

func acceptedIdentity(raw []byte) (persistence.Identity, error) {
	return runtimekernel.AcceptedIdentity(raw, PublicationResolver().ReadCandidate)
}

func OpenJournal(ctx context.Context, url string) (persistence.StorageBundle, error) {
	j, err := persistence.OpenPostgres(ctx, url, acceptedIdentity)
	if err != nil {
		return persistence.StorageBundle{}, err
	}
	j.Delta = storageDelta
	return persistence.PostgresBundle(j), nil
}

func storageDelta(raw []byte) (persistence.StateDelta, error) {
	return runtimekernel.StorageDelta(raw, aistate.ApplyUsageState, PublicationResolver().ReadCandidate)
}

func OpenFileJournal(dir string) (persistence.StorageBundle, error) {
	return persistence.OpenFileBundle(dir, acceptedIdentity)
}

var errAcceptedConflict = persistence.ErrAcceptedConflict
