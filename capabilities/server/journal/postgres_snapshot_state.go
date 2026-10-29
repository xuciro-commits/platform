package journal

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	pgx "github.com/jackc/pgx/v5"
	"platformserver/platform"
	"slices"
)

type StateSnapshotStorage interface {
	SaveStateSnapshot(context.Context, string, int64, string, []byte, StateDigest) error
	BeginSnapshotStateRead(context.Context, string, int64, int64) (StateReadView, error)
}

type StateSnapshotVerifier interface {
	VerifyStateSnapshot(context.Context, string, int64, int64, []StateDelta) error
}

var stateSnapshotSchema = []string{
	`create table if not exists runtime_snapshot_meta (tenant text not null,snapshot_seq bigint not null,unique_version integer not null,primary key(tenant,snapshot_seq),foreign key(tenant,snapshot_seq) references snapshots(tenant,seq) on delete cascade)`,
	`create table if not exists runtime_snapshot_records (tenant text not null,snapshot_seq bigint not null,type text not null,id text not null,image bytea not null,query jsonb not null,history_count integer not null,history_head text not null,seq bigint not null,primary key(tenant,snapshot_seq,type,id),foreign key(tenant,snapshot_seq) references runtime_snapshot_meta on delete cascade)`,
	`create table if not exists runtime_snapshot_unique (tenant text not null,snapshot_seq bigint not null,type text not null,id text not null,rule text not null,key text not null,key_hash bytea not null,seq bigint not null,primary key(tenant,snapshot_seq,type,id,rule),foreign key(tenant,snapshot_seq) references runtime_snapshot_meta on delete cascade)`,
	`create index if not exists runtime_snapshot_unique_key on runtime_snapshot_unique(tenant,snapshot_seq,type,rule,key_hash,id)`,
	`create table if not exists runtime_snapshot_values (tenant text not null,snapshot_seq bigint not null,owner text not null,key text not null,image bytea not null,query jsonb not null,seq bigint not null,primary key(tenant,snapshot_seq,owner,key),foreign key(tenant,snapshot_seq) references runtime_snapshot_meta on delete cascade)`,
	`create table if not exists runtime_snapshot_changes (tenant text not null,snapshot_seq bigint not null,app text not null,num integer not null,primary key(tenant,snapshot_seq,app),foreign key(tenant,snapshot_seq) references runtime_snapshot_meta on delete cascade)`,
}

func (j *Postgres) SaveStateSnapshot(ctx context.Context, tenant string, position int64, code string, state []byte, expected StateDigest) error {
	packed, err := packSnapshot(state)
	if err != nil {
		return err
	}
	tx, err := j.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var actual int64
	if err = tx.QueryRow(ctx, `select seq from runtime_tenants where tenant=$1 for update`, tenant).Scan(&actual); err != nil {
		return err
	}
	if actual != position {
		return fmt.Errorf("tenant %s checkpoint is at %d, current state at %d", tenant, position, actual)
	}
	digest, err := stateCheckpoint(ctx, tx, tenant, nil)
	if err != nil {
		return err
	}
	if digest != expected {
		return fmt.Errorf("tenant %s checkpoint state changed during capture", tenant)
	}
	var version int
	if err = tx.QueryRow(ctx, `select version from runtime_unique_formats where tenant=$1`, tenant).Scan(&version); err != nil {
		return err
	}
	if version != uniqueStateVersion {
		return fmt.Errorf("tenant %s unique checkpoint format differs", tenant)
	}
	if _, err = tx.Exec(ctx, `insert into snapshots(tenant,seq,code,state) values($1,$2,$3,$4) on conflict(tenant,seq) do update set code=excluded.code,state=excluded.state where not exists(select 1 from runtime_snapshot_meta m where m.tenant=excluded.tenant and m.snapshot_seq=excluded.seq)`, tenant, position, code, packed); err != nil {
		return err
	}
	var exists bool
	if err = tx.QueryRow(ctx, `select exists(select 1 from runtime_snapshot_meta where tenant=$1 and snapshot_seq=$2)`, tenant, position).Scan(&exists); err != nil {
		return err
	}
	if exists {
		prior, readErr := stateCheckpoint(ctx, tx, tenant, &position)
		if readErr != nil {
			return readErr
		}
		if prior != expected {
			return fmt.Errorf("tenant %s retained SQL checkpoint differs", tenant)
		}
		if _, err = tx.Exec(ctx, `update snapshots set code=$3,state=$4 where tenant=$1 and seq=$2`, tenant, position, code, packed); err != nil {
			return err
		}
	} else {
		if _, err = tx.Exec(ctx, `insert into runtime_snapshot_meta(tenant,snapshot_seq,unique_version) values($1,$2,$3)`, tenant, position, version); err != nil {
			return err
		}
		for _, query := range []string{
			`insert into runtime_snapshot_records select tenant,$2,type,id,image,query,history_count,history_head,seq from runtime_records where tenant=$1`,
			`insert into runtime_snapshot_unique select tenant,$2,type,id,rule,key,key_hash,seq from runtime_unique where tenant=$1`,
			`insert into runtime_snapshot_values select tenant,$2,owner,key,image,query,seq from runtime_values where tenant=$1`,
			`insert into runtime_snapshot_changes select tenant,$2,app,max(num) from runtime_changes where tenant=$1 group by tenant,app`,
		} {
			if _, err = tx.Exec(ctx, query, tenant, position); err != nil {
				return err
			}
		}
	}
	if err = verifyCheckpointHistory(ctx, tx, tenant, position); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `delete from snapshots where tenant=$1 and seq<(select min(seq) from(select seq from snapshots where tenant=$1 order by seq desc limit 2) newest)`, tenant); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type postgresSnapshotRead struct {
	*postgresStateRead
	snapshot int64
}

func (j *Postgres) BeginSnapshotStateRead(ctx context.Context, tenant string, position, applied int64) (StateReadView, error) {
	read, err := j.BeginStateRead(ctx, tenant, applied)
	if err != nil {
		return nil, err
	}
	base := read.(*postgresStateRead)
	view := &postgresSnapshotRead{postgresStateRead: base, snapshot: position}
	_, err = readState(base, ctx, tenant, func(db stateQueries) (bool, error) {
		var version int
		if err := db.QueryRow(ctx, `select unique_version from runtime_snapshot_meta where tenant=$1 and snapshot_seq=$2`, tenant, position).Scan(&version); err != nil {
			return false, err
		}
		if position > applied || version != uniqueStateVersion {
			return false, fmt.Errorf("tenant %s SQL checkpoint position or format differs", tenant)
		}
		return true, verifyCheckpointHistory(ctx, db, tenant, position)
	})
	if err != nil {
		_ = view.Close()
		return nil, err
	}
	base.position = position
	return view, nil
}

func (v *postgresSnapshotRead) Checkpoint(ctx context.Context, tenant string) (StateDigest, error) {
	return readState(v.postgresStateRead, ctx, tenant, func(db stateQueries) (StateDigest, error) { return stateCheckpoint(ctx, db, tenant, &v.snapshot) })
}
func (v *postgresSnapshotRead) StateCurrent(ctx context.Context, tenant, typ, id string) (StateCurrent, error) {
	return readState(v.postgresStateRead, ctx, tenant, func(db stateQueries) (StateCurrent, error) {
		var r StateCurrent
		err := db.QueryRow(ctx, `select image,history_count,history_head,seq from runtime_snapshot_records where tenant=$1 and snapshot_seq=$2 and type=$3 and id=$4`, tenant, v.snapshot, typ, id).Scan(&r.Image, &r.HistoryCount, &r.HistoryHead, &r.Position)
		if errors.Is(err, pgx.ErrNoRows) {
			err = nil
		}
		return r, err
	})
}
func (v *postgresSnapshotRead) StateRecord(ctx context.Context, tenant, typ, id string) (json.RawMessage, error) {
	r, err := v.StateCurrent(ctx, tenant, typ, id)
	return r.Image, err
}
func (v *postgresSnapshotRead) StateCandidates(ctx context.Context, tenant, typ string, _ []StateFilter, after string, limit int, applied int64) ([]string, error) {
	return readState(v.postgresStateRead, ctx, tenant, func(db stateQueries) ([]string, error) {
		if applied != v.snapshot {
			return nil, fmt.Errorf("SQL checkpoint candidate position differs")
		}
		rows, err := db.Query(ctx, `select id from runtime_snapshot_records where tenant=$1 and snapshot_seq=$2 and type=$3 and id>$4 order by id limit $5`, tenant, v.snapshot, typ, after, limit)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var ids []string
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				return nil, err
			}
			ids = append(ids, id)
		}
		return ids, rows.Err()
	})
}
func (v *postgresSnapshotRead) StateUnique(ctx context.Context, tenant, typ, rule, key string, applied int64) ([]string, error) {
	return readState(v.postgresStateRead, ctx, tenant, func(db stateQueries) ([]string, error) {
		if applied != v.snapshot {
			return nil, fmt.Errorf("SQL checkpoint unique position differs")
		}
		h := sha256.Sum256([]byte(key))
		rows, err := db.Query(ctx, `select id from runtime_snapshot_unique where tenant=$1 and snapshot_seq=$2 and type=$3 and rule=$4 and key_hash=$5 and key=$6 order by id`, tenant, v.snapshot, typ, rule, h[:], key)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var ids []string
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				return nil, err
			}
			ids = append(ids, id)
		}
		return ids, rows.Err()
	})
}
func (v *postgresSnapshotRead) StateValue(ctx context.Context, tenant, owner, key string, through int64) (json.RawMessage, error) {
	return readState(v.postgresStateRead, ctx, tenant, func(db stateQueries) (json.RawMessage, error) {
		if through >= 0 && through != v.snapshot {
			return nil, fmt.Errorf("SQL checkpoint value position differs")
		}
		var raw []byte
		err := db.QueryRow(ctx, `select image from runtime_snapshot_values where tenant=$1 and snapshot_seq=$2 and owner=$3 and key=$4`, tenant, v.snapshot, owner, key).Scan(&raw)
		if errors.Is(err, pgx.ErrNoRows) {
			err = nil
		}
		return json.RawMessage(raw), err
	})
}
func (v *postgresSnapshotRead) StateValues(ctx context.Context, tenant, owner string, through int64) (map[string]json.RawMessage, error) {
	return readState(v.postgresStateRead, ctx, tenant, func(db stateQueries) (map[string]json.RawMessage, error) {
		if through >= 0 && through != v.snapshot {
			return nil, fmt.Errorf("SQL checkpoint values position differs")
		}
		rows, err := db.Query(ctx, `select key,image from runtime_snapshot_values where tenant=$1 and snapshot_seq=$2 and owner=$3 order by key`, tenant, v.snapshot, owner)
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
	})
}

func verifyCheckpointHistory(ctx context.Context, db stateQueries, tenant string, position int64) error {
	rows, err := db.Query(ctx, `select r.type,r.id,r.history_count,r.history_head,h.pos,h.image from runtime_snapshot_records r left join runtime_history h on h.tenant=r.tenant and h.type=r.type and h.id=r.id and h.pos<r.history_count where r.tenant=$1 and r.snapshot_seq=$2 order by r.type,r.id,h.pos`, tenant, position)
	if err != nil {
		return err
	}
	defer rows.Close()
	var previousType, previousID, expectedHead string
	var count, seen int
	head := platform.EmptyRecordHistory
	started := false
	finish := func() error {
		if started && (seen != count || head != expectedHead) {
			return fmt.Errorf("SQL checkpoint history differs: %s/%s", previousType, previousID)
		}
		return nil
	}
	for rows.Next() {
		var typ, id, storedHead string
		var n int
		var pos *int
		var image []byte
		if err = rows.Scan(&typ, &id, &n, &storedHead, &pos, &image); err != nil {
			return err
		}
		if !started || typ != previousType || id != previousID {
			if err = finish(); err != nil {
				return err
			}
			previousType, previousID, expectedHead, count, seen, head, started = typ, id, storedHead, n, 0, platform.EmptyRecordHistory, true
		}
		if pos == nil {
			continue
		}
		if *pos != seen {
			return fmt.Errorf("SQL checkpoint history has a missing entry: %s/%s", typ, id)
		}
		head, err = platform.AppendRecordHistory(head, seen, image)
		if err != nil {
			return err
		}
		seen++
	}
	if err = rows.Err(); err != nil {
		return err
	}
	return finish()
}

func (j *Postgres) VerifyStateSnapshot(ctx context.Context, tenant string, snapshot, position int64, deltas []StateDelta) error {
	tx, err := j.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var actual int64
	if err = tx.QueryRow(ctx, `select seq from runtime_tenants where tenant=$1`, tenant).Scan(&actual); err != nil {
		return err
	}
	if actual != position {
		return fmt.Errorf("tenant %s SQL checkpoint tail position changed", tenant)
	}
	for _, query := range []string{
		`create temporary table expected_records on commit drop as select type,id,image,query,history_count,history_head from runtime_snapshot_records where tenant=$1 and snapshot_seq=$2`,
		`create temporary table expected_unique on commit drop as select type,id,rule,key from runtime_snapshot_unique where tenant=$1 and snapshot_seq=$2`,
		`create temporary table expected_values on commit drop as select owner,key,image,query from runtime_snapshot_values where tenant=$1 and snapshot_seq=$2`,
		`create temporary table expected_changes on commit drop as select c.app,c.id,c.key,c.num,c.image from runtime_changes c join runtime_snapshot_changes p on p.tenant=c.tenant and p.app=c.app and c.num<=p.num where p.tenant=$1 and p.snapshot_seq=$2`,
	} {
		if _, err = tx.Exec(ctx, query, tenant, snapshot); err != nil {
			return err
		}
	}
	for _, query := range []string{`create unique index on expected_records(type,id)`, `create unique index on expected_values(owner,key)`, `create unique index on expected_changes(app,id)`} {
		if _, err = tx.Exec(ctx, query); err != nil {
			return err
		}
	}
	for _, delta := range deltas {
		if delta.LegacyUnique {
			return fmt.Errorf("SQL checkpoint tail has no saved unique-key changes")
		}
		for _, r := range delta.Records {
			if err = verifyTailRecord(ctx, tx, tenant, r); err != nil {
				return err
			}
		}
		for _, replacement := range delta.Replacements {
			var image []byte
			if err = tx.QueryRow(ctx, `select image from expected_records where type=$1 and id=$2`, replacement.Type, replacement.ID).Scan(&image); err != nil {
				return err
			}
			replaced, replaceErr := ReplaceStateRecordValue(image, replacement)
			if replaceErr != nil {
				return replaceErr
			}
			if _, err = tx.Exec(ctx, `update expected_records set image=$3,query=$4::jsonb where type=$1 and id=$2`, replacement.Type, replacement.ID, []byte(replaced), string(replaced)); err != nil {
				return err
			}
		}
		for _, r := range delta.Unique {
			if _, err = tx.Exec(ctx, `delete from expected_unique where type=$1 and id=$2`, r.Type, r.ID); err != nil {
				return err
			}
			for _, rule := range slices.Sorted(func(yield func(string) bool) {
				for name := range r.Keys {
					if !yield(name) {
						return
					}
				}
			}) {
				if _, err = tx.Exec(ctx, `insert into expected_unique values($1,$2,$3,$4)`, r.Type, r.ID, rule, r.Keys[rule]); err != nil {
					return err
				}
			}
		}
		for _, v := range delta.Values {
			if _, err = tx.Exec(ctx, `insert into expected_values values($1,$2,$3,$4) on conflict(owner,key) do update set image=excluded.image,query=excluded.query`, v.Owner, v.Key, []byte(v.Image), v.Image); err != nil {
				return err
			}
		}
		for _, update := range delta.ValueUpdates {
			var image []byte
			if err = tx.QueryRow(ctx, `select image from expected_values where owner=$1 and key=$2`, update.Owner, update.Key).Scan(&image); err != nil {
				return err
			}
			updated, updateErr := update.Apply(image)
			if updateErr != nil {
				return updateErr
			}
			if _, err = tx.Exec(ctx, `update expected_values set image=$3,query=$4::jsonb where owner=$1 and key=$2`, update.Owner, update.Key, []byte(updated), string(updated)); err != nil {
				return err
			}
		}
		for _, c := range delta.Changes {
			var saved []byte
			var key string
			var num int
			if err = tx.QueryRow(ctx, `select image,key,num from runtime_changes where tenant=$1 and app=$2 and id=$3`, tenant, c.App, c.ID).Scan(&saved, &key, &num); err != nil {
				return err
			}
			if key != c.Key || !bytes.Equal(saved, c.Image) {
				return fmt.Errorf("SQL checkpoint tail receipt differs: %s/%s", c.App, c.ID)
			}
			var next int
			if err = tx.QueryRow(ctx, `select coalesce(max(num),0)+1 from expected_changes where app=$1`, c.App).Scan(&next); err != nil {
				return err
			}
			if next != num || c.Index > 0 && c.Index != num {
				return fmt.Errorf("SQL checkpoint tail receipt position differs: %s/%s", c.App, c.ID)
			}
			if _, err = tx.Exec(ctx, `insert into expected_changes values($1,$2,$3,$4,$5)`, c.App, c.ID, c.Key, num, saved); err != nil {
				return err
			}
		}
	}
	for _, query := range []string{
		`select exists((select type,id,image,query,history_count,history_head from runtime_records where tenant=$1 except select * from expected_records) union all (select * from expected_records except select type,id,image,query,history_count,history_head from runtime_records where tenant=$1))`,
		`select exists((select type,id,rule,key from runtime_unique where tenant=$1 except select * from expected_unique) union all (select * from expected_unique except select type,id,rule,key from runtime_unique where tenant=$1))`,
		`select exists((select owner,key,image,query from runtime_values where tenant=$1 except select * from expected_values) union all (select * from expected_values except select owner,key,image,query from runtime_values where tenant=$1))`,
		`select exists((select app,id,key,num,image from runtime_changes where tenant=$1 except select * from expected_changes) union all (select * from expected_changes except select app,id,key,num,image from runtime_changes where tenant=$1))`,
	} {
		var different bool
		if err = tx.QueryRow(ctx, query, tenant).Scan(&different); err != nil {
			return err
		}
		if different {
			return fmt.Errorf("tenant %s current SQL state differs from checkpoint and accepted tail", tenant)
		}
	}
	return tx.Commit(ctx)
}

func verifyTailRecord(ctx context.Context, tx pgx.Tx, tenant string, r StateRecord) error {
	var count int
	var head string
	err := tx.QueryRow(ctx, `select history_count,history_head from expected_records where type=$1 and id=$2`, r.Type, r.ID).Scan(&count, &head)
	if errors.Is(err, pgx.ErrNoRows) {
		head = platform.EmptyRecordHistory
		err = nil
	}
	if err != nil {
		return err
	}
	if count == 0 && head == "" {
		head = platform.EmptyRecordHistory
	}
	base, changes := r.HistoryBase, r.History
	if base < 0 {
		if len(changes) < count {
			return fmt.Errorf("checkpoint history shrank")
		}
		changes = changes[count:]
		base = count
	}
	if base != count || r.HistoryPrior != "" && r.HistoryPrior != head {
		return fmt.Errorf("checkpoint tail predecessor differs: %s/%s", r.Type, r.ID)
	}
	for i, image := range changes {
		var stored []byte
		if err = tx.QueryRow(ctx, `select image from runtime_history where tenant=$1 and type=$2 and id=$3 and pos=$4`, tenant, r.Type, r.ID, count+i).Scan(&stored); err != nil {
			return err
		}
		if !bytes.Equal(stored, image) {
			return fmt.Errorf("checkpoint tail history differs: %s/%s", r.Type, r.ID)
		}
		head, err = platform.AppendRecordHistory(head, count+i, image)
		if err != nil {
			return err
		}
	}
	if r.HistoryHead != "" && head != r.HistoryHead {
		return fmt.Errorf("checkpoint tail history head differs")
	}
	_, err = tx.Exec(ctx, `insert into expected_records values($1,$2,$3,$4,$5,$6) on conflict(type,id) do update set image=excluded.image,query=excluded.query,history_count=excluded.history_count,history_head=excluded.history_head`, r.Type, r.ID, []byte(r.Image), r.Image, count+len(changes), head)
	return err
}
