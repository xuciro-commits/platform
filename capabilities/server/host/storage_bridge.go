package host

import (
	"context"

	persistence "platformserver/kernel/store"
)

type (
	Entry      = persistence.Entry
	Transcript = persistence.Transcript
	Store      = persistence.Store
	Journals   = persistence.Journals
)

type resultIdentity = persistence.Identity

func OpenJournal(ctx context.Context, url string) (*persistence.Postgres, error) {
	j, err := persistence.OpenPostgres(ctx, url, acceptedIdentity)
	if err == nil {
		j.Delta = StorageDelta
	}
	return j, err
}

func OpenFileJournal(dir string) (*persistence.File, error) {
	return persistence.OpenFile(dir, acceptedIdentity)
}

var journalPool = persistence.Pool

var (
	errAcceptedConflict = persistence.ErrAcceptedConflict
	errTenantJournal    = persistence.ErrTenantJournal
	errTenantSnapshot   = persistence.ErrTenantSnapshot
)
