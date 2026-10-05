package platformserver

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Journals is a tenant journal a host runs on: PostgreSQL on the delivery path
// (ADR-0019), or one local file on the lightweight one (ADR-0049 D2). Both keep
// the same semantics — entries in order per tenant, appends expect the read
// position, an accepted result is committed once under its idempotency key,
// snapshots are checkpoints of this code — and both carry the derived store
// (vectors and transcripts), which lives wherever the journal lives.
type Journals interface {
	Store
	Entries(ctx context.Context, tenant string, after int64) ([]Entry, error)
	Position(tenant string) int64
	SaveSnapshot(ctx context.Context, tenant string, seq int64, code string, state []byte) error
	RepairSnapshot(ctx context.Context, tenant string, seq int64, code string, state []byte) error
	Snapshot(ctx context.Context, tenant, code string) (int64, []byte, bool, error)
	Append(ctx context.Context, tenant string, e Entry) error
	AppendAccepted(ctx context.Context, tenant string, e Entry, key, hash string) ([]byte, error)
	Close()
}

// pool is the PostgreSQL pool behind a journal, for the reads only the delivery
// path has (the projection for outside tools, ADR-0019 D7). A local file
// journal has none; the deployment refuses what needs one.
func pool(j Journals) (*pgxpool.Pool, bool) {
	pg, ok := j.(*Journal)
	if !ok {
		return nil, false
	}
	return pg.pool, true
}
