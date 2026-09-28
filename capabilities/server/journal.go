package platformserver

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Entry is one durable tenant input or accepted result. Legacy entries replay
// the application code; accepted-result entries apply saved record and intent
// bytes without rerunning the original business decision (ADR-0038).
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

// PostgreSQL timestamptz stores microseconds while the JSONB result envelope
// retains Go's nanoseconds. Compare the durable precision, not the transient
// sub-microsecond fraction, when replaying a saved result from a real journal.
func sameJournalTime(result, entry time.Time) bool {
	return result.Truncate(time.Microsecond).Equal(entry)
}

// Journal keeps entries in PostgreSQL. Each tenant's entries are numbered; an
// append names the number it expects, so a second writer that has not replayed
// the other's entries fails instead of interleaving (one authority per tenant, K5).
type Journal struct {
	mu    sync.Mutex
	pool  *pgxpool.Pool
	next  map[string]int64
	locks map[string]*sync.Mutex // one per tenant: appends of a tenant are in order
}

var errAcceptedConflict = errors.New("accepted result idempotency key reused for another request")
var errTenantJournal = errors.New("tenant journal is damaged")
var errTenantSnapshot = errors.New("tenant snapshot is damaged")

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
	`create table if not exists embeddings (
		tenant text not null, model text not null, hash text not null, vector bytea not null,
		primary key (tenant, model, hash))`,
	`create table if not exists transcripts (
		tenant text not null, at timestamptz not null, member text not null, model text not null,
		run text not null default '', request jsonb not null, answer jsonb not null, outcome text not null)`,
	`create index if not exists transcripts_run on transcripts (tenant, run, at)`,
	`drop index if exists journal_accepted_key`,
	`drop index if exists journal_result_key`,
	`drop index if exists journal_result_request_key`,
	`drop index if exists journal_result_input_key`,
	`drop index if exists journal_result_scoped_key`,
	`create unique index if not exists journal_result_scoped_key on journal
		(tenant, app, (case when body ->> 'kind' = 'work-result' then 'work'
			when body ->> 'kind' = 'input-result' then 'input'
			when body ->> 'kind' = 'effect-result' then 'effect' else 'submission' end),
		 (coalesce(body ->> 'key', body #>> '{submission,idempotencyKey}', body #>> '{receipt,submission,idempotencyKey}')))
		where kind = 'accepted-result'`,
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
	return &Journal{pool: pool, next: map[string]int64{}, locks: map[string]*sync.Mutex{}}, nil
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
		var seq int64
		if err := rows.Scan(&seq, &e.App, &e.Kind, &e.Principal, &e.Body, &e.At, &e.Versions); err != nil {
			return nil, fmt.Errorf("%w: tenant %s entry after %d: %v", errTenantJournal, tenant, last, err)
		}
		if seq != last+1 {
			return nil, fmt.Errorf("%w: tenant %s expected entry %d, found %d", errTenantJournal, tenant, last+1, seq)
		}
		last = seq
		e.At = e.At.UTC() // jsonb replay must reproduce the original audit bytes
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err // a database/transport failure affects more than this tenant
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	j.next[tenant] = last + 1
	return out, nil
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
	packed, err := packSnapshot(state)
	if err != nil {
		return err
	}
	if _, err := j.pool.Exec(ctx, `insert into snapshots (tenant, seq, code, state) values ($1, $2, $3, $4) on conflict (tenant, seq) do nothing`,
		tenant, seq, code, packed); err != nil {
		return err
	}
	_, err = j.pool.Exec(ctx, `delete from snapshots where tenant = $1 and seq < (select min(seq) from (select seq from snapshots where tenant = $1 order by seq desc limit 2) newest)`, tenant)
	return err
}

func packSnapshot(state []byte) ([]byte, error) {
	var packed bytes.Buffer
	w := gzip.NewWriter(&packed)
	if _, err := w.Write(state); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return packed.Bytes(), nil
}

// RepairSnapshot replaces only this tenant's derived checkpoints, after the
// entire durable journal has been validated and a fresh state captured. A
// corrupt checkpoint at the same sequence must not survive the next restart.
func (j *Journal) RepairSnapshot(ctx context.Context, tenant string, seq int64, code string, state []byte) error {
	packed, err := packSnapshot(state)
	if err != nil {
		return err
	}
	tx, err := j.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `delete from snapshots where tenant=$1`, tenant); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `insert into snapshots (tenant, seq, code, state) values ($1,$2,$3,$4)`,
		tenant, seq, code, packed); err != nil {
		return err
	}
	return tx.Commit(ctx)
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
		return 0, nil, false, fmt.Errorf("%w: tenant %s at %d: %v", errTenantSnapshot, tenant, seq, err)
	}
	state, err := io.ReadAll(r)
	if err != nil {
		return 0, nil, false, fmt.Errorf("%w: tenant %s at %d: %v", errTenantSnapshot, tenant, seq, err)
	}
	if err := r.Close(); err != nil {
		return 0, nil, false, fmt.Errorf("%w: tenant %s at %d: %v", errTenantSnapshot, tenant, seq, err)
	}
	return seq, state, true, nil
}

// Append adds an entry to a tenant's journal. The order is per tenant, so
// tenants append side by side, each under its own lock (F-34, ADR-0027 D7).
func (j *Journal) Append(ctx context.Context, tenant string, e Entry) error {
	j.mu.Lock()
	lock := j.locks[tenant]
	if lock == nil {
		lock = &sync.Mutex{}
		j.locks[tenant] = lock
	}
	j.mu.Unlock()
	lock.Lock()
	defer lock.Unlock()
	j.mu.Lock()
	seq, read := j.next[tenant]
	j.mu.Unlock()
	if !read {
		return fmt.Errorf("journal: append to %s before reading its entries", tenant)
	}
	started := time.Now()
	defer func() { meters.append.Record(ctx, float64(time.Since(started).Microseconds())/1000) }()
	if _, err := j.pool.Exec(ctx, `insert into journal (tenant, seq, app, kind, principal, body, at, versions) values ($1, $2, $3, $4, $5, $6, $7, $8)`,
		tenant, seq, e.App, e.Kind, e.Principal, e.Body, e.At, e.Versions); err != nil {
		return err
	}
	j.mu.Lock()
	j.next[tenant] = seq + 1
	j.mu.Unlock()
	return nil
}

// AppendAccepted commits a complete decision as one journal row. The partial
// unique index makes retries after an unknown commit outcome return the prior
// result; a competing writer with a stale sequence cannot advance this tenant.
func (j *Journal) AppendAccepted(ctx context.Context, tenant string, e Entry, key, hash string) ([]byte, error) {
	identity, err := acceptedIdentity(e.Body)
	if err != nil || e.Kind != "accepted-result" || identity.Tenant != tenant || identity.App != e.App ||
		identity.Key != key {
		return nil, fmt.Errorf("journal: accepted input identity differs: %v", err)
	}
	if identity.Hash != hash {
		return nil, errAcceptedConflict
	}
	j.mu.Lock()
	lock := j.locks[tenant]
	if lock == nil {
		lock = &sync.Mutex{}
		j.locks[tenant] = lock
	}
	j.mu.Unlock()
	lock.Lock()
	defer lock.Unlock()
	read := func() ([]byte, error) {
		var raw []byte
		err := j.pool.QueryRow(ctx, `select body from journal where tenant=$1 and app=$2
			and kind='accepted-result' and coalesce(body ->> 'key', body #>> '{submission,idempotencyKey}',
				body #>> '{receipt,submission,idempotencyKey}')=$3
			and (case when body ->> 'kind' = 'work-result' then 'work'
				when body ->> 'kind' = 'input-result' then 'input'
				when body ->> 'kind' = 'effect-result' then 'effect' else 'submission' end)=$4`,
			tenant, e.App, key, identity.Scope).Scan(&raw)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		savedHash, err := resultRequestHash(raw)
		if err != nil {
			return nil, err
		}
		if savedHash != hash {
			return nil, errAcceptedConflict
		}
		return raw, nil
	}
	if prior, err := read(); err != nil || prior != nil {
		return prior, err
	}
	j.mu.Lock()
	seq, ok := j.next[tenant]
	j.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("journal: append to %s before reading its entries", tenant)
	}
	tag, err := j.pool.Exec(ctx, `insert into journal (tenant, seq, app, kind, principal, body, at, versions)
		values ($1,$2,$3,$4,$5,$6,$7,$8) on conflict do nothing`,
		tenant, seq, e.App, e.Kind, e.Principal, e.Body, e.At, e.Versions)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		if prior, err := read(); err != nil || prior != nil {
			return prior, err
		}
		return nil, fmt.Errorf("journal: concurrent writer changed tenant %s at %d", tenant, seq)
	}
	j.mu.Lock()
	j.next[tenant] = seq + 1
	j.mu.Unlock()
	// PostgreSQL stores the envelope as jsonb. It normalizes key order even
	// inside raw record history values; applying the caller's pre-insert bytes
	// would leave live history different from a restart's recovered history.
	// Always apply the exact representation the committed journal returns.
	return read()
}

func (j *Journal) Close() { j.pool.Close() }

// The journal's database also keeps what is derived outside the journal
// (ADR-0022): passages' vectors and model calls' transcripts. Losing either
// loses no truth, so failures are logged, not fatal.

func (j *Journal) Vectors(tenant, model string, hashes []string) map[string][]float32 {
	out := map[string][]float32{}
	rows, err := j.pool.Query(context.Background(), `select hash, vector from embeddings where tenant = $1 and model = $2 and hash = any($3)`, tenant, model, hashes)
	if err != nil {
		log.Printf("vectors: %v", err)
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var h string
		var v []byte
		if rows.Scan(&h, &v) == nil {
			out[h] = decodeVector(v)
		}
	}
	return out
}

func (j *Journal) SaveVectors(tenant, model string, vs map[string][]float32) {
	for h, v := range vs {
		if _, err := j.pool.Exec(context.Background(), `insert into embeddings (tenant, model, hash, vector) values ($1, $2, $3, $4) on conflict do nothing`,
			tenant, model, h, encodeVector(v)); err != nil {
			log.Printf("save vectors: %v", err)
			return
		}
	}
}

func (j *Journal) SaveTranscript(x Transcript) {
	if _, err := j.pool.Exec(context.Background(), `insert into transcripts (tenant, at, member, model, run, request, answer, outcome) values ($1, $2, $3, $4, $5, $6, $7, $8)`,
		x.Tenant, x.At, x.Member, x.Model, x.Run, x.Request, x.Answer, x.Outcome); err != nil {
		log.Printf("transcript: %v", err)
	}
}

func (j *Journal) Transcripts(tenant, run string, limit int) []Transcript {
	out := []Transcript{}
	rows, err := j.pool.Query(context.Background(), `select at, member, model, run, request, answer, outcome from transcripts
		where tenant = $1 and ($2 = '' or run = $2) order by at desc limit $3`, tenant, run, limit)
	if err != nil {
		log.Printf("transcripts: %v", err)
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var x Transcript
		if rows.Scan(&x.At, &x.Member, &x.Model, &x.Run, &x.Request, &x.Answer, &x.Outcome) == nil {
			out = append(out, x)
		}
	}
	return out
}

func (j *Journal) PurgeTranscripts(tenant string, before time.Time) {
	if _, err := j.pool.Exec(context.Background(), `delete from transcripts where tenant = $1 and at < $2`, tenant, before); err != nil {
		log.Printf("purge transcripts: %v", err)
	}
}
