package platformserver

import (
	"context"

	"platformserver/journal"
)

// The journal is its own package (ADR-0080 wave 1); the host keeps these names
// so tenants, deployments and tests read as before. The only thing the host
// gives the journal is how to read an accepted result's identity.

type (
	Entry      = journal.Entry
	Transcript = journal.Transcript
	Store      = journal.Store
	Journals   = journal.Journals
)

type resultIdentity = journal.Identity

// OpenJournal opens the PostgreSQL journal (ADR-0019).
func OpenJournal(ctx context.Context, url string) (*journal.Postgres, error) {
	return journal.OpenPostgres(ctx, url, acceptedIdentity)
}

// OpenFileJournal opens the one-file journal of the lightweight profile (ADR-0049 D2).
func OpenFileJournal(dir string) (*journal.File, error) {
	return journal.OpenFile(dir, acceptedIdentity)
}

// journalPool is journal.Pool for the places whose local is named journal.
var journalPool = journal.Pool

var (
	errAcceptedConflict = journal.ErrAcceptedConflict
	errTenantJournal    = journal.ErrTenantJournal
	errTenantSnapshot   = journal.ErrTenantSnapshot
)
