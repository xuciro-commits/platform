package platformserver

import runtimekernel "platformserver/kernel"

import (
	"context"

	"platformserver/journal"
)

type (
	Entry      = journal.Entry
	Transcript = journal.Transcript
	Store      = journal.Store
	Journals   = journal.Journals
)

type resultIdentity = journal.Identity

func OpenJournal(ctx context.Context, url string) (*journal.Postgres, error) {
	j, err := journal.OpenPostgres(ctx, url, acceptedIdentity)
	if err == nil {
		j.Delta = runtimekernel.StorageDelta
	}
	return j, err
}

func OpenFileJournal(dir string) (*journal.File, error) {
	return journal.OpenFile(dir, acceptedIdentity)
}

var journalPool = journal.Pool

var (
	errAcceptedConflict = journal.ErrAcceptedConflict
	errTenantJournal    = journal.ErrTenantJournal
	errTenantSnapshot   = journal.ErrTenantSnapshot
)
