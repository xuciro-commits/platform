package platformserver

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Entry is one accepted input of a tenant: a submission, a connector delivery.
// A domain rebuilds its state, kernel logs included, by replaying its entries in
// order through the same code that accepted them (docs/ADR/0007).
type Entry struct {
	App       string          `json:"app"`
	Kind      string          `json:"kind"`
	Principal json.RawMessage `json:"principal"`
	Body      json.RawMessage `json:"body"`
	At        time.Time       `json:"at"`
	// Versions are the flow versions instances started with while the entry was
	// handled (ADR-0020 D6): replay starts them on the same ones, whatever the
	// code declares since.
	Versions map[string]int `json:"versions,omitempty"`
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
	`alter table journal add column if not exists app text not null default ''`,
	`create table if not exists snapshots (
		tenant text not null, seq bigint not null, code text not null, state bytea not null,
		at timestamptz not null default now(), primary key (tenant, seq))`,
	`alter table journal add column if not exists versions jsonb`,
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

// Entries reads a tenant's entries after position after (0: all) in order;
// appends continue after the last.
func (j *Journal) Entries(ctx context.Context, tenant string, after int64) ([]Entry, error) {
	rows, err := j.pool.Query(ctx, `select seq, app, kind, principal, body, at, versions from journal where tenant = $1 and seq > $2 order by seq`, tenant, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Entry
	last := after
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&last, &e.App, &e.Kind, &e.Principal, &e.Body, &e.At, &e.Versions); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	j.next[tenant] = last + 1
	return out, rows.Err()
}

// Position is the number of the tenant's last entry.
func (j *Journal) Position(tenant string) int64 {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.next[tenant] - 1
}

// SaveSnapshot keeps a tenant's state at a position, compressed, with the code
// that wrote it; the two newest are kept (ADR-0019 D6).
func (j *Journal) SaveSnapshot(ctx context.Context, tenant string, seq int64, code string, state []byte) error {
	var packed bytes.Buffer
	w := gzip.NewWriter(&packed)
	w.Write(state)
	if err := w.Close(); err != nil {
		return err
	}
	if _, err := j.pool.Exec(ctx, `insert into snapshots (tenant, seq, code, state) values ($1, $2, $3, $4) on conflict (tenant, seq) do nothing`,
		tenant, seq, code, packed.Bytes()); err != nil {
		return err
	}
	_, err := j.pool.Exec(ctx, `delete from snapshots where tenant = $1 and seq < (select min(seq) from (select seq from snapshots where tenant = $1 order by seq desc limit 2) newest)`, tenant)
	return err
}

// Snapshot is the tenant's newest snapshot written by code, if any.
func (j *Journal) Snapshot(ctx context.Context, tenant, code string) (int64, []byte, bool, error) {
	var seq int64
	var packed []byte
	err := j.pool.QueryRow(ctx, `select seq, state from snapshots where tenant = $1 and code = $2 order by seq desc limit 1`, tenant, code).Scan(&seq, &packed)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil, false, nil
	}
	if err != nil {
		return 0, nil, false, err
	}
	r, err := gzip.NewReader(bytes.NewReader(packed))
	if err != nil {
		return 0, nil, false, err
	}
	state, err := io.ReadAll(r)
	return seq, state, err == nil, err
}

func (j *Journal) Append(ctx context.Context, tenant string, e Entry) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	seq, read := j.next[tenant]
	if !read {
		return fmt.Errorf("journal: append to %s before reading its entries", tenant)
	}
	if _, err := j.pool.Exec(ctx, `insert into journal (tenant, seq, app, kind, principal, body, at, versions) values ($1, $2, $3, $4, $5, $6, $7, $8)`,
		tenant, seq, e.App, e.Kind, e.Principal, e.Body, e.At, e.Versions); err != nil {
		return err
	}
	j.next[tenant] = seq + 1
	return nil
}

func (j *Journal) Close() { j.pool.Close() }
