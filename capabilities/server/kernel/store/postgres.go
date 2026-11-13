package store

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	pgx "github.com/jackc/pgx/v5"
	"io"
	"log"
	"strings"
	"sync"
	"time"

	"platformserver/platform"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Entry struct {
	App       string          `json:"app"`
	Kind      string          `json:"kind"`
	Principal json.RawMessage `json:"principal"`
	Body      json.RawMessage `json:"body"`
	At        time.Time       `json:"at"`
	Versions  map[string]int  `json:"versions,omitempty"`
	Epoch     int             `json:"epoch"`
}

const FormatEpoch = 1

func SameTime(result, entry time.Time) bool {
	return result.Equal(entry) || result.Truncate(time.Microsecond).Equal(entry)
}

type Postgres struct {
	Delta    func([]byte) (StateDelta, error)
	identify Identify
	mu       sync.Mutex
	pool     *pgxpool.Pool
	readPool *pgxpool.Pool
	next     map[string]int64
	locks    map[string]*sync.Mutex
}

var ErrAcceptedConflict = errors.New("accepted result idempotency key reused for another request")
var ErrTenantJournal = errors.New("tenant journal is damaged")
var ErrTenantSnapshot = errors.New("tenant snapshot is damaged")

var ErrCommitUnknown = errors.New("journal commit outcome is unknown")

const acceptedScopeSQL = `(case when body ->> 'kind' = 'work-result' then 'work'
 when body ->> 'kind' = 'input-result' then 'input'
 when body ->> 'kind' = 'effect-result' then 'effect'
 when body ->> 'kind' = 'host-result' then 'host'
 when body ->> 'kind' = 'composite-result' then 'composite'
 when body ->> 'kind' = 'usage-result' then 'usage'
 when body ->> 'kind' = 'agent-result' then 'agent' else 'submission' end)`

const legacyScopedIndexSQL = `create unique index if not exists journal_result_scoped_key on journal
		(tenant, app, (case when body ->> 'kind' = 'work-result' then 'work'
			when body ->> 'kind' = 'input-result' then 'input'
			when body ->> 'kind' = 'effect-result' then 'effect' else 'submission' end),
		 (coalesce(body ->> 'key', body #>> '{submission,idempotencyKey}', body #>> '{receipt,submission,idempotencyKey}')))
		where kind = 'accepted-result'`

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
	`alter table journal add column if not exists epoch integer not null default 0`,
	`create table if not exists embeddings (
		tenant text not null, model text not null, hash text not null, vector bytea not null,
		primary key (tenant, model, hash))`,
	`create table if not exists transcripts (
		tenant text not null, at timestamptz not null, member text not null, model text not null,
		run text not null default '', request jsonb not null, answer jsonb not null, outcome text not null)`,
	`create index if not exists transcripts_run on transcripts (tenant, run, at)`,
	`create table if not exists observations (
		tenant text not null, id text not null, at timestamptz not null,
		level text not null, source text not null, member text not null default '',
		app text not null default '', route text not null default '',
		request_id text not null default '', trace_id text not null default '',
		span_id text not null default '', run_id text not null default '',
		resource text not null default '', submission_id text not null default '',
		change_id text not null default '', outcome text not null default '',
		message text not null, attributes jsonb not null default '{}'::jsonb,
		primary key (tenant, id))`,
	`create index if not exists observations_tenant_at on observations (tenant, at desc, id desc)`,
	`create index if not exists observations_tenant_run on observations (tenant, run_id, at desc)`,
	`create index if not exists observations_tenant_trace on observations (tenant, trace_id, at desc)`,
	`create index if not exists observations_tenant_level on observations (tenant, level, at desc)`,
	`create table if not exists audit_entries (
		tenant text not null, id text not null, at timestamptz not null,
		member text not null default '', app text not null default '',
		action text not null default '', target text not null default '',
		primary key (tenant, id))`,
	`create index if not exists audit_entries_tenant_at on audit_entries (tenant, at desc, id desc)`,
	`create index if not exists audit_entries_tenant_action on audit_entries (tenant, action, at desc)`,
	`create table if not exists personal_reads (
		tenant text not null, id text not null, at timestamptz not null,
		member text not null default '', record_type text not null default '',
		record_ids jsonb not null default '[]'::jsonb, fields jsonb not null default '[]'::jsonb,
		primary key (tenant, id))`,
	`create index if not exists personal_reads_tenant_at on personal_reads (tenant, at desc, id desc)`,
	`create table if not exists run_index (
		tenant text not null, scope text not null default '', id text not null, type text not null default '',
		source text not null default '', app text not null default '',
		status text not null default '', at timestamptz not null,
		payload jsonb not null default '{}'::jsonb, primary key (tenant, scope, id))`,
	`alter table run_index add column if not exists scope text not null default ''`,
	`alter table run_index drop constraint if exists run_index_pkey`,
	`alter table run_index add primary key (tenant, scope, id)`,
	`create index if not exists run_index_tenant_at on run_index (tenant, at desc, id desc)`,
	`drop index if exists journal_accepted_key`,
	`drop index if exists journal_result_key`,
	`drop index if exists journal_result_request_key`,
	`drop index if exists journal_result_input_key`,
	`drop index if exists journal_result_scoped_key`,
	legacyScopedIndexSQL,
	`drop index if exists journal_result_scoped_key`,
	`create unique index journal_result_scoped_key on journal
		(tenant, app, ` + acceptedScopeSQL + `,
		 (coalesce(body ->> 'key', body #>> '{submission,idempotencyKey}', body #>> '{receipt,submission,idempotencyKey}')))
		where kind = 'accepted-result'`,
}

func OpenPostgres(ctx context.Context, url string, identify Identify) (*Postgres, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	if err := initializeJournal(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	if err := migrateVectors(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	readPool, err := pgxpool.NewWithConfig(ctx, pool.Config())
	if err != nil {
		pool.Close()
		return nil, err
	}
	return &Postgres{identify: identify, pool: pool, readPool: readPool, next: map[string]int64{}, locks: map[string]*sync.Mutex{}}, nil
}

func initializeJournal(ctx context.Context, pool *pgxpool.Pool) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(1347174740, 1)`); err != nil {
		return err
	}
	var fenced bool
	if err := tx.QueryRow(ctx, `select to_regclass('public.platform_storage_fence') is not null`).Scan(&fenced); err != nil {
		return err
	}
	if fenced {
		return fmt.Errorf("storage is fenced for handover; use the verified target or perform a new handover from the current authority")
	}
	if _, err := tx.Exec(ctx, `create table if not exists journal_migrations (id text primary key, applied timestamptz not null default now())`); err != nil {
		return err
	}
	applied := map[string]bool{}
	rows, err := tx.Query(ctx, `select id from journal_migrations`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		applied[id] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	migrationID := func(stmt string) string { sum := sha256.Sum256([]byte(stmt)); return hex.EncodeToString(sum[:]) }
	mark := func(stmt string) error {
		_, err := tx.Exec(ctx, `insert into journal_migrations(id) values ($1) on conflict do nothing`, migrationID(stmt))
		return err
	}
	var currentIndex string
	for _, stmt := range append(append(schema, stateSchema...), vectorSchema...) {
		if strings.Contains(stmt, "journal_result_scoped_key") {
			if strings.HasPrefix(stmt, "create unique index journal_result_scoped_key") {
				currentIndex = stmt
			}
			continue
		}
		if applied[migrationID(stmt)] {
			continue
		}
		if _, err := tx.Exec(ctx, stmt); err != nil {
			return err
		}
		if err := mark(stmt); err != nil {
			return err
		}
	}
	var definition string
	err = tx.QueryRow(ctx, `select coalesce(pg_get_indexdef(to_regclass('journal_result_scoped_key')),'')`).Scan(&definition)
	if err != nil {
		return err
	}
	definition = strings.ToLower(definition)
	correct := strings.Contains(definition, "coalesce")
	for _, scope := range []string{"work-result", "input-result", "effect-result", "host-result", "composite-result", "usage-result", "agent-result"} {
		correct = correct && strings.Contains(definition, "'"+scope+"'")
	}
	if !correct {
		if _, err := tx.Exec(ctx, `drop index if exists journal_result_scoped_key`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, currentIndex); err != nil {
			return err
		}
	}
	if err := mark(currentIndex); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (j *Postgres) Entries(ctx context.Context, tenant string, after int64) ([]Entry, error) {
	rows, err := j.pool.Query(ctx, `select seq, app, kind, principal, body, at, versions, epoch from journal where tenant = $1 and seq > $2 order by seq`, tenant, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Entry
	last := after
	for rows.Next() {
		var e Entry
		var seq int64
		if err := rows.Scan(&seq, &e.App, &e.Kind, &e.Principal, &e.Body, &e.At, &e.Versions, &e.Epoch); err != nil {
			return nil, fmt.Errorf("%w: tenant %s entry after %d: %v", ErrTenantJournal, tenant, last, err)
		}
		if seq != last+1 {
			return nil, fmt.Errorf("%w: tenant %s expected entry %d, found %d", ErrTenantJournal, tenant, last+1, seq)
		}
		last = seq
		e.At = e.At.UTC()
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	j.next[tenant] = last + 1
	return out, nil
}

func (j *Postgres) Position(tenant string) int64 {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.next[tenant] - 1
}

func (j *Postgres) SaveSnapshot(ctx context.Context, tenant string, seq int64, code string, state []byte) error {
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

func (j *Postgres) RepairSnapshot(ctx context.Context, tenant string, seq int64, code string, state []byte) error {
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

func (j *Postgres) Snapshot(ctx context.Context, tenant, code string) (int64, []byte, bool, error) {
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
		return 0, nil, false, fmt.Errorf("%w: tenant %s at %d: %v", ErrTenantSnapshot, tenant, seq, err)
	}
	state, err := io.ReadAll(r)
	if err != nil {
		return 0, nil, false, fmt.Errorf("%w: tenant %s at %d: %v", ErrTenantSnapshot, tenant, seq, err)
	}
	if err := r.Close(); err != nil {
		return 0, nil, false, fmt.Errorf("%w: tenant %s at %d: %v", ErrTenantSnapshot, tenant, seq, err)
	}
	return seq, state, true, nil
}

func (j *Postgres) AppendAccepted(ctx context.Context, tenant string, e Entry, key, hash string) ([]byte, error) {
	identity, err := j.identify(e.Body)
	if err != nil || e.Kind != "accepted-result" || identity.Tenant != tenant || identity.App != e.App ||
		identity.Key != key {
		return nil, fmt.Errorf("journal: accepted input identity differs: %v", err)
	}
	if identity.Hash != hash {
		return nil, ErrAcceptedConflict
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
		var priorHash string
		var priorPosition int64
		advance := func() {
			j.mu.Lock()
			j.next[tenant] = max(j.next[tenant], priorPosition+1)
			j.mu.Unlock()
		}
		lookupErr := j.pool.QueryRow(ctx, `select body,hash,seq from runtime_results where tenant=$1 and app=$2 and scope=$3 and key=$4`, tenant, e.App, identity.Scope, key).Scan(&raw, &priorHash, &priorPosition)
		if lookupErr == nil {
			if priorHash != hash {
				return nil, ErrAcceptedConflict
			}
			advance()
			return raw, nil
		}
		if !errors.Is(lookupErr, pgx.ErrNoRows) {
			return nil, lookupErr
		}
		err := j.pool.QueryRow(ctx, `select body,seq from journal where tenant=$1 and app=$2
			and kind='accepted-result' and coalesce(body ->> 'key', body #>> '{submission,idempotencyKey}',
				body #>> '{receipt,submission,idempotencyKey}')=$3
			and `+acceptedScopeSQL+`=$4`,
			tenant, e.App, key, identity.Scope).Scan(&raw, &priorPosition)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		savedIdentity, err := j.identify(raw)
		savedHash := savedIdentity.Hash
		if err != nil {
			return nil, err
		}
		if savedHash != hash {
			return nil, ErrAcceptedConflict
		}
		advance()
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
	tx, err := j.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext($1),63)`, tenant); err != nil {
		return nil, err
	}
	var active bool
	var statePosition int64
	stateErr := tx.QueryRow(ctx, `select seq from runtime_tenants where tenant=$1 for update`, tenant).Scan(&statePosition)
	if stateErr == nil {
		active = true
		if statePosition != seq-1 {
			return nil, fmt.Errorf("tenant %s state position differs", tenant)
		}
	} else if !errors.Is(stateErr, pgx.ErrNoRows) {
		return nil, stateErr
	}
	tag, err := tx.Exec(ctx, `insert into journal (tenant, seq, app, kind, principal, body, at, versions, epoch)
 values ($1,$2,$3,$4,$5,$6,$7,$8,$9) on conflict do nothing`, tenant, seq, e.App, e.Kind, e.Principal, e.Body, e.At, e.Versions, e.Epoch)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		_ = tx.Rollback(ctx)
		if prior, err := read(); err != nil || prior != nil {
			return prior, err
		}
		return nil, fmt.Errorf("journal: concurrent writer changed tenant %s at %d", tenant, seq)
	}
	var saved []byte
	if err = tx.QueryRow(ctx, `select body from journal where tenant=$1 and seq=$2`, tenant, seq).Scan(&saved); err != nil {
		return nil, err
	}
	if active {
		if j.Delta == nil {
			return nil, fmt.Errorf("tenant %s has no state delta owner", tenant)
		}
		delta, err := j.Delta(saved)
		if err != nil {
			return nil, err
		}
		if err = j.writeState(ctx, tx, tenant, seq, delta); err != nil {
			return nil, err
		}
		if _, err = tx.Exec(ctx, `insert into runtime_results (tenant,app,scope,key,hash,seq,body) values ($1,$2,$3,$4,$5,$6,$7)`, tenant, e.App, identity.Scope, key, hash, seq, saved); err != nil {
			return nil, err
		}
		if _, err = tx.Exec(ctx, `update runtime_tenants set seq=$2 where tenant=$1`, tenant, seq); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		if prior, lookupErr := read(); lookupErr == nil && prior != nil {
			j.mu.Lock()
			j.next[tenant] = seq + 1
			j.mu.Unlock()
			return prior, nil
		}
		if errors.Is(err, pgx.ErrTxCommitRollback) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %v", ErrCommitUnknown, err)
	}

	j.mu.Lock()
	j.next[tenant] = seq + 1
	j.mu.Unlock()
	committed, err := read()
	if err != nil {
		return nil, fmt.Errorf("%w: read committed result: %v", ErrCommitUnknown, err)
	}
	if len(committed) == 0 {
		return nil, fmt.Errorf("%w: accepted result missing after append", ErrCommitUnknown)
	}
	return committed, nil
}

func (j *Postgres) Close() { j.readPool.Close(); j.pool.Close() }

func (j *Postgres) Vectors(tenant, model string, hashes []string) map[string][]float32 {
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
			out[h] = DecodeVector(v)
		}
	}
	return out
}

func (j *Postgres) SaveVectors(tenant, model string, vs map[string][]float32) {
	j.saveSearchVectors(tenant, model, vs)
}

func (j *Postgres) SaveTranscript(x Transcript) {
	if _, err := j.pool.Exec(context.Background(), `insert into transcripts (tenant, at, member, model, run, request, answer, outcome) values ($1, $2, $3, $4, $5, $6, $7, $8)`,
		x.Tenant, x.At, x.Member, x.Model, x.Run, x.Request, x.Answer, x.Outcome); err != nil {
		log.Printf("transcript: %v", err)
	}
}

func (j *Postgres) Transcripts(tenant, run string, limit int) []Transcript {
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

func (j *Postgres) PurgeTranscripts(tenant string, before time.Time) {
	if _, err := j.pool.Exec(context.Background(), `delete from transcripts where tenant = $1 and at < $2`, tenant, before); err != nil {
		log.Printf("purge transcripts: %v", err)
	}
}

func (j *Postgres) SaveObservations(events []platform.Observation) error {
	if len(events) == 0 {
		return nil
	}
	ctx := context.Background()
	tx, err := j.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	batch := &pgx.Batch{}
	for _, event := range events {
		event = normalizeObservation(event)
		batch.Queue(`insert into observations
			(tenant, id, at, level, source, member, app, route, request_id, trace_id, span_id,
			 run_id, resource, submission_id, change_id, outcome, message, attributes)
			values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
			on conflict (tenant, id) do nothing`,
			event.Tenant, event.ID, event.At, event.Level, event.Source, event.Member, event.App, event.Route,
			event.RequestID, event.TraceID, event.SpanID, event.RunID, event.Resource, event.SubmissionID,
			event.ChangeID, event.Outcome, event.Message, event.Attributes)
	}
	results := tx.SendBatch(ctx, batch)
	for range events {
		if _, err := results.Exec(); err != nil {
			results.Close()
			return err
		}
	}
	if err := results.Close(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (j *Postgres) QueryObservations(query platform.ObservationQuery) (platform.ObservationPage, error) {
	cursorAt, cursorID, hasCursor, err := prepareObservationQuery(query)
	if err != nil {
		return platform.ObservationPage{}, err
	}
	limit := observationLimit(query.Limit)
	args := []any{query.Tenant}
	where := []string{"tenant = $1"}
	filter := func(column, value string) {
		if value == "" {
			return
		}
		args = append(args, value)
		where = append(where, fmt.Sprintf("%s = $%d", column, len(args)))
	}
	filter("level", query.Level)
	filter("source", query.Source)
	filter("app", query.App)
	filter("member", query.Member)
	filter("request_id", query.RequestID)
	filter("trace_id", query.TraceID)
	filter("run_id", query.RunID)
	filter("resource", query.Resource)
	if !query.From.IsZero() {
		args = append(args, query.From)
		where = append(where, fmt.Sprintf("at >= $%d", len(args)))
	}
	if !query.To.IsZero() {
		args = append(args, query.To)
		where = append(where, fmt.Sprintf("at < $%d", len(args)))
	}
	if query.Search != "" {
		args = append(args, strings.ToLower(query.Search))
		where = append(where, fmt.Sprintf(`(strpos(lower(message), $%d) > 0 or exists (
			select 1 from jsonb_each_text(attributes) attribute
			where strpos(lower(attribute.value), $%d) > 0))`, len(args), len(args)))
	}
	if hasCursor {
		args = append(args, cursorAt, cursorID)
		where = append(where, fmt.Sprintf("(at < $%d or (at = $%d and id < $%d))", len(args)-1, len(args)-1, len(args)))
	}
	statement := `select tenant, id, at, level, source, member, app, route, request_id, trace_id, span_id,
		run_id, resource, submission_id, change_id, outcome, message, attributes
		from observations where ` + strings.Join(where, " and ") +
		fmt.Sprintf(" order by at desc, id desc limit %d", limit+1)
	rows, err := j.pool.Query(context.Background(), statement, args...)
	if err != nil {
		return platform.ObservationPage{}, err
	}
	defer rows.Close()
	entries := make([]platform.Observation, 0, limit+1)
	for rows.Next() {
		var event platform.Observation
		if err := rows.Scan(&event.Tenant, &event.ID, &event.At, &event.Level, &event.Source, &event.Member,
			&event.App, &event.Route, &event.RequestID, &event.TraceID, &event.SpanID, &event.RunID,
			&event.Resource, &event.SubmissionID, &event.ChangeID, &event.Outcome, &event.Message,
			&event.Attributes); err != nil {
			return platform.ObservationPage{}, err
		}
		event.At = event.At.UTC()
		entries = append(entries, event)
	}
	if err := rows.Err(); err != nil {
		return platform.ObservationPage{}, err
	}
	var retainedFrom *time.Time
	if err := j.pool.QueryRow(context.Background(), `select min(at) from observations where tenant = $1`, query.Tenant).Scan(&retainedFrom); err != nil {
		return platform.ObservationPage{}, err
	}
	hasMore := len(entries) > limit
	if hasMore {
		entries = entries[:limit]
	}
	var earliest time.Time
	if retainedFrom != nil {
		earliest = retainedFrom.UTC()
	}
	return finishObservationPage(entries, earliest, hasMore), nil
}

func (j *Postgres) PurgeObservations(tenant string, before time.Time) (int, error) {
	tag, err := j.pool.Exec(context.Background(), `delete from observations where tenant = $1 and at < $2`, tenant, before)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

func (j *Postgres) ObservationUsage(tenant string, from time.Time) (int64, error) {
	var total int64
	if err := j.pool.QueryRow(context.Background(), `select coalesce(sum(pg_column_size(o)), 0) from observations o where tenant = $1 and at >= $2`, tenant, from).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func (j *Postgres) SummarizeObservations(tenant, member string, from, to time.Time) (platform.ObservationSummary, error) {
	out := platform.ObservationSummary{From: from.UTC(), To: to.UTC()}
	memberClause := ""
	args := []any{tenant, from, to}
	if member != "" {
		memberClause = " and member = $4"
		args = append(args, member)
	}
	if err := j.pool.QueryRow(context.Background(), `select
		count(*) filter (where source = 'request'),
		count(*) filter (where level = 'error'),
		count(*) filter (where level = 'warn'),
		coalesce(avg((attributes ->> 'durationMs')::bigint) filter (where source = 'request' and attributes ->> 'durationMs' ~ '^[0-9]+$'), 0)::float8,
		coalesce(max((attributes ->> 'durationMs')::bigint) filter (where source = 'request' and attributes ->> 'durationMs' ~ '^[0-9]+$'), 0)
		from observations where tenant = $1 and at >= $2 and at < $3`+memberClause, args...).Scan(&out.Requests, &out.Errors, &out.Warnings, &out.AverageMillis, &out.MaxMillis); err != nil {
		return platform.ObservationSummary{}, err
	}
	return out, nil
}

func (j *Postgres) SaveAuditEntries(tenant string, entries []platform.AuditEntry) error {
	if tenant == "" || len(entries) == 0 {
		return nil
	}
	ctx := context.Background()
	tx, err := j.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	batch := &pgx.Batch{}
	for _, entry := range entries {
		entry = normalizeAuditEntry(entry)
		if entry.ID == "" {
			return fmt.Errorf("audit entry needs an identity")
		}
		batch.Queue(`insert into audit_entries (tenant, id, at, member, app, action, target)
			values ($1,$2,$3,$4,$5,$6,$7) on conflict (tenant, id) do nothing`,
			tenant, entry.ID, entry.At, entry.Member, entry.App, entry.Action, entry.Target)
	}
	results := tx.SendBatch(ctx, batch)
	for range entries {
		if _, err := results.Exec(); err != nil {
			results.Close()
			return err
		}
	}
	if err := results.Close(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (j *Postgres) QueryAuditEntries(query platform.AuditQuery) (platform.AuditPage, error) {
	limit := auditLimit(query.Limit)
	args := []any{query.Tenant}
	where := []string{"tenant = $1"}
	filter := func(column, value string) {
		if value == "" {
			return
		}
		args = append(args, value)
		where = append(where, fmt.Sprintf("%s = $%d", column, len(args)))
	}
	filter("member", query.Member)
	filter("app", query.App)
	filter("action", query.Action)
	if !query.From.IsZero() {
		args = append(args, query.From)
		where = append(where, fmt.Sprintf("at >= $%d", len(args)))
	}
	if !query.To.IsZero() {
		args = append(args, query.To)
		where = append(where, fmt.Sprintf("at < $%d", len(args)))
	}
	if query.Search != "" {
		args = append(args, strings.ToLower(query.Search))
		where = append(where, fmt.Sprintf(`strpos(lower(member || ' ' || app || ' ' || action || ' ' || target), $%d) > 0`, len(args)))
	}
	if query.Cursor != "" {
		at, id, err := decodeAuditCursor(query.Cursor)
		if err != nil {
			return platform.AuditPage{}, err
		}
		args = append(args, at, id)
		where = append(where, fmt.Sprintf("(at < $%d or (at = $%d and id < $%d))", len(args)-1, len(args)-1, len(args)))
	}
	statement := `select id, at, member, app, action, target from audit_entries where ` + strings.Join(where, " and ") +
		fmt.Sprintf(" order by at desc, id desc limit %d", limit+1)
	rows, err := j.pool.Query(context.Background(), statement, args...)
	if err != nil {
		return platform.AuditPage{}, err
	}
	defer rows.Close()
	entries := make([]platform.AuditEntry, 0, limit+1)
	for rows.Next() {
		var entry platform.AuditEntry
		if err := rows.Scan(&entry.ID, &entry.At, &entry.Member, &entry.App, &entry.Action, &entry.Target); err != nil {
			return platform.AuditPage{}, err
		}
		entry.At = entry.At.UTC()
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return platform.AuditPage{}, err
	}
	var retainedFrom *time.Time
	if err := j.pool.QueryRow(context.Background(), `select min(at) from audit_entries where tenant = $1`, query.Tenant).Scan(&retainedFrom); err != nil {
		return platform.AuditPage{}, err
	}
	hasMore := len(entries) > limit
	if hasMore {
		entries = entries[:limit]
	}
	if entries == nil {
		entries = []platform.AuditEntry{}
	}
	var earliest time.Time
	if retainedFrom != nil {
		earliest = retainedFrom.UTC()
	}
	page := platform.AuditPage{Entries: entries, HasMore: hasMore, Coverage: platform.AuditCoverage{RetainedFrom: earliest, Truncated: hasMore}}
	if hasMore {
		last := entries[len(entries)-1]
		page.NextCursor = encodeAuditCursor(last.At, last.ID)
	}
	return page, nil
}

func (j *Postgres) SavePersonalReads(tenant string, reads []platform.PersonalRead) error {
	if tenant == "" || len(reads) == 0 {
		return nil
	}
	ctx := context.Background()
	tx, err := j.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	batch := &pgx.Batch{}
	for _, read := range reads {
		read = normalizePersonalRead(read)
		if read.ID == "" {
			return fmt.Errorf("personal read needs an identity")
		}
		ids, _ := json.Marshal(read.IDs)
		fields, _ := json.Marshal(read.Fields)
		batch.Queue(`insert into personal_reads (tenant, id, at, member, record_type, record_ids, fields)
			values ($1,$2,$3,$4,$5,$6,$7) on conflict (tenant, id) do nothing`,
			tenant, read.ID, read.At, read.Member, read.Type, ids, fields)
	}
	results := tx.SendBatch(ctx, batch)
	for range reads {
		if _, err := results.Exec(); err != nil {
			results.Close()
			return err
		}
	}
	if err := results.Close(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (j *Postgres) QueryPersonalReads(query platform.PersonalReadQuery) (platform.PersonalReadPage, error) {
	limit := auditLimit(query.Limit)
	args := []any{query.Tenant}
	where := []string{"tenant = $1"}
	filter := func(column, value string) {
		if value == "" {
			return
		}
		args = append(args, value)
		where = append(where, fmt.Sprintf("%s = $%d", column, len(args)))
	}
	filter("member", query.Member)
	filter("record_type", query.Type)
	if !query.From.IsZero() {
		args = append(args, query.From)
		where = append(where, fmt.Sprintf("at >= $%d", len(args)))
	}
	if !query.To.IsZero() {
		args = append(args, query.To)
		where = append(where, fmt.Sprintf("at < $%d", len(args)))
	}
	if query.Cursor != "" {
		at, id, err := decodeAuditCursor(query.Cursor)
		if err != nil {
			return platform.PersonalReadPage{}, err
		}
		args = append(args, at, id)
		where = append(where, fmt.Sprintf("(at < $%d or (at = $%d and id < $%d))", len(args)-1, len(args)-1, len(args)))
	}
	statement := `select id, at, member, record_type, record_ids, fields from personal_reads where ` + strings.Join(where, " and ") +
		fmt.Sprintf(" order by at desc, id desc limit %d", limit+1)
	rows, err := j.pool.Query(context.Background(), statement, args...)
	if err != nil {
		return platform.PersonalReadPage{}, err
	}
	defer rows.Close()
	reads := make([]platform.PersonalRead, 0, limit+1)
	for rows.Next() {
		var read platform.PersonalRead
		var ids, fields []byte
		if err := rows.Scan(&read.ID, &read.At, &read.Member, &read.Type, &ids, &fields); err != nil {
			return platform.PersonalReadPage{}, err
		}
		if json.Unmarshal(ids, &read.IDs) != nil || json.Unmarshal(fields, &read.Fields) != nil {
			return platform.PersonalReadPage{}, fmt.Errorf("personal read has invalid arrays")
		}
		read.At = read.At.UTC()
		reads = append(reads, read)
	}
	if err := rows.Err(); err != nil {
		return platform.PersonalReadPage{}, err
	}
	var retainedFrom *time.Time
	if err := j.pool.QueryRow(context.Background(), `select min(at) from personal_reads where tenant = $1`, query.Tenant).Scan(&retainedFrom); err != nil {
		return platform.PersonalReadPage{}, err
	}
	hasMore := len(reads) > limit
	if hasMore {
		reads = reads[:limit]
	}
	if reads == nil {
		reads = []platform.PersonalRead{}
	}
	var earliest time.Time
	if retainedFrom != nil {
		earliest = retainedFrom.UTC()
	}
	page := platform.PersonalReadPage{Entries: reads, HasMore: hasMore, Coverage: platform.PersonalReadCoverage{RetainedFrom: earliest, Truncated: hasMore}}
	if hasMore {
		last := reads[len(reads)-1]
		page.NextCursor = encodeAuditCursor(last.At, last.ID)
	}
	return page, nil
}

func (j *Postgres) ReplaceRunIndex(tenant, scope string, entries []platform.RunIndexEntry) error {
	ctx := context.Background()
	tx, err := j.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `delete from run_index where tenant = $1 and scope = $2`, tenant, scope); err != nil {
		return err
	}
	batch := &pgx.Batch{}
	queued := 0
	for _, entry := range entries {
		entry = normalizeRun(entry)
		entry.Scope = scope
		if entry.ID == "" {
			continue
		}
		batch.Queue(`insert into run_index (tenant, scope, id, type, source, app, status, at, payload)
			values ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
			tenant, scope, entry.ID, entry.Type, entry.Source, entry.App, entry.Status, entry.At, entry.Payload)
		queued++
	}
	results := tx.SendBatch(ctx, batch)
	for range queued {
		if _, err := results.Exec(); err != nil {
			results.Close()
			return err
		}
	}
	if err := results.Close(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (j *Postgres) QueryRunIndex(query platform.RunIndexQuery) (platform.RunIndexPage, error) {
	limit := query.Limit
	if limit < 1 {
		limit = 1
	}
	if limit > 1000 {
		limit = 1000
	}
	args := []any{query.Tenant, query.Scope}
	where := []string{"tenant = $1", "scope = $2"}
	filter := func(column, value string) {
		if value == "" {
			return
		}
		args = append(args, value)
		where = append(where, fmt.Sprintf("%s = $%d", column, len(args)))
	}
	filter("type", query.Type)
	filter("source", query.Source)
	filter("app", query.App)
	filter("status", query.Status)
	if !query.From.IsZero() {
		args = append(args, query.From)
		where = append(where, fmt.Sprintf("at >= $%d", len(args)))
	}
	if !query.To.IsZero() {
		args = append(args, query.To)
		where = append(where, fmt.Sprintf("at < $%d", len(args)))
	}
	var total int
	countStatement := `select count(*) from run_index where ` + strings.Join(where, " and ")
	if err := j.pool.QueryRow(context.Background(), countStatement, args...).Scan(&total); err != nil {
		return platform.RunIndexPage{}, err
	}
	args = append(args, max(0, query.Offset), limit)
	statement := `select id, type, source, app, status, at, payload from run_index where ` + strings.Join(where, " and ") +
		fmt.Sprintf(" order by at desc, id desc offset $%d limit $%d", len(args)-1, len(args))
	rows, err := j.pool.Query(context.Background(), statement, args...)
	if err != nil {
		return platform.RunIndexPage{}, err
	}
	defer rows.Close()
	entries := []platform.RunIndexEntry{}
	for rows.Next() {
		var entry platform.RunIndexEntry
		if err := rows.Scan(&entry.ID, &entry.Type, &entry.Source, &entry.App, &entry.Status, &entry.At, &entry.Payload); err != nil {
			return platform.RunIndexPage{}, err
		}
		entry.At = entry.At.UTC()
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return platform.RunIndexPage{}, err
	}
	var retainedFrom *time.Time
	if err := j.pool.QueryRow(context.Background(), `select min(at) from run_index where tenant = $1 and scope = $2`, query.Tenant, query.Scope).Scan(&retainedFrom); err != nil {
		return platform.RunIndexPage{}, err
	}
	var earliest time.Time
	if retainedFrom != nil {
		earliest = retainedFrom.UTC()
	}
	return platform.RunIndexPage{Entries: entries, Total: total, RetainedFrom: earliest}, nil
}
