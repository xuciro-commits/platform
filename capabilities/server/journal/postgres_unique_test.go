package journal

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestPostgresUniqueKeysReplaceAtAppliedPosition(t *testing.T) {
	dsn := os.Getenv("PLATFORM_TEST_DATABASE")
	if dsn == "" {
		t.Skip("PLATFORM_TEST_DATABASE not set")
	}
	ctx := context.Background()
	tenant := fmt.Sprintf("unique-keys-%d", time.Now().UnixNano())
	j, err := OpenPostgres(ctx, dsn, func([]byte) (Identity, error) { return Identity{}, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		j.pool.Exec(ctx, `delete from runtime_unique where tenant=$1`, tenant)
		j.pool.Exec(ctx, `delete from runtime_unique_formats where tenant=$1`, tenant)
		j.pool.Exec(ctx, `delete from runtime_tenants where tenant=$1`, tenant)
		j.Close()
	}()
	// A long composite key is indexed by digest while exact bytes decide lookup.
	key := `["` + strings.Repeat("value", 10000) + `",0,false]`
	tx, err := j.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `insert into runtime_tenants(tenant,seq) values($1,7)`, tenant); err != nil {
		t.Fatal(err)
	}
	initial := []StateUnique{{Type: "entry", ID: "a", Keys: map[string]string{"code": key}}, {Type: "entry", ID: "b", Keys: map[string]string{"code": key}}}
	if err = j.writeUnique(ctx, tx, tenant, 7, initial); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := j.StateUnique(ctx, tenant, "entry", "code", key, 7); err == nil {
		t.Fatal("uninitialized derived keys were used for business checks")
	}
	tx, err = j.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = j.compareUnique(ctx, tx, tenant, 7, initial); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if ids, err := j.StateUnique(ctx, tenant, "entry", "code", key, 7); err != nil || !reflect.DeepEqual(ids, []string{"a", "b"}) {
		t.Fatal("historical duplicates were lost", ids, err)
	}
	changed := []StateUnique{{Type: "entry", ID: "a", Keys: map[string]string{"code": "new"}}, {Type: "entry", ID: "b"}}
	tx, err = j.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = j.writeUnique(ctx, tx, tenant, 8, changed); err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if ids, err := j.StateUnique(ctx, tenant, "entry", "code", key, 7); err != nil || len(ids) != 2 {
		t.Fatal("failed private transaction published keys", ids, err)
	}
	tx, err = j.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = j.writeUnique(ctx, tx, tenant, 8, changed); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `update runtime_tenants set seq=8 where tenant=$1`, tenant); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := j.StateUnique(ctx, tenant, "entry", "code", key, 7); err == nil {
		t.Fatal("unapplied newer database view was treated as empty old view")
	}
	if ids, err := j.StateUnique(ctx, tenant, "entry", "code", key, 8); err != nil || len(ids) != 0 {
		t.Fatal("key replacement retained old occupants", ids, err)
	}
	if ids, err := j.StateUnique(ctx, tenant, "entry", "code", "new", 8); err != nil || !reflect.DeepEqual(ids, []string{"a"}) {
		t.Fatal("new key not readable", ids, err)
	}
	tx, err = j.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = j.compareUnique(ctx, tx, tenant, 8, changed); err != nil {
		t.Fatal("matching existing keys did not compare", err)
	}
	if _, err = tx.Exec(ctx, `update runtime_unique set key='corrupt' where tenant=$1`, tenant); err != nil {
		t.Fatal(err)
	}
	if err = j.compareUnique(ctx, tx, tenant, 8, changed); err == nil {
		t.Fatal("existing derived corruption was silently repaired")
	}
	var corrupt string
	if err = tx.QueryRow(ctx, `select key from runtime_unique where tenant=$1`, tenant).Scan(&corrupt); err != nil || corrupt != "corrupt" {
		t.Fatal("comparison rewrote differing derived data", corrupt, err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	tx, err = j.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `update runtime_unique_formats set version=2 where tenant=$1`, tenant); err != nil {
		t.Fatal(err)
	}
	if err = j.compareUnique(ctx, tx, tenant, 8, changed); err == nil {
		t.Fatal("unknown derived format was overwritten")
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

}
