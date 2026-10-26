package journal

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"os"
	"reflect"
	"testing"
	"time"
)

func TestPostgresStateReadKeepsOneAppliedView(t *testing.T) {
	dsn := os.Getenv("PLATFORM_TEST_DATABASE")
	if dsn == "" {
		t.Skip("PLATFORM_TEST_DATABASE not set")
	}
	ctx := context.Background()
	j, err := OpenPostgres(ctx, dsn, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	tenant := fmt.Sprintf("state-view-%d", time.Now().UnixNano())
	defer func() {
		for _, table := range []string{"runtime_unique", "runtime_unique_formats", "runtime_records", "runtime_history", "runtime_values", "runtime_changes", "runtime_tenants"} {
			_, _ = j.pool.Exec(ctx, "delete from "+table+" where tenant=$1", tenant)
		}
	}()
	write := func(position int64, image string, extra bool) {
		t.Helper()
		tx, err := j.pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if _, err = tx.Exec(ctx, `insert into runtime_tenants(tenant,seq) values($1,$2) on conflict(tenant) do update set seq=excluded.seq`, tenant, position); err != nil {
			t.Fatal(err)
		}
		d := StateDelta{Records: []StateRecord{{Type: "entry", ID: "a", Image: json.RawMessage(image), HistoryBase: 0}}, Unique: []StateUnique{{Type: "entry", ID: "a", Keys: map[string]string{"code": image}}}, Values: []StateValue{{Owner: "owner", Key: "key", Image: json.RawMessage(image)}}}
		d.Records[0].HistoryBase = int(position - 7)
		d.Records[0].History = []json.RawMessage{json.RawMessage(fmt.Sprintf(`{"revision":%d}`, position))}
		d.Changes = []StateChange{{App: "app", ID: fmt.Sprint(position), Key: fmt.Sprint(position), Image: []byte(image)}}
		if extra {
			d.Records = append(d.Records, StateRecord{Type: "entry", ID: "b", Image: json.RawMessage(image), HistoryBase: 0})
		}
		if err = j.writeState(ctx, tx, tenant, position, d); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, `insert into runtime_unique_formats(tenant,version) values($1,$2) on conflict(tenant) do nothing`, tenant, uniqueStateVersion); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	old := `{"code":"old"}`
	newImage := `{"code":"new"}`
	write(7, old, false)
	view, err := j.BeginStateRead(ctx, tenant, 7)
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	write(8, newImage, true)
	if view.Position() != 7 {
		t.Fatal(view.Position())
	}
	current, err := view.StateCurrent(ctx, tenant, "entry", "a")
	if err != nil || string(current.Image) != old || current.Position != 7 || current.HistoryCount != 1 {
		t.Fatal(current, err)
	}
	history, err := view.StateHistory(ctx, tenant, "entry", "a", 0, 1)
	if err != nil || len(history) != 1 || string(history[0]) != `{"revision":7}` {
		t.Fatal(history, err)
	}
	changes, err := view.StateChanges(ctx, tenant, "app", "", "", 2)
	if err != nil || len(changes) != 1 || string(changes[0]) != old {
		t.Fatal(changes, err)
	}
	filters := []StateFilter{{Path: []string{"code"}, Values: []string{"old"}}}
	ids, err := view.StateCandidates(ctx, tenant, "entry", filters, "", 64, 7)
	if err != nil || !reflect.DeepEqual(ids, []string{"a"}) {
		t.Fatal("snapshot candidates changed", ids, err)
	}
	ids, err = view.StateUnique(ctx, tenant, "entry", "code", old, 7)
	if err != nil || !reflect.DeepEqual(ids, []string{"a"}) {
		t.Fatal("snapshot unique key changed", ids, err)
	}
	value, err := view.StateValue(ctx, tenant, "owner", "key", -1)
	if err != nil || string(value) != old {
		t.Fatal("snapshot owner value changed", string(value), err)
	}
	values, err := view.StateValues(ctx, tenant, "owner", -1)
	if err != nil || string(values["key"]) != old {
		t.Fatal(values, err)
	}
	if _, err = j.BeginStateRead(ctx, tenant, 7); err == nil {
		t.Fatal("unapplied SQL position accepted")
	}
	fresh, err := j.BeginStateRead(ctx, tenant, 8)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := fresh.StateRecord(ctx, tenant, "entry", "a")
	if err != nil || string(actual) != newImage {
		t.Fatal(string(actual), err)
	}
	ids, err = fresh.StateCandidates(ctx, tenant, "entry", filters, "", 64, 8)
	if err != nil || len(ids) != 0 {
		t.Fatal(ids, err)
	}
	// Reading with another position is an error, never an empty substitute view.
	if _, err = fresh.StateCandidates(ctx, tenant, "entry", nil, "", 64, 7); err == nil {
		t.Fatal("candidate position mismatch accepted")
	}
	if _, err = fresh.StateRecord(ctx, tenant, "entry", "a"); err != pgx.ErrTxClosed {
		t.Fatal("failed view remained open", err)
	}
	if err = fresh.Close(); err != nil {
		t.Fatal(err)
	}
	if err = view.Close(); err != nil {
		t.Fatal(err)
	}

	// An in-flight legacy Background query also belongs to its operation.
	operation, stop := context.WithCancel(ctx)
	inflight, err := j.BeginStateRead(operation, tenant, 8)
	if err != nil {
		t.Fatal(err)
	}
	concrete := inflight.(*postgresStateRead)
	pid := concrete.tx.Conn().PgConn().PID()
	queryDone := make(chan error, 1)
	go func() {
		_, err := readState(concrete, ctx, tenant, func(db stateQueries) (bool, error) {
			var unused any
			err := db.QueryRow(ctx, `select pg_sleep(30)`).Scan(&unused)
			return true, err
		})
		queryDone <- err
	}()
	waiting := false
	startedDeadline := time.Now().Add(time.Second)
	for time.Now().Before(startedDeadline) {
		if err = j.pool.QueryRow(ctx, `select coalesce(wait_event='PgSleep',false) from pg_stat_activity where pid=$1`, pid).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !waiting {
		stop()
		t.Fatal("test SQL query did not start")
	}
	stop()
	select {
	case err := <-queryDone:
		if err == nil {
			t.Fatal("operation cancellation ignored by running query")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("operation cancellation waited for SQL query to finish")
	}
	if err = inflight.Close(); err != nil {
		t.Fatal(err)
	}

	// Cancellation releases a transaction even when no further method is called.
	cancelled, cancel := context.WithCancel(ctx)
	abandoned, err := j.BeginStateRead(cancelled, tenant, 8)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	deadline := time.Now().Add(3 * time.Second)
	for j.readPool.Stat().AcquiredConns() != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if n := j.readPool.Stat().AcquiredConns(); n != 0 {
		t.Fatal("cancelled snapshot retained connection", n)
	}
	if err = abandoned.Close(); err != nil {
		t.Fatal(err)
	}
}
