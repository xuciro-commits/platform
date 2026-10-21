package journal

import (
	"context"
	"encoding/json"
	"fmt"
)

// StateValueSelection narrows saved envelopes by a top-level field. The owner
// still decodes the original bytes and decides what the saved facts mean.
type StateValueSelection interface {
	StateValuesWithField(context.Context, string, string, string, int64) (map[string]json.RawMessage, error)
}

var valueSelectionSchema = []string{
	`create index if not exists runtime_values_business on runtime_values(tenant,owner,key) where query ? 'business'`,
	`create index if not exists runtime_snapshot_values_business on runtime_snapshot_values(tenant,snapshot_seq,owner,key) where query ? 'business'`,
	`create index if not exists runtime_values_active on runtime_values(tenant,owner,key) where query ? 'active'`,
	`create index if not exists runtime_snapshot_values_active on runtime_snapshot_values(tenant,snapshot_seq,owner,key) where query ? 'active'`,
}

func (j *Postgres) StateValuesWithField(ctx context.Context, tenant, owner, field string, through int64) (map[string]json.RawMessage, error) {
	return stateValuesWithField(ctx, j.pool, tenant, owner, field, through, nil)
}

func (v *postgresStateRead) StateValuesWithField(ctx context.Context, tenant, owner, field string, through int64) (map[string]json.RawMessage, error) {
	return readState(v, ctx, tenant, func(db stateQueries) (map[string]json.RawMessage, error) {
		if through >= 0 && through != v.position {
			return nil, fmt.Errorf("selected owner-values position differs from read view")
		}
		return stateValuesWithField(ctx, db, tenant, owner, field, through, nil)
	})
}

func (v *postgresSnapshotRead) StateValuesWithField(ctx context.Context, tenant, owner, field string, through int64) (map[string]json.RawMessage, error) {
	return readState(v.postgresStateRead, ctx, tenant, func(db stateQueries) (map[string]json.RawMessage, error) {
		if through >= 0 && through != v.snapshot {
			return nil, fmt.Errorf("selected checkpoint-values position differs")
		}
		return stateValuesWithField(ctx, db, tenant, owner, field, through, &v.snapshot)
	})
}

func stateValuesWithField(ctx context.Context, db stateQueries, tenant, owner, field string, through int64, snapshot *int64) (map[string]json.RawMessage, error) {
	table, suffix := "runtime_values", ""
	args := []any{tenant, owner, through, field}
	if snapshot != nil {
		table, suffix = "runtime_snapshot_values", " and snapshot_seq=$5"
		args = append(args, *snapshot)
	}
	rows, err := db.Query(ctx, `select key,image from `+table+` where tenant=$1 and owner=$2 and ($3::bigint<0 or seq<=$3) and query ? $4`+suffix+` order by key`, args...)
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
