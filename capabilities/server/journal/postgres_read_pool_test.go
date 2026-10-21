package journal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"
)

func TestPostgresStateReadDoesNotExhaustAcceptanceConnections(t *testing.T) {
	dsn := os.Getenv("PLATFORM_TEST_DATABASE")
	if dsn == "" {
		t.Skip("PLATFORM_TEST_DATABASE not set")
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Scheme == "postgres" || parsed.Scheme == "postgresql" {
		options := parsed.Query()
		options.Set("pool_max_conns", "1")
		parsed.RawQuery = options.Encode()
		dsn = parsed.String()
	} else {
		dsn += " pool_max_conns=1"
	}
	type input struct {
		Tenant string `json:"tenant"`
		App    string `json:"app"`
		Kind   string `json:"kind"`
		Key    string `json:"key"`
		Hash   string `json:"requestHash"`
	}
	identify := func(raw []byte) (Identity, error) {
		var in input
		err := json.Unmarshal(raw, &in)
		return Identity{Tenant: in.Tenant, App: in.App, Scope: "input", Key: in.Key, Hash: in.Hash}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	j, err := OpenPostgres(ctx, dsn, identify)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	defer j.Close()
	defer cancel()
	if j.pool.Config().MaxConns != 1 || j.readPool.Config().MaxConns != 1 {
		t.Fatal("test did not constrain both original and read pools")
	}
	tenants := []string{fmt.Sprintf("read-pool-a-%d", time.Now().UnixNano()), fmt.Sprintf("read-pool-b-%d", time.Now().UnixNano())}
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 3*time.Second)
		defer done()
		for _, tenant := range tenants {
			for _, table := range []string{"runtime_results", "runtime_records", "runtime_history", "runtime_unique", "runtime_unique_formats", "runtime_tenants", "journal"} {
				_, _ = j.pool.Exec(cleanup, "delete from "+table+" where tenant=$1", tenant)
			}
		}
	}()
	for _, tenant := range tenants {
		if _, err = j.Entries(ctx, tenant, 0); err != nil {
			t.Fatal(err)
		}
		if err = j.InitializeState(ctx, tenant, 0, StateDelta{}); err != nil {
			t.Fatal(err)
		}
	}
	j.Delta = func([]byte) (StateDelta, error) {
		return StateDelta{Records: []StateRecord{{Type: "entry", ID: "new", Image: json.RawMessage(`{"value":{"id":"new"}}`), HistoryBase: 0}}}, nil
	}
	start := make(chan struct{})
	done := make(chan error, len(tenants))
	for _, tenant := range tenants {
		go func() {
			<-start
			view, err := j.BeginStateRead(ctx, tenant, 0)
			if err != nil {
				done <- err
				return
			}
			defer view.Close()
			in := input{Tenant: tenant, App: "test", Kind: "input-result", Key: "one", Hash: "one"}
			raw, _ := json.Marshal(in)
			entry := Entry{App: in.App, Kind: "accepted-result", Principal: json.RawMessage(`{"id":"test"}`), Body: raw, At: time.Now().UTC()}
			if _, err = j.AppendAccepted(ctx, tenant, entry, in.Key, in.Hash); err != nil {
				done <- err
				return
			}
			image, err := view.StateRecord(ctx, tenant, "entry", "new")
			if err == nil && len(image) != 0 {
				err = errors.New("acceptance changed its original fixed read view")
			}
			done <- err
		}()
	}
	close(start)
	for range tenants {
		if err := <-done; err != nil {
			t.Fatal("a writer could not append while retaining its read view", err)
		}
	}
	for _, tenant := range tenants {
		if j.Position(tenant) != 1 {
			t.Fatal("tenant did not advance", tenant)
		}
	}
	// Saturating the read pool queues another read, but cancelling that acquire
	// does not consume a connection or prevent the next tenant's operation.
	held, err := j.BeginStateRead(ctx, tenants[0], 1)
	if err != nil {
		t.Fatal(err)
	}
	acquire, cancelAcquire := context.WithCancel(ctx)
	waiting := make(chan error, 1)
	go func() {
		view, err := j.BeginStateRead(acquire, tenants[1], 1)
		if view != nil {
			_ = view.Close()
		}
		waiting <- err
	}()
	select {
	case err := <-waiting:
		t.Fatalf("read unexpectedly bypassed its single connection: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	cancelAcquire()
	if err := <-waiting; !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled read acquisition", err)
	}
	if err = held.Close(); err != nil {
		t.Fatal(err)
	}
	next, err := j.BeginStateRead(ctx, tenants[1], 1)
	if err != nil {
		t.Fatal(err)
	}
	if err = next.Close(); err != nil {
		t.Fatal(err)
	}
	if j.readPool.Stat().AcquiredConns() != 0 || j.pool.Stat().AcquiredConns() != 0 {
		t.Fatal("completed operations retained database connections")
	}
}
