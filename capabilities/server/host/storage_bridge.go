package host

import (
	"context"

	runtimekernel "platformserver/kernel"
	persistence "platformserver/kernel/store"
	"platformserver/platform"
	aistate "platformserver/services/ai/state"
)

type (
	Entry      = persistence.Entry
	Transcript = persistence.Transcript
	Store      = persistence.Store
	Journals   = persistence.Journals
)

func acceptedIdentity(raw []byte) (persistence.Identity, error) {
	return runtimekernel.AcceptedIdentity(raw, platform.ReadCandidate)
}

func OpenJournal(ctx context.Context, url string) (*persistence.Postgres, error) {
	j, err := persistence.OpenPostgres(ctx, url, acceptedIdentity)
	if err == nil {
		j.Delta = storageDelta
	}
	return j, err
}

func storageDelta(raw []byte) (persistence.StateDelta, error) {
	return runtimekernel.StorageDelta(raw, aistate.ApplyUsageState, platform.ReadCandidate)
}

func OpenFileJournal(dir string) (*persistence.File, error) {
	return persistence.OpenFile(dir, acceptedIdentity)
}

var errAcceptedConflict = persistence.ErrAcceptedConflict
