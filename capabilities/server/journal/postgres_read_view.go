package journal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	pgx "github.com/jackc/pgx/v5"

	"sync"
	"time"
)

// Both the legacy pool reads and fixed transaction reads use these queries.
type stateQueries interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}
type postgresStateRead struct {
	mu       sync.Mutex
	tx       pgx.Tx
	ctx      context.Context
	tenant   string
	position int64
	stop     func() bool
	closed   bool
}

func (j *Postgres) BeginStateRead(ctx context.Context, tenant string, expected int64) (StateReadView, error) {
	tx, err := j.readPool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	v := &postgresStateRead{tx: tx, ctx: ctx, tenant: tenant}
	if err = tx.QueryRow(ctx, `select seq from runtime_tenants where tenant=$1`, tenant).Scan(&v.position); err == nil && v.position != expected {
		err = &StatePositionError{Tenant: tenant, Position: v.position, Applied: expected}
	}
	if err != nil {
		_ = v.Close()
		return nil, err
	}
	// pgx does not roll back automatically when its begin context is cancelled.
	v.mu.Lock()
	v.stop = context.AfterFunc(ctx, func() { _ = v.Close() })
	v.mu.Unlock()
	return v, nil
}
func (v *postgresStateRead) Position() int64 { return v.position }
func (v *postgresStateRead) Close() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.closeLocked()
}
func (v *postgresStateRead) closeLocked() error {
	if v.closed {
		return nil
	}
	v.closed = true
	if v.stop != nil {
		v.stop()
	}
	// A cancelled read must still release its transaction and pool connection.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := v.tx.Rollback(ctx)
	if errors.Is(err, pgx.ErrTxClosed) {
		return nil
	}
	return err
}
func readState[T any](v *postgresStateRead, ctx context.Context, tenant string, read func(stateQueries) (T, error)) (T, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	var zero T
	if v.closed {
		return zero, pgx.ErrTxClosed
	}
	if err := ctx.Err(); err != nil {
		_ = v.closeLocked()
		return zero, err
	}
	if tenant != v.tenant {
		_ = v.closeLocked()
		return zero, fmt.Errorf("state read belongs to tenant %s", v.tenant)
	}
	value, err := read(stateReadQueries{tx: v.tx, operation: v.ctx})
	if err != nil {
		_ = v.closeLocked()
	}
	return value, err
}
func (v *postgresStateRead) StateCurrent(ctx context.Context, tenant, typ, id string) (StateCurrent, error) {
	return readState(v, ctx, tenant, func(db stateQueries) (StateCurrent, error) { return stateCurrent(ctx, db, tenant, typ, id) })
}
func (v *postgresStateRead) StateRecord(ctx context.Context, tenant, typ, id string) (json.RawMessage, error) {
	return readState(v, ctx, tenant, func(db stateQueries) (json.RawMessage, error) { return stateRecord(ctx, db, tenant, typ, id) })
}
func (v *postgresStateRead) StateHistory(ctx context.Context, tenant, typ, id string, after, through int) ([]json.RawMessage, error) {
	return readState(v, ctx, tenant, func(db stateQueries) ([]json.RawMessage, error) {
		return stateHistory(ctx, db, tenant, typ, id, after, through)
	})
}
func (v *postgresStateRead) StateChanges(ctx context.Context, tenant, app, key, id string, through int) ([][]byte, error) {
	return readState(v, ctx, tenant, func(db stateQueries) ([][]byte, error) { return stateChanges(ctx, db, tenant, app, key, id, through) })
}
func (v *postgresStateRead) StateValue(ctx context.Context, tenant, owner, key string, through int64) (json.RawMessage, error) {
	return readState(v, ctx, tenant, func(db stateQueries) (json.RawMessage, error) {
		if through >= 0 && through != v.position {
			return nil, fmt.Errorf("owner-value position %d differs from read view %d", through, v.position)
		}
		return stateValue(ctx, db, tenant, owner, key, through)
	})
}
func (v *postgresStateRead) StateValues(ctx context.Context, tenant, owner string, through int64) (map[string]json.RawMessage, error) {
	return readState(v, ctx, tenant, func(db stateQueries) (map[string]json.RawMessage, error) {
		if through >= 0 && through != v.position {
			return nil, fmt.Errorf("owner-values position %d differs from read view %d", through, v.position)
		}
		return stateValues(ctx, db, tenant, owner, through)
	})
}
func (v *postgresStateRead) StateCandidates(ctx context.Context, tenant, typ string, filters []StateFilter, after string, limit int, applied int64) ([]string, error) {
	return readState(v, ctx, tenant, func(db stateQueries) ([]string, error) {
		if applied != v.position {
			return nil, fmt.Errorf("candidate position %d differs from read view %d", applied, v.position)
		}
		return stateCandidates(ctx, db, tenant, typ, filters, after, limit, -1)
	})
}
func (v *postgresStateRead) StateUnique(ctx context.Context, tenant, typ, rule, key string, applied int64) ([]string, error) {
	return readState(v, ctx, tenant, func(db stateQueries) ([]string, error) {
		if applied != v.position {
			return nil, fmt.Errorf("unique position %d differs from read view %d", applied, v.position)
		}
		return stateUnique(ctx, db, tenant, typ, rule, key, applied)
	})
}
