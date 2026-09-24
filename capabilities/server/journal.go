package platformserver

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Entry is one accepted input of a tenant: a submission, a connector delivery.
// A domain rebuilds its state, kernel logs included, by replaying its entries in
// order through the same code that accepted them (docs/ADR/0007).
type Entry struct {
	Kind      string          `json:"kind"`
	Principal json.RawMessage `json:"principal"`
	Body      json.RawMessage `json:"body"`
	At        time.Time       `json:"at"`
}

// Journal keeps entries in PostgreSQL. Each tenant's entries are numbered; an
// append names the number it expects, so a second writer that has not replayed
// the other's entries fails instead of interleaving (one authority per tenant, K5).
type Journal struct {
	mu   sync.Mutex
	pool *pgxpool.Pool
	next map[string]int64
}

// schema is forward-only: new statements are appended, never edited.
var schema = []string{
	`create table if not exists journal (
		tenant text not null, seq bigint not null, kind text not null,
		principal jsonb not null, body jsonb not null, at timestamptz not null,
		primary key (tenant, seq))`,
}

func OpenJournal(ctx context.Context, url string) (*Journal, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	for _, stmt := range schema {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			pool.Close()
			return nil, err
		}
	}
	return &Journal{pool: pool, next: map[string]int64{}}, nil
}

// Entries reads a tenant's entries in order; appends continue after them.
func (j *Journal) Entries(ctx context.Context, tenant string) ([]Entry, error) {
	rows, err := j.pool.Query(ctx, `select kind, principal, body, at from journal where tenant = $1 order by seq`, tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.Kind, &e.Principal, &e.Body, &e.At); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	j.next[tenant] = int64(len(out)) + 1
	return out, rows.Err()
}

func (j *Journal) Append(ctx context.Context, tenant string, e Entry) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	seq, read := j.next[tenant]
	if !read {
		return fmt.Errorf("journal: append to %s before reading its entries", tenant)
	}
	if _, err := j.pool.Exec(ctx, `insert into journal (tenant, seq, kind, principal, body, at) values ($1, $2, $3, $4, $5, $6)`,
		tenant, seq, e.Kind, e.Principal, e.Body, e.At); err != nil {
		return err
	}
	j.next[tenant] = seq + 1
	return nil
}

func (j *Journal) Close() { j.pool.Close() }
