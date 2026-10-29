package journal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"platformserver/platform"
	"reflect"
	"testing"
	"time"
)

func TestPostgresCheckpointArchiveAndTail(t *testing.T) {
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
	tenant := fmt.Sprintf("checkpoint-%d", time.Now().UnixNano())
	defer func() {
		for _, table := range []string{"snapshots", "runtime_unique", "runtime_unique_formats", "runtime_history", "runtime_records", "runtime_values", "runtime_changes", "runtime_tenants"} {
			_, _ = j.pool.Exec(ctx, "delete from "+table+" where tenant=$1", tenant)
		}
	}()
	write := func(position int64, d StateDelta) {
		t.Helper()
		tx, err := j.pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if _, err = tx.Exec(ctx, `insert into runtime_tenants(tenant,seq) values($1,$2) on conflict(tenant) do update set seq=excluded.seq`, tenant, position); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, `insert into runtime_unique_formats values($1,$2) on conflict(tenant) do nothing`, tenant, uniqueStateVersion); err != nil {
			t.Fatal(err)
		}
		if err = j.writeState(ctx, tx, tenant, position, d); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	first := json.RawMessage(`{"revision":1}`)
	oldImage := json.RawMessage(`{"name":"old"}`)
	initial := StateDelta{Records: []StateRecord{{Type: "item", ID: "a", Image: oldImage, HistoryBase: 0, History: []json.RawMessage{first}}, {Type: "item", ID: "b", Image: json.RawMessage(`{"name":"unchanged"}`), HistoryBase: 0}}, Unique: []StateUnique{{Type: "item", ID: "a", Keys: map[string]string{"name": "old"}}}, Values: []StateValue{{Owner: "owner", Key: "one", Image: oldImage}}, Changes: []StateChange{{App: "owner", ID: "change1", Key: "key1", Image: []byte("original-receipt")}}}
	for i := range 512 {
		initial.Values = append(initial.Values, StateValue{Owner: "answer", Key: fmt.Sprintf("ordinary-%04d", i), Image: json.RawMessage(`{"receipt":"ordinary"}`)})
	}
	business := json.RawMessage(`{"business":[{"code":"review"}],"receipt":"original"}`)
	initial.Values = append(initial.Values, StateValue{Owner: "answer", Key: "business", Image: business})
	write(1, initial)
	current, err := j.BeginStateRead(ctx, tenant, 1)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := current.Checkpoint(ctx, tenant)
	if err != nil {
		t.Fatal(err)
	}
	before, err := current.StateCurrent(ctx, tenant, "item", "a")
	if err != nil {
		t.Fatal(err)
	}
	selected, err := current.(StateValueSelection).StateValuesWithField(ctx, tenant, "answer", "business", 1)
	if err != nil || len(selected) != 1 || !bytes.Equal(selected["business"], business) {
		t.Fatal("selection loaded unrelated ordinary answers", len(selected), err)
	}
	current.Close()
	// The original portable checkpoint at the same position is upgraded once.
	if err = j.SaveSnapshot(ctx, tenant, 1, "code", []byte(`{"portable":true}`)); err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"owner":"first","runtime":"checkpoint"}`)
	if err = j.SaveStateSnapshot(ctx, tenant, 1, "code", payload, digest); err != nil {
		t.Fatal(err)
	}
	_, saved, ok, err := j.Snapshot(ctx, tenant, "code")
	if err != nil || !ok || !bytes.Equal(saved, payload) {
		t.Fatal("portable checkpoint not replaced", string(saved), err)
	}
	if err = j.SaveStateSnapshot(ctx, tenant, 1, "new-format", payload, digest); err != nil {
		t.Fatal(err)
	}
	if _, _, ok, err = j.Snapshot(ctx, tenant, "new-format"); err != nil || !ok {
		t.Fatal("unchanged archive did not refresh owner checkpoint eligibility", err)
	}
	second := json.RawMessage(`{"revision":2}`)
	head, err := platform.AppendRecordHistory(before.HistoryHead, 1, second)
	if err != nil {
		t.Fatal(err)
	}
	newImage := json.RawMessage(`{"name":"new"}`)
	tail := StateDelta{Records: []StateRecord{{Type: "item", ID: "a", Image: newImage, HistoryBase: 1, HistoryPrior: before.HistoryHead, HistoryHead: head, History: []json.RawMessage{second}}, {Type: "item", ID: "c", Image: newImage, HistoryBase: 0}}, Unique: []StateUnique{{Type: "item", ID: "a", Keys: map[string]string{"name": "new"}}}, Values: []StateValue{{Owner: "owner", Key: "one", Image: newImage}}, Changes: []StateChange{{App: "owner", ID: "change2", Key: "key2", Image: []byte("receipt")}}}
	tail.Values = append(tail.Values, StateValue{Owner: "answer", Key: "business", Image: json.RawMessage(`{"receipt":"new"}`)})
	write(2, tail)
	archive, err := j.BeginSnapshotStateRead(ctx, tenant, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	old, err := archive.StateCurrent(ctx, tenant, "item", "a")
	if err != nil || !bytes.Equal(old.Image, oldImage) || old.HistoryCount != 1 || archive.Position() != 1 {
		t.Fatal("archive read current image", old, err)
	}
	ids, err := archive.StateCandidates(ctx, tenant, "item", nil, "", 256, 1)
	if err != nil || !reflect.DeepEqual(ids, []string{"a", "b"}) {
		t.Fatal(ids, err)
	}
	ids, err = archive.StateUnique(ctx, tenant, "item", "name", "old", 1)
	if err != nil || !reflect.DeepEqual(ids, []string{"a"}) {
		t.Fatal(ids, err)
	}
	value, err := archive.StateValue(ctx, tenant, "owner", "one", -1)
	if err != nil || !bytes.Equal(value, oldImage) {
		t.Fatal(string(value), err)
	}
	selected, err = archive.(StateValueSelection).StateValuesWithField(ctx, tenant, "answer", "business", 1)
	if err != nil || len(selected) != 1 || !bytes.Equal(selected["business"], business) {
		t.Fatal("selection escaped its archived view", len(selected), err)
	}
	currentSelection, err := j.StateValuesWithField(ctx, tenant, "answer", "business", 2)
	if err != nil || len(currentSelection) != 0 {
		t.Fatal("current selection returned a removed field", err)
	}
	actual, err := archive.Checkpoint(ctx, tenant)
	if err != nil || actual != digest {
		t.Fatal(actual, digest, err)
	}
	if err = j.VerifyStateSnapshot(ctx, tenant, 1, 2, []StateDelta{tail}); err != nil {
		t.Fatal(err)
	}
	if err = j.SaveStateSnapshot(ctx, tenant, 1, "code", payload, digest); err == nil {
		t.Fatal("stale capture accepted after commit")
	}
	if _, err = j.pool.Exec(ctx, `insert into runtime_changes values($1,'owner','extra','extra',3,$2)`, tenant, []byte("extra")); err != nil {
		t.Fatal(err)
	}
	if err = j.VerifyStateSnapshot(ctx, tenant, 1, 2, []StateDelta{tail}); err == nil {
		t.Fatal("unrelated extra receipt accepted")
	}
	if _, err = j.pool.Exec(ctx, `delete from runtime_changes where tenant=$1 and id='extra'`, tenant); err != nil {
		t.Fatal(err)
	}
	if _, err = j.pool.Exec(ctx, `update runtime_changes set image=$2 where tenant=$1 and id='change1'`, tenant, []byte("corrupt")); err != nil {
		t.Fatal(err)
	}
	corrupt, err := j.BeginSnapshotStateRead(ctx, tenant, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	badDigest, err := corrupt.Checkpoint(ctx, tenant)
	corrupt.Close()
	if err != nil || badDigest == digest {
		t.Fatal("receipt prefix mutation not detected", err)
	}
	if _, err = j.pool.Exec(ctx, `update runtime_changes set image=$2 where tenant=$1 and id='change1'`, tenant, []byte("original-receipt")); err != nil {
		t.Fatal(err)
	}
	// A comparison never repairs or overwrites differing formal SQL state.
	bad := []byte(`{"name":"corrupt"}`)
	if _, err = j.pool.Exec(ctx, `update runtime_records set image=$2 where tenant=$1 and type='item' and id='a'`, tenant, bad); err != nil {
		t.Fatal(err)
	}
	if err = j.VerifyStateSnapshot(ctx, tenant, 1, 2, []StateDelta{tail}); err == nil {
		t.Fatal("corrupt final image accepted")
	}
	kept, err := j.StateRecord(ctx, tenant, "item", "a")
	if err != nil || !bytes.Equal(kept, bad) {
		t.Fatal("verification overwrote mismatch", string(kept), err)
	}
	if _, err = j.pool.Exec(ctx, `update runtime_records set image=$2 where tenant=$1 and type='item' and id='a'`, tenant, []byte(newImage)); err != nil {
		t.Fatal(err)
	}
	if _, err = j.pool.Exec(ctx, `update runtime_history set image=$2 where tenant=$1 and type='item' and id='a' and pos=0`, tenant, []byte(`{"revision":9}`)); err != nil {
		t.Fatal(err)
	}
	if broken, err := j.BeginSnapshotStateRead(ctx, tenant, 1, 2); err == nil {
		broken.Close()
		t.Fatal("corrupt shared prefix accepted")
	}
	if _, err = j.pool.Exec(ctx, `update runtime_history set image=$2 where tenant=$1 and type='item' and id='a' and pos=0`, tenant, []byte(first)); err != nil {
		t.Fatal(err)
	}
	// Retention is the original two snapshots, with archive rows cascading.
	for position := int64(3); position <= 4; position++ {
		write(position, StateDelta{})
		view, err := j.BeginStateRead(ctx, tenant, position)
		if err != nil {
			t.Fatal(err)
		}
		d, err := view.Checkpoint(ctx, tenant)
		view.Close()
		if err != nil {
			t.Fatal(err)
		}
		if err = j.SaveStateSnapshot(ctx, tenant, position, "code", payload, d); err != nil {
			t.Fatal(err)
		}
	}
	var retained int
	if err = j.pool.QueryRow(ctx, `select count(*) from runtime_snapshot_meta where tenant=$1`, tenant).Scan(&retained); err != nil || retained != 2 {
		t.Fatal("archive retention", retained, err)
	}
	if stale, err := j.BeginSnapshotStateRead(ctx, tenant, 1, 4); err == nil {
		stale.Close()
		t.Fatal("pruned snapshot still available")
	}
	old, err = archive.StateCurrent(ctx, tenant, "item", "a")
	if err != nil || !bytes.Equal(old.Image, oldImage) {
		t.Fatal("existing snapshot transaction lost old archive", old, err)
	}
}
