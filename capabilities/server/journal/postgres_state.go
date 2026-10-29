package journal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	pgx "github.com/jackc/pgx/v5"
	"platformserver/platform"
	"strings"
)

var stateSchema = []string{

	`create table if not exists runtime_tenants (tenant text primary key, seq bigint not null)`,
	`create table if not exists runtime_records (tenant text not null, type text not null, id text not null, image bytea not null, query jsonb not null, history_count integer not null, seq bigint not null default 0, primary key(tenant,type,id))`,
	`alter table runtime_records add column if not exists seq bigint not null default 0`,
	`alter table runtime_records add column if not exists history_head text not null default ''`,
	`create index if not exists runtime_records_query on runtime_records using gin (query)`,
	`create index if not exists runtime_records_position on runtime_records (tenant,type,seq,id)`,
	`create table if not exists runtime_history (tenant text not null, type text not null, id text not null, pos integer not null, image bytea not null, query jsonb not null, primary key(tenant,type,id,pos))`,
	`create table if not exists runtime_changes (tenant text not null, app text not null, id text not null, key text not null, num integer not null, image bytea not null, primary key(tenant,app,id), unique(tenant,app,key), unique(tenant,app,num))`,
	`create table if not exists runtime_values (tenant text not null, owner text not null, key text not null, image bytea not null, query jsonb not null, primary key(tenant,owner,key))`,
	`alter table runtime_values add column if not exists seq bigint not null default 0`,
	`create table if not exists runtime_results (tenant text not null, app text not null, scope text not null, key text not null, hash text not null, seq bigint not null, body bytea not null, primary key(tenant,app,scope,key))`,
}

func init() {
	stateSchema = append(stateSchema, uniqueStateSchema...)
	stateSchema = append(stateSchema, stateSnapshotSchema...)
	stateSchema = append(stateSchema, valueSelectionSchema...)
	stateSchema = append(stateSchema, `alter table runtime_tenants add column if not exists format integer not null default 1`)
}

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
		if err = j.writeState(ctx, tx, tenant, position, delta); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `insert into runtime_results (tenant,app,scope,key,hash,seq,body)
   select tenant,app,`+acceptedScopeSQL+`,coalesce(body->>'key',body#>>'{submission,idempotencyKey}',body#>>'{receipt,submission,idempotencyKey}'),body->>'requestHash',seq,convert_to(body::text,'UTF8') from journal where tenant=$1 and kind='accepted-result'`, tenant); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `insert into runtime_tenants (tenant,seq,format) values ($1,$2,3)`, tenant, position); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if saved != position {
		return fmt.Errorf("tenant %s SQL state is at %d, journal at %d", tenant, saved, position)
	}
	if err = j.upgradeStateInstallations(ctx, tx, tenant, position, delta); err != nil {
		return err
	}
	if err = j.compareState(ctx, tx, tenant, position, delta); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (j *Postgres) StatePosition(ctx context.Context, tenant string) (int64, bool, error) {
	var position int64
	err := j.pool.QueryRow(ctx, `select seq from runtime_tenants where tenant=$1`, tenant).Scan(&position)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	return position, err == nil, err
}

func (j *Postgres) RestoreState(ctx context.Context, tenant string, position int64, prefix StateCheckpoint, full StateDelta) error {
	tx, err := j.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext($1),63)`, tenant); err != nil {
		return err
	}
	var latest, saved int64
	if err = tx.QueryRow(ctx, `select coalesce(max(seq),0) from journal where tenant=$1`, tenant).Scan(&latest); err != nil {
		return err
	}
	if latest != position {
		return fmt.Errorf("tenant %s changed during state recovery: %d/%d", tenant, latest, position)
	}
	if err = tx.QueryRow(ctx, `select seq from runtime_tenants where tenant=$1 for update`, tenant).Scan(&saved); err != nil {
		return err
	}
	if saved == position {
		if err = j.upgradeStateInstallations(ctx, tx, tenant, position, full); err != nil {
			return err
		}
		if err = j.compareState(ctx, tx, tenant, position, full); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if saved != prefix.Position || saved < 0 || saved >= position {
		return fmt.Errorf("tenant %s SQL recovery predecessor differs: %d/%d/%d", tenant, saved, prefix.Position, position)
	}
	if err = j.upgradeStateInstallations(ctx, tx, tenant, prefix.Position, prefix.Delta); err != nil {
		return err
	}
	if err = j.compareState(ctx, tx, tenant, prefix.Position, prefix.Delta); err != nil {
		return fmt.Errorf("SQL recovery prefix: %w", err)
	}
	if j.Delta == nil {
		return fmt.Errorf("tenant %s has no state delta owner", tenant)
	}
	type retainedResult struct {
		position  int64
		app, kind string
		raw       []byte
	}
	rows, err := tx.Query(ctx, `select seq,app,kind,body from journal where tenant=$1 and seq>$2 and seq<=$3 order by seq`, tenant, saved, position)
	if err != nil {
		return err
	}
	var suffix []retainedResult
	for rows.Next() {
		var result retainedResult
		if err = rows.Scan(&result.position, &result.app, &result.kind, &result.raw); err != nil {
			rows.Close()
			return err
		}
		suffix = append(suffix, result)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	next := saved + 1
	legacyKeys := false
	for _, result := range suffix {
		if result.position != next || result.kind != "accepted-result" {
			return fmt.Errorf("tenant %s recovery requires a contiguous accepted-result at %d", tenant, next)
		}
		identity, err := j.identify(result.raw)
		if err != nil || identity.Tenant != tenant || identity.App != result.app || identity.Key == "" || identity.Hash == "" {
			return fmt.Errorf("tenant %s invalid retained result at %d: %v", tenant, next, err)
		}
		delta, err := j.Delta(result.raw)
		if err != nil {
			return fmt.Errorf("tenant %s retained state delta at %d: %w", tenant, next, err)
		}
		legacyKeys = legacyKeys || delta.LegacyUnique
		if err = j.writeState(ctx, tx, tenant, next, delta); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `insert into runtime_results (tenant,app,scope,key,hash,seq,body) values ($1,$2,$3,$4,$5,$6,$7)`, tenant, identity.App, identity.Scope, identity.Key, identity.Hash, next, result.raw); err != nil {
			return err
		}
		next++
	}
	if next != position+1 {
		return fmt.Errorf("tenant %s recovery suffix is incomplete", tenant)
	}
	if err = j.compareStateImage(ctx, tx, tenant, position, full, legacyKeys); err != nil {
		return fmt.Errorf("SQL recovered state: %w", err)
	}
	if err = tx.QueryRow(ctx, `select coalesce(max(seq),0) from journal where tenant=$1`, tenant).Scan(&latest); err != nil {
		return err
	}
	if latest != position {
		return fmt.Errorf("tenant %s changed during state recovery: %d/%d", tenant, latest, position)
	}
	if _, err = tx.Exec(ctx, `update runtime_tenants set seq=$2 where tenant=$1`, tenant, position); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (j *Postgres) compareState(ctx context.Context, tx pgx.Tx, tenant string, position int64, d StateDelta) error {
	return j.compareStateImage(ctx, tx, tenant, position, d, false)
}

func (j *Postgres) compareStateImage(ctx context.Context, tx pgx.Tx, tenant string, position int64, d StateDelta, rebuildUnique bool) error {
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
		var storedHead string
		var original []byte
		if err := tx.QueryRow(ctx, `select query=$4::jsonb,history_count,history_head,image from runtime_records where tenant=$1 and type=$2 and id=$3`, tenant, r.Type, r.ID, []byte(r.Image)).Scan(&equal, &n, &storedHead, &original); err != nil {
			return err
		}
		if !equal || !sameJSON(original, r.Image) || n != len(r.History) {
			return fmt.Errorf("tenant %s state differs for %s/%s", tenant, r.Type, r.ID)
		}
		rows, err := tx.Query(ctx, `select pos, query from runtime_history where tenant=$1 and type=$2 and id=$3 order by pos`, tenant, r.Type, r.ID)
		if err != nil {
			return err
		}
		at := 0
		head := platform.EmptyRecordHistory
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
			head, err = platform.AppendRecordHistory(head, at, raw)
			if err != nil {
				rows.Close()
				return err
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
		if storedHead != "" && storedHead != head || r.HistoryHead != "" && r.HistoryHead != head {
			return fmt.Errorf("tenant %s history chain differs for %s/%s", tenant, r.Type, r.ID)
		}
		if storedHead == "" {
			if _, err = tx.Exec(ctx, `update runtime_records set history_head=$4 where tenant=$1 and type=$2 and id=$3`, tenant, r.Type, r.ID, head); err != nil {
				return err
			}
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
	if rebuildUnique {
		if _, err := tx.Exec(ctx, `delete from runtime_unique_formats where tenant=$1`, tenant); err != nil {
			return err
		}
	}
	return j.compareUnique(ctx, tx, tenant, position, d.Unique)
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
		var head string
		err := tx.QueryRow(ctx, `select history_count,history_head from runtime_records where tenant=$1 and type=$2 and id=$3`, tenant, r.Type, r.ID).Scan(&n, &head)
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
		if n == 0 && head == "" {
			head = platform.EmptyRecordHistory
		}
		if head == "" {
			if r.HistoryPrior != "" {
				return fmt.Errorf("record history head was not initialized")
			}
			head = platform.EmptyRecordHistory
			prefix, err := tx.Query(ctx, `select image from runtime_history where tenant=$1 and type=$2 and id=$3 order by pos`, tenant, r.Type, r.ID)
			if err != nil {
				return err
			}
			at := 0
			for prefix.Next() {
				var raw []byte
				if err = prefix.Scan(&raw); err != nil {
					prefix.Close()
					return err
				}
				head, err = platform.AppendRecordHistory(head, at, raw)
				if err != nil {
					prefix.Close()
					return err
				}
				at++
			}
			err = prefix.Err()
			prefix.Close()
			if err != nil {
				return err
			}
			if at != n {
				return fmt.Errorf("record history prefix is incomplete")
			}
		}
		if r.HistoryPrior != "" && head != r.HistoryPrior {
			return fmt.Errorf("record history chain predecessor differs")
		}
		for i, image := range h {
			head, err = platform.AppendRecordHistory(head, base+i, image)
			if err != nil {
				return err
			}
		}
		if r.HistoryHead != "" && head != r.HistoryHead {
			return fmt.Errorf("record history appended chain differs")
		}
		for i, image := range h {
			if _, err = tx.Exec(ctx, `insert into runtime_history (tenant,type,id,pos,image,query) values ($1,$2,$3,$4,$5,$6::jsonb)`, tenant, r.Type, r.ID, base+i, []byte(image), string(image)); err != nil {
				return err
			}
		}
		if _, err = tx.Exec(ctx, `insert into runtime_records (tenant,type,id,image,query,history_count,seq,history_head) values ($1,$2,$3,$4,$5::jsonb,$6,$7,$8) on conflict(tenant,type,id) do update set image=excluded.image,query=excluded.query,history_count=excluded.history_count,seq=excluded.seq,history_head=excluded.history_head`, tenant, r.Type, r.ID, []byte(r.Image), string(r.Image), base+len(h), position, head); err != nil {
			return err
		}
	}
	for _, replacement := range d.Replacements {
		var image []byte
		if err := tx.QueryRow(ctx, `select image from runtime_records where tenant=$1 and type=$2 and id=$3`, tenant, replacement.Type, replacement.ID).Scan(&image); err != nil {
			return err
		}
		replaced, err := ReplaceStateRecordValue(image, replacement)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `update runtime_records set image=$4,query=$5::jsonb,seq=$6 where tenant=$1 and type=$2 and id=$3`, tenant, replacement.Type, replacement.ID, []byte(replaced), string(replaced), position); err != nil {
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
		if _, err := tx.Exec(ctx, `insert into runtime_values (tenant,owner,key,image,query,seq) values ($1,$2,$3,$4,$5::jsonb,$6) on conflict(tenant,owner,key) do update set image=excluded.image,query=excluded.query,seq=excluded.seq`, tenant, v.Owner, v.Key, []byte(v.Image), string(v.Image), position); err != nil {
			return err
		}
	}
	for _, update := range d.ValueUpdates {
		var image []byte
		if err := tx.QueryRow(ctx, `select image from runtime_values where tenant=$1 and owner=$2 and key=$3`, tenant, update.Owner, update.Key).Scan(&image); err != nil {
			return err
		}
		updated, err := update.Apply(image)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `update runtime_values set image=$4,query=$5::jsonb,seq=$6 where tenant=$1 and owner=$2 and key=$3`, tenant, update.Owner, update.Key, []byte(updated), string(updated), position); err != nil {
			return err
		}
	}
	return j.writeUnique(ctx, tx, tenant, position, d.Unique)
}

func (j *Postgres) StateRecord(ctx context.Context, tenant, typ, id string) (json.RawMessage, error) {
	return stateRecord(ctx, j.pool, tenant, typ, id)
}

func stateRecord(ctx context.Context, db stateQueries, tenant, typ, id string) (json.RawMessage, error) {
	current, err := stateCurrent(ctx, db, tenant, typ, id)
	return current.Image, err
}

func stateCurrent(ctx context.Context, db stateQueries, tenant, typ, id string) (StateCurrent, error) {
	var current StateCurrent
	err := db.QueryRow(ctx, `select image,history_count,history_head,seq from runtime_records where tenant=$1 and type=$2 and id=$3`, tenant, typ, id).Scan(&current.Image, &current.HistoryCount, &current.HistoryHead, &current.Position)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	return current, err
}
func (j *Postgres) StateHistory(ctx context.Context, tenant, typ, id string, after, through int) ([]json.RawMessage, error) {
	return stateHistory(ctx, j.pool, tenant, typ, id, after, through)
}

func stateHistory(ctx context.Context, db stateQueries, tenant, typ, id string, after, through int) ([]json.RawMessage, error) {
	rows, err := db.Query(ctx, `select pos,image from runtime_history where tenant=$1 and type=$2 and id=$3 and pos>=$4 and pos<$5 order by pos`, tenant, typ, id, after, through)
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
	return stateChanges(ctx, j.pool, tenant, app, key, id, through)
}

func stateChanges(ctx context.Context, db stateQueries, tenant, app, key, id string, through int) ([][]byte, error) {
	rows, err := db.Query(ctx, `select image from runtime_changes where tenant=$1 and app=$2 and num<=$3 and ($4='' or key=$4) and ($5='' or id=$5) order by num`, tenant, app, through, key, id)
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
func (j *Postgres) StateValue(ctx context.Context, tenant, owner, key string, through int64) (json.RawMessage, error) {
	return stateValue(ctx, j.pool, tenant, owner, key, through)
}

func stateValue(ctx context.Context, db stateQueries, tenant, owner, key string, through int64) (json.RawMessage, error) {
	var raw []byte
	err := db.QueryRow(ctx, `select image from runtime_values where tenant=$1 and owner=$2 and key=$3 and ($4<0 or seq<=$4)`, tenant, owner, key, through).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	return raw, err
}
func (j *Postgres) StateValues(ctx context.Context, tenant, owner string, through int64) (map[string]json.RawMessage, error) {
	return stateValues(ctx, j.pool, tenant, owner, through)
}

func stateValues(ctx context.Context, db stateQueries, tenant, owner string, through int64) (map[string]json.RawMessage, error) {
	rows, err := db.Query(ctx, `select key,image from runtime_values where tenant=$1 and owner=$2 and ($3<0 or seq<=$3)`, tenant, owner, through)
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

func (j *Postgres) StateCandidates(ctx context.Context, tenant, typ string, filters []StateFilter, after string, limit int, applied int64) ([]string, error) {
	return stateCandidates(ctx, j.pool, tenant, typ, filters, after, limit, applied)
}

func stateCandidates(ctx context.Context, db stateQueries, tenant, typ string, filters []StateFilter, after string, limit int, applied int64) ([]string, error) {
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
	rows, err := db.Query(ctx, `select id from runtime_records where tenant=$1 and type=$2 and id>$3 and (`+where+` or ($5::bigint >= 0 and seq>$5) or query ? 'projection') order by id limit $4`, args...)
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
