package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	pgx "github.com/jackc/pgx/v5"
)

func TestPostgresAcceptedBatchAtomicRetry(t *testing.T) {
	url := os.Getenv("PLATFORM_BENCH_DATABASE")
	if url == "" {
		t.Skip("requires isolated PostgreSQL")
	}
	ctx := context.Background()
	j, err := OpenPostgres(ctx, url, benchmarkIdentity)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	tenant := fmt.Sprintf("batch-%d", time.Now().UnixNano())
	if _, err := j.Entries(ctx, tenant, 0); err != nil {
		t.Fatal(err)
	}
	if err := j.InitializeState(ctx, tenant, 0, StateDelta{}); err != nil {
		t.Fatal(err)
	}
	j.Delta = func(raw []byte) (StateDelta, error) {
		id, err := benchmarkIdentity(raw)
		if err != nil {
			return StateDelta{}, err
		}
		return StateDelta{Records: []StateRecord{{Type: "object", ID: id.Key, Image: []byte("{}")}}}, nil
	}
	makeBatch := func(prefix string, n int) []AcceptedAppend {
		batch := make([]AcceptedAppend, n)
		for i := range batch {
			key := fmt.Sprintf("%s-%d", prefix, i)
			batch[i] = AcceptedAppend{Entry: benchmarkEntry(tenant, "submission", key, "h"), Key: key, Hash: "h"}
		}
		return batch
	}
	check := func(n int64) {
		t.Helper()
		if j.Position(tenant) != n {
			t.Fatalf("position %d, want %d", j.Position(tenant), n)
		}
		var count, lo, hi, state, records, results int64
		err := j.pool.QueryRow(ctx, "select count(*),coalesce(min(seq),0),coalesce(max(seq),0) from journal where tenant=$1", tenant).Scan(&count, &lo, &hi)
		if err != nil {
			t.Fatal(err)
		}
		if count != n || hi != n || n > 0 && lo != 1 {
			t.Fatalf("journal %d/%d/%d, want %d continuous", count, lo, hi, n)
		}
		err = j.pool.QueryRow(ctx, "select seq,(select count(*) from runtime_records where tenant=$1),(select count(*) from runtime_results where tenant=$1) from runtime_tenants where tenant=$1", tenant).Scan(&state, &records, &results)
		if err != nil {
			t.Fatal(err)
		}
		if state != n || records != n || results != n {
			t.Fatalf("state/records/results %d/%d/%d want %d", state, records, results, n)
		}
	}
	first := makeBatch("first", 3)
	answers, err := j.AppendAcceptedBatch(ctx, tenant, first)
	if err != nil {
		t.Fatal(err)
	}
	check(3)
	retry, err := j.AppendAcceptedBatch(ctx, tenant, first)
	if err != nil || !reflect.DeepEqual(answers, retry) {
		t.Fatalf("retry differs: %v", err)
	}
	check(3)
	rollback := makeBatch("rollback", 128)
	delta := j.Delta
	calls := 0
	injected := errors.New("injected failure at row 64")
	j.Delta = func(raw []byte) (StateDelta, error) {
		calls++
		if calls == 64 {
			return StateDelta{}, injected
		}
		return delta(raw)
	}
	if _, err := j.AppendAcceptedBatch(ctx, tenant, rollback); !errors.Is(err, injected) {
		t.Fatalf("mid-batch failure: %v", err)
	}
	check(3)
	j.Delta = delta
	_, err = j.appendAcceptedBatch(ctx, tenant, rollback, func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.Rollback(ctx); err != nil {
			return err
		}
		return pgx.ErrTxCommitRollback
	})
	if !errors.Is(err, pgx.ErrTxCommitRollback) {
		t.Fatalf("expected rollback: %v", err)
	}
	check(3)
	if _, err := j.AppendAcceptedBatch(ctx, tenant, rollback); err != nil {
		t.Fatal(err)
	}
	check(131)
	unknown := makeBatch("unknown", 128)
	answers, err = j.appendAcceptedBatch(ctx, tenant, unknown, func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return errors.New("injected lost COMMIT response")
	})
	if err != nil {
		t.Fatal(err)
	}
	check(259)
	retry, err = j.AppendAcceptedBatch(ctx, tenant, unknown)
	if err != nil || !reflect.DeepEqual(answers, retry) {
		t.Fatalf("lost response retry differs: %v", err)
	}
	check(259)
	conflict := makeBatch("conflict", 1)
	conflict = append(conflict, AcceptedAppend{Entry: benchmarkEntry(tenant, "submission", first[0].Key, "different"), Key: first[0].Key, Hash: "different"})
	if _, err := j.AppendAcceptedBatch(ctx, tenant, conflict); !errors.Is(err, ErrAcceptedConflict) {
		t.Fatalf("expected conflict: %v", err)
	}
	check(259)
	last := makeBatch("last", 1)
	if _, err := j.AppendAccepted(ctx, tenant, last[0].Entry, last[0].Key, last[0].Hash); err != nil {
		t.Fatal(err)
	}
	check(260)
}
