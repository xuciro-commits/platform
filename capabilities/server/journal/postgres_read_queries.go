package journal

import (
	"context"
	"github.com/jackc/pgx/v5"
)

// Each query observes both its caller's cancellation and the operation that
// owns the fixed view, including callers using the legacy Background helpers.
type stateReadQueries struct {
	tx        pgx.Tx
	operation context.Context
}

func (q stateReadQueries) queryContext(ctx context.Context) (context.Context, func()) {
	if q.operation == nil || q.operation.Done() == nil {
		return ctx, func() {}
	}
	query, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(q.operation, cancel)
	return query, func() { stop(); cancel() }
}
func (q stateReadQueries) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	ctx, done := q.queryContext(ctx)
	rows, err := q.tx.Query(ctx, sql, args...)
	if err != nil {
		done()
		return nil, err
	}
	return stateReadRows{Rows: rows, done: done}, nil
}
func (q stateReadQueries) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	ctx, done := q.queryContext(ctx)
	return stateReadRow{Row: q.tx.QueryRow(ctx, sql, args...), done: done}
}

type stateReadRows struct {
	pgx.Rows
	done func()
}

func (r stateReadRows) Close() { r.Rows.Close(); r.done() }
func (r stateReadRows) Next() bool {
	next := r.Rows.Next()
	if !next {
		r.done()
	}
	return next
}

type stateReadRow struct {
	pgx.Row
	done func()
}

func (r stateReadRow) Scan(dest ...any) error { defer r.done(); return r.Row.Scan(dest...) }
