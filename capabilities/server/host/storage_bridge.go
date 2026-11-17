package host

import (
	"context"

	runtimekernel "platformserver/kernel"
	persistence "platformserver/kernel/store"
	aistate "platformserver/services/ai/state"
)

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
