package journal

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"

	"github.com/jackc/pgx/v5"
)

const uniqueStateVersion = 1

var uniqueStateSchema = []string{
	`create table if not exists runtime_unique_formats (tenant text primary key, version integer not null)`,
	`create table if not exists runtime_unique (tenant text not null, type text not null, id text not null, rule text not null, key text not null, key_hash bytea not null, seq bigint not null, primary key(tenant,type,id,rule))`,
	`create index if not exists runtime_unique_key on runtime_unique (tenant,type,rule,key_hash,id)`,
}

// writeUnique is part of the original accepted-result transaction. Keys are
// already checked by their owner; this table is a lookup, not a second judge.
func (j *Postgres) writeUnique(ctx context.Context, tx pgx.Tx, tenant string, position int64, replacements []StateUnique) error {
	for _, row := range replacements {
		if _, err := tx.Exec(ctx, `delete from runtime_unique where tenant=$1 and type=$2 and id=$3`, tenant, row.Type, row.ID); err != nil {
			return err
		}
		for _, rule := range slices.Sorted(maps.Keys(row.Keys)) {
			key := row.Keys[rule]
			digest := sha256.Sum256([]byte(key))
			if _, err := tx.Exec(ctx, `insert into runtime_unique(tenant,type,id,rule,key,key_hash,seq) values($1,$2,$3,$4,$5,$6,$7)`, tenant, row.Type, row.ID, rule, key, digest[:], position); err != nil {
				return err
			}
		}
	}
	return nil
}

// Filtering out newer keys would lose an old occupant whose key was replaced.
// Check the exact applied position in the same database view as the lookup.
func (j *Postgres) StateUnique(ctx context.Context, tenant, typ, rule, key string, applied int64) ([]string, error) {
	tx, err := j.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	ids, err := stateUnique(ctx, tx, tenant, typ, rule, key, applied)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return ids, nil
}

func stateUnique(ctx context.Context, db stateQueries, tenant, typ, rule, key string, applied int64) ([]string, error) {
	var position int64
	var version *int
	if err := db.QueryRow(ctx, `select t.seq,f.version from runtime_tenants t left join runtime_unique_formats f on f.tenant=t.tenant where t.tenant=$1`, tenant).Scan(&position, &version); err != nil {
		return nil, err
	}
	if version == nil || *version != uniqueStateVersion {
		return nil, fmt.Errorf("tenant %s unique-key format is unavailable", tenant)
	}
	if position != applied {
		return nil, fmt.Errorf("tenant %s unique state is at %d, applied at %d", tenant, position, applied)
	}
	digest := sha256.Sum256([]byte(key))
	rows, err := db.Query(ctx, `select id from runtime_unique where tenant=$1 and type=$2 and rule=$3 and key_hash=$4 and key=$5 order by id`, tenant, typ, rule, digest[:], key)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// compareUnique runs only after the owner images, history and position have
// already been verified by compareState. A missing format marks a first
// derivation; an existing format must match and is never silently repaired.
func (j *Postgres) compareUnique(ctx context.Context, tx pgx.Tx, tenant string, position int64, complete []StateUnique) error {
	var version int
	err := tx.QueryRow(ctx, `select version from runtime_unique_formats where tenant=$1 for update`, tenant).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		if _, err = tx.Exec(ctx, `delete from runtime_unique where tenant=$1`, tenant); err != nil {
			return err
		}
		if err = j.writeUnique(ctx, tx, tenant, position, complete); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `insert into runtime_unique_formats(tenant,version) values($1,$2)`, tenant, uniqueStateVersion)
		return err
	}
	if err != nil {
		return err
	}
	if version != uniqueStateVersion {
		return fmt.Errorf("tenant %s unique-key format %d is unsupported", tenant, version)
	}
	expected := 0
	for _, row := range complete {
		expected += len(row.Keys)
		if len(row.Keys) == 0 {
			continue
		}
		rows, err := tx.Query(ctx, `select rule,key,key_hash,seq from runtime_unique where tenant=$1 and type=$2 and id=$3 order by rule`, tenant, row.Type, row.ID)
		if err != nil {
			return err
		}
		actual := map[string]string{}
		for rows.Next() {
			var rule, key string
			var digest []byte
			var seq int64
			if err = rows.Scan(&rule, &key, &digest, &seq); err != nil {
				rows.Close()
				return err
			}
			expectedDigest := sha256.Sum256([]byte(key))
			if !bytes.Equal(digest, expectedDigest[:]) || seq > position {
				rows.Close()
				return fmt.Errorf("tenant %s derived unique key index differs for %s/%s", tenant, row.Type, row.ID)
			}
			actual[rule] = key
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(actual, row.Keys) {
			return fmt.Errorf("tenant %s derived unique keys differ for %s/%s", tenant, row.Type, row.ID)
		}
	}
	var actual int
	if err = tx.QueryRow(ctx, `select count(*) from runtime_unique where tenant=$1`, tenant).Scan(&actual); err != nil {
		return err
	}
	if actual != expected {
		return fmt.Errorf("tenant %s derived unique key count differs: %d/%d", tenant, actual, expected)
	}
	return nil
}
