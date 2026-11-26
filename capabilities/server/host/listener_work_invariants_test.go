package host

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"platformserver/kernel/acceptedformat"
	persistence "platformserver/kernel/store"
	"platformserver/services/ontology"
	build "platformserver/services/resources/authoring"
)

func TestInvariantK1BuildListenerWorkResultKeepsEvidenceOnce(t *testing.T) {
	live := newInvariantTenant(t, "listener-"+fmt.Sprint(time.Now().UnixNano()))
	const typ = "invariantproject.unit"
	rows := make([]map[string]any, 200)
	for i := range rows {
		rows[i] = map[string]any{"id": fmt.Sprintf("unit-%03d", i), "title": fmt.Sprintf("Unit %d", i)}
	}
	live.accept(t, "build.object.create", build.ObjectType, "UNIT", map[string]any{"name": "unit", "title": "Unit", "display": "title", "fields": []ontology.Field{{Name: "title", Title: "Title", Type: "text", Required: true}}, "records": rows})
	live.accept(t, "build.object.publish", build.ObjectType, "UNIT", nil)
	var journal persistence.StorageBundle
	ctx := t.Context()
	if url := os.Getenv("PLATFORM_BENCH_DATABASE"); url != "" {
		var err error
		journal, err = OpenJournal(ctx, url)
		if err != nil {
			t.Fatal(err)
		}
		defer journal.Close()
		if _, err = journal.Entries(ctx, live.ID, 0); err != nil {
			t.Fatal(err)
		}
		for _, entry := range live.entries {
			id, err := acceptedIdentity(entry.Body)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = journal.AppendAccepted(ctx, live.ID, entry, id.Key, id.Hash); err != nil {
				t.Fatal(err)
			}
		}
		if err = live.MigrateRuntimeState(ctx, journal.Journals, nil, live.runtimeOwners()); err != nil {
			t.Fatal(err)
		}
		live.AttachJournal(context.Background(), journal.Journals)
	}
	commit := live.AcceptResult
	workCount, total, maxBytes := 0, 0, 0
	var commitTime time.Duration
	live.AcceptResult = func(e persistence.Entry, key, hash string) ([]byte, error) {
		w, decodeErr := acceptedformat.DecodeWork(e.Body)
		start := time.Now()
		raw, err := commit(e, key, hash)
		elapsed := time.Since(start)
		if decodeErr == nil && w.App == build.ID {
			if len(w.Changes) > 0 {
				batch, _, err := acceptedformat.DecodeBatch(w.Changes)
				if err != nil {
					t.Fatal(err)
				}
				if len(batch.Authorizations) < 200 || len(w.Authorizations) != 0 {
					t.Fatalf("child evidence must be retained once, child=%d outer=%d", len(batch.Authorizations), len(w.Authorizations))
				}
			}
			workCount++
			total += len(e.Body)
			maxBytes = max(maxBytes, len(e.Body))
			commitTime += elapsed
			t.Logf("build work bytes=%d changes=%d outer_authorizations=%d commit=%s", len(e.Body), len(w.Changes), len(w.Authorizations), elapsed)
		}
		return raw, err
	}
	started := time.Now()
	live.Work(invariantClock)
	t.Logf("listener work=%d total_bytes=%d max_bytes=%d commit_total=%s elapsed=%s", workCount, total, maxBytes, commitTime, time.Since(started))
	if workCount == 0 {
		t.Fatal("no Build listener work accepted")
	}
	for i := range rows {
		id := fmt.Sprintf("unit-%03d", i)
		if _, refusal := live.RecordOf(ctx, live.member(t), typ, id, invariantClock); refusal != nil {
			t.Fatalf("reference %s: %s", id, refusal.Message)
		}
	}
	entries := live.entries
	if journal.Journals != nil {
		var err error
		entries, err = journal.Entries(ctx, live.ID, 0)
		if err != nil {
			t.Fatal(err)
		}
	}
	got, want := live.restore(t, entries).recordImage(t), live.recordImage(t)
	for _, typ := range differingTypes(want, got) {
		var a, b any
		if json.Unmarshal(want[typ], &a) != nil || json.Unmarshal(got[typ], &b) != nil || !reflect.DeepEqual(a, b) {
			t.Fatalf("listener replay differs: %s", typ)
		}
	}
}
