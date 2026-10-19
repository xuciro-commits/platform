package journal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

var stateSchema = []string{
	`create table if not exists runtime_tenants (tenant text primary key, seq bigint not null)`,
	`create table if not exists runtime_records (tenant text not null, type text not null, id text not null, image bytea not null, query jsonb not null, history_count integer not null, seq bigint not null default 0, primary key(tenant,type,id))`,
	`alter table runtime_records add column if not exists seq bigint not null default 0`,
	`create index if not exists runtime_records_query on runtime_records using gin (query)`,
	`create index if not exists runtime_records_position on runtime_records (tenant,type,seq,id)`,
	`create table if not exists runtime_history (tenant text not null, type text not null, id text not null, pos integer not null, image bytea not null, query jsonb not null, primary key(tenant,type,id,pos))`,
	`create table if not exists runtime_changes (tenant text not null, app text not null, id text not null, key text not null, num integer not null, image bytea not null, primary key(tenant,app,id), unique(tenant,app,key), unique(tenant,app,num))`,
	`create table if not exists runtime_values (tenant text not null, owner text not null, key text not null, image bytea not null, query jsonb not null, primary key(tenant,owner,key))`,
	`create table if not exists runtime_results (tenant text not null, app text not null, scope text not null, key text not null, hash text not null, seq bigint not null, body bytea not null, primary key(tenant,app,scope,key))`,
}

// InitializeState runs before this tenant accepts writes. The SQL position and
// every image/history entry are compared at the same retained journal position;
// the tenant marker becomes visible only when the complete copy matches.
func (j *Postgres) InitializeState(ctx context.Context, tenant string, position int64, delta StateDelta) error {
	tx, err := j.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext($1), 63)`, tenant); err != nil {
		return err
	}
	var latest int64
	if err = tx.QueryRow(ctx, `select coalesce(max(seq),0) from journal where tenant=$1`, tenant).Scan(&latest); err != nil {
		return err
	}
	if latest != position {
		return fmt.Errorf("tenant %s changed during state migration: %d/%d", tenant, latest, position)
	}
	var saved int64
	err = tx.QueryRow(ctx, `select seq from runtime_tenants where tenant=$1 for update`, tenant).Scan(&saved)
	if errors.Is(err, pgx.ErrNoRows) {
		// Insert only for the first switch. A normal restart never rebuilds tables.
		if err = j.writeState(ctx, tx, tenant, position, delta); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `insert into runtime_results (tenant,app,scope,key,hash,seq,body)
   select tenant,app,`+acceptedScopeSQL+`,coalesce(body->>'key',body#>>'{submission,idempotencyKey}',body#>>'{receipt,submission,idempotencyKey}'),body->>'requestHash',seq,convert_to(body::text,'UTF8') from journal where tenant=$1 and kind='accepted-result'`, tenant); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `insert into runtime_tenants (tenant,seq) values ($1,$2)`, tenant, position); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if saved != position {
		return fmt.Errorf("tenant %s SQL state is at %d, journal at %d", tenant, saved, position)
	}
	if err = j.compareState(ctx, tx, tenant, delta); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (j *Postgres) compareState(ctx context.Context, tx pgx.Tx, tenant string, d StateDelta) error {
	var count int
	if err := tx.QueryRow(ctx, `select count(*) from runtime_records where tenant=$1`, tenant).Scan(&count); err != nil {
		return err
	}
	if count != len(d.Records) {
		return fmt.Errorf("tenant %s current record count differs: %d/%d", tenant, count, len(d.Records))
	}
	for _, r := range d.Records {
		var equal bool
		var n int
		if err := tx.QueryRow(ctx, `select query=$4::jsonb,history_count from runtime_records where tenant=$1 and type=$2 and id=$3`, tenant, r.Type, r.ID, []byte(r.Image)).Scan(&equal, &n); err != nil {
			return err
		}
		if !equal || n != len(r.History) {
			return fmt.Errorf("tenant %s state differs for %s/%s", tenant, r.Type, r.ID)
		}
		rows, err := tx.Query(ctx, `select pos, query from runtime_history where tenant=$1 and type=$2 and id=$3 order by pos`, tenant, r.Type, r.ID)
		if err != nil {
			return err
		}
		at := 0
		for rows.Next() {
			var pos int
			var raw []byte
			if err = rows.Scan(&pos, &raw); err != nil {
				rows.Close()
				return err
			}
			if pos != at || at >= len(r.History) || !sameJSON(raw, r.History[at]) {
				rows.Close()
				return fmt.Errorf("tenant %s history differs for %s/%s at %d", tenant, r.Type, r.ID, at)
			}
			at++
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if at != n {
			return fmt.Errorf("tenant %s history is incomplete", tenant)
		}
	}
	for _, c := range d.Changes {
		var raw []byte
		if err := tx.QueryRow(ctx, `select image from runtime_changes where tenant=$1 and app=$2 and id=$3 and key=$4 and num=$5`, tenant, c.App, c.ID, c.Key, c.Index).Scan(&raw); err != nil {
			return err
		}
		if !bytes.Equal(raw, c.Image) {
			return fmt.Errorf("tenant %s receipt differs: %s/%s", tenant, c.App, c.ID)
		}
	}
	for _, v := range d.Values {
		var equal bool
		if err := tx.QueryRow(ctx, `select query=$4::jsonb from runtime_values where tenant=$1 and owner=$2 and key=$3`, tenant, v.Owner, v.Key, []byte(v.Image)).Scan(&equal); err != nil {
			return err
		}
		if !equal {
			return fmt.Errorf("tenant %s owner state differs: %s/%s", tenant, v.Owner, v.Key)
		}
	}
	return nil
}

func sameJSON(a, b []byte) bool {
	var x, y any
	dx := json.NewDecoder(bytes.NewReader(a))
	dx.UseNumber()
	dy := json.NewDecoder(bytes.NewReader(b))
	dy.UseNumber()
	if dx.Decode(&x) != nil || dy.Decode(&y) != nil {
		return false
	}
	ax, _ := json.Marshal(x)
	by, _ := json.Marshal(y)
	return bytes.Equal(ax, by)
}

func (j *Postgres) writeState(ctx context.Context, tx pgx.Tx, tenant string, position int64, d StateDelta) error {
	for _, r := range d.Records {
		var n int
		err := tx.QueryRow(ctx, `select history_count from runtime_records where tenant=$1 and type=$2 and id=$3`, tenant, r.Type, r.ID).Scan(&n)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		base, h := r.HistoryBase, r.History
		if base < 0 {
			base = n
			if len(h) < n {
				return fmt.Errorf("record history shrank: %s/%s", r.Type, r.ID)
			}
			h = h[n:]
		} else if base != n {
			return fmt.Errorf("record history predecessor differs: %s/%s (%d/%d)", r.Type, r.ID, base, n)
		}
		for i, image := range h {
			if _, err = tx.Exec(ctx, `insert into runtime_history (tenant,type,id,pos,image,query) values ($1,$2,$3,$4,$5,$6::jsonb)`, tenant, r.Type, r.ID, base+i, []byte(image), string(image)); err != nil {
				return err
			}
		}
		if _, err = tx.Exec(ctx, `insert into runtime_records (tenant,type,id,image,query,history_count,seq) values ($1,$2,$3,$4,$5::jsonb,$6,$7) on conflict(tenant,type,id) do update set image=excluded.image,query=excluded.query,history_count=excluded.history_count,seq=excluded.seq`, tenant, r.Type, r.ID, []byte(r.Image), string(r.Image), base+len(h), position); err != nil {
			return err
		}
	}
	for _, c := range d.Changes {
		if c.Index == 0 {
			if err := tx.QueryRow(ctx, `select coalesce(max(num),0)+1 from runtime_changes where tenant=$1 and app=$2`, tenant, c.App).Scan(&c.Index); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `insert into runtime_changes (tenant,app,id,key,num,image) values ($1,$2,$3,$4,$5,$6)`, tenant, c.App, c.ID, c.Key, c.Index, c.Image); err != nil {
			return err
		}
	}
	for _, v := range d.Values {
		if _, err := tx.Exec(ctx, `insert into runtime_values (tenant,owner,key,image,query) values ($1,$2,$3,$4,$5::jsonb) on conflict(tenant,owner,key) do update set image=excluded.image,query=excluded.query`, tenant, v.Owner, v.Key, []byte(v.Image), string(v.Image)); err != nil {
			return err
		}
	}
	return nil
}

func (j *Postgres) StateRecord(ctx context.Context, tenant, typ, id string) (json.RawMessage, error) {
	var raw []byte
	err := j.pool.QueryRow(ctx, `select image from runtime_records where tenant=$1 and type=$2 and id=$3`, tenant, typ, id).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	return raw, err
}
func (j *Postgres) StateHistory(ctx context.Context, tenant, typ, id string, after, through int) ([]json.RawMessage, error) {
	rows, err := j.pool.Query(ctx, `select pos,image from runtime_history where tenant=$1 and type=$2 and id=$3 and pos>=$4 and pos<$5 order by pos`, tenant, typ, id, after, through)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []json.RawMessage
	for rows.Next() {
		var pos int
		var raw []byte
		if err = rows.Scan(&pos, &raw); err != nil {
			return nil, err
		}
		if pos != after+len(out) {
			return nil, fmt.Errorf("record history has a missing entry")
		}
		out = append(out, json.RawMessage(raw))
	}
	if err = rows.Err(); err == nil && len(out) != through-after {
		err = fmt.Errorf("record history is incomplete: %d/%d", len(out), through-after)
	}
	return out, err
}
func (j *Postgres) StateChanges(ctx context.Context, tenant, app, key, id string, through int) ([][]byte, error) {
	rows, err := j.pool.Query(ctx, `select image from runtime_changes where tenant=$1 and app=$2 and num<=$3 and ($4='' or key=$4) and ($5='' or id=$5) order by num`, tenant, app, through, key, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out [][]byte
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		out = append(out, raw)
	}
	return out, rows.Err()
}
func (j *Postgres) StateValue(ctx context.Context, tenant, owner, key string) (json.RawMessage, error) {
	var raw []byte
	err := j.pool.QueryRow(ctx, `select image from runtime_values where tenant=$1 and owner=$2 and key=$3`, tenant, owner, key).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	return raw, err
}
func (j *Postgres) StateValues(ctx context.Context, tenant, owner string) (map[string]json.RawMessage, error) {
	rows, err := j.pool.Query(ctx, `select key,image from runtime_values where tenant=$1 and owner=$2`, tenant, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]json.RawMessage{}
	for rows.Next() {
		var key string
		var raw []byte
		if err = rows.Scan(&key, &raw); err != nil {
			return nil, err
		}
		out[key] = raw
	}
	return out, rows.Err()
}

// Read candidate IDs in bounded pages. A result committed beyond the working
// set's applied position stays a candidate regardless of its future value;
// the owner reads its immutable applied image and filters that image instead.
func (j *Postgres) StateCandidates(ctx context.Context, tenant, typ string, filters []StateFilter, after string, limit int, applied int64) ([]string, error) {
	args := []any{tenant, typ, after, limit, applied}
	conditions := []string{}
	for _, f := range filters {
		choices := []string{}
		for _, value := range f.Values {
			var contains any = value
			if f.List {
				contains = []string{value}
			}
			for i := len(f.Path) - 1; i >= 0; i-- {
				contains = map[string]any{f.Path[i]: contains}
			}
			raw, _ := json.Marshal(contains)
			args = append(args, raw)
			choices = append(choices, fmt.Sprintf(`query @> $%d::jsonb`, len(args)))
		}
		if !f.List {
			for _, value := range f.Values {
				if value == "" {
					args = append(args, f.Path)
					choices = append(choices, fmt.Sprintf(`query #> $%d::text[] is null`, len(args)))
					break
				}
			}
		}
		if len(choices) == 0 {
			conditions = append(conditions, "false")
		} else {
			conditions = append(conditions, "("+strings.Join(choices, " or ")+")")
		}
	}
	where := "true"
	if len(conditions) > 0 {
		where = strings.Join(conditions, " and ")
	}
	rows, err := j.pool.Query(ctx, `select id from runtime_records where tenant=$1 and type=$2 and id>$3 and (`+where+` or seq>$5 or query ? 'projection') order by id limit $4`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
