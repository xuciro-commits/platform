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

type resultIdentity = persistence.Identity

func OpenJournal(ctx context.Context, url string) (*persistence.Postgres, error) {
	j, err := persistence.OpenPostgres(ctx, url, runtimekernel.AcceptedIdentity)
	if err == nil {
		j.Delta = storageDelta
	}
	return j, err
}

func storageDelta(raw []byte) (persistence.StateDelta, error) {
	return runtimekernel.StorageDelta(raw, aistate.ApplyUsageState)
}

func OpenFileJournal(dir string) (*persistence.File, error) {
	return persistence.OpenFile(dir, runtimekernel.AcceptedIdentity)
}

var journalPool = persistence.Pool

var (
	errAcceptedConflict = persistence.ErrAcceptedConflict
	errTenantJournal    = persistence.ErrTenantJournal
	errTenantSnapshot   = persistence.ErrTenantSnapshot
)
