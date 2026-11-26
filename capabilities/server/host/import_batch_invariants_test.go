package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"platformserver/kernel/protocol"
	persistence "platformserver/kernel/store"
	"platformserver/platform"
	"platformserver/services/ontology"
	build "platformserver/services/resources/authoring"
)

func TestInvariantK1K2ImportBatchMixedRowsRollbackAndRetry(t *testing.T) {
	for _, mode := range []string{"memory", "file", "postgres"} {
		t.Run(mode, func(t *testing.T) {
			url := os.Getenv("PLATFORM_BENCH_DATABASE")
			if mode == "postgres" && url == "" {
				t.Skip("requires isolated PostgreSQL")
			}
			ctx := t.Context()
			live := newInvariantTenant(t, fmt.Sprintf("import-%s-%d", mode, time.Now().UnixNano()))
			object := invariantObject("item", "Item")
			object["fields"] = []ontology.Field{{Name: "note", Title: "Note", Type: "text", Required: true}}
			live.accept(t, "build.object.create", build.ObjectType, "OBJ1", object)
			live.accept(t, "build.object.publish", build.ObjectType, "OBJ1", map[string]any{})
			const typ = "invariantproject.item"
			var journal persistence.StorageBundle
			if mode != "memory" {
				var err error
				if mode == "file" {
					journal, err = OpenFileJournal(t.TempDir())
				} else {
					journal, err = OpenJournal(ctx, url)
				}
				if err != nil {
					t.Fatal(err)
				}
				defer journal.Close()
				if _, err := journal.Entries(ctx, live.ID, 0); err != nil {
					t.Fatal(err)
				}
				for _, entry := range live.entries {
					id, err := acceptedIdentity(entry.Body)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := journal.AppendAccepted(ctx, live.ID, entry, id.Key, id.Hash); err != nil {
						t.Fatal(err)
					}
				}
				if mode == "postgres" {
					if err := live.MigrateRuntimeState(ctx, journal.Journals, nil, live.runtimeOwners()); err != nil {
						t.Fatal(err)
					}
				}
				live.AttachJournal(context.Background(), journal.Journals)
			}
			position := func() int64 {
				if mode == "memory" {
					return int64(len(live.entries))
				}
				return journal.Position(live.ID)
			}
			base := position()
			csv := []byte("id,note\nA,first\nBAD,\nB,second\nA,updated\n")
			member := live.member(t)
			total := func() int {
				page, refusal := live.Records(ctx, member, typ, platform.Query{Limit: 10}, invariantClock)
				if refusal != nil {
					t.Fatalf("list: %v", refusal)
				}
				return page.Total
			}
			commit := live.AcceptResults
			live.AcceptResults = func([]persistence.AcceptedAppend) ([][]byte, error) {
				return nil, errors.New("injected definite rollback")
			}
			if _, refusal := live.Import(member, typ, csv, false, invariantClock); refusal == nil {
				t.Fatal("expected rollback refusal")
			}
			if position() != base || total() != 0 || live.quarantined() {
				t.Fatal("failed batch changed live tenant")
			}
			live.AcceptResults = commit
			rows, refusal := live.Import(member, typ, csv, false, invariantClock)
			if refusal != nil {
				t.Fatalf("import: %#v", refusal)
			}
			if len(rows) != 4 || rows[0].Outcome != "ok" || rows[1].Outcome == "ok" || rows[2].Outcome != "ok" || rows[3].Outcome != "ok" {
				t.Fatalf("mixed outcomes: %+v", rows)
			}
			if total() != 2 {
				t.Fatal("valid rows did not land")
			}
			after := position()
			retry, refusal := live.Import(member, typ, csv, false, invariantClock)
			if refusal != nil || !reflect.DeepEqual(rows, retry) || position() != after {
				t.Fatalf("retry differs: %+v, %+v, %v", rows, retry, refusal)
			}
			view, refusal := live.RecordOf(ctx, member, typ, "A", invariantClock)
			if refusal != nil || !strings.Contains(string(platform.Raw(view.Record)), "\"note\":\"updated\"") {
				t.Fatalf("sequential edit lost: %v %+v", refusal, view)
			}
			live.accept(t, typ+".create", typ, "C", map[string]any{"note": "after"})
			if total() != 3 || live.quarantined() {
				t.Fatal("tenant not writable after retry")
			}
		})
	}
}

func TestInvariantK1ImportBatchThousandReplay(t *testing.T) {
	ctx := t.Context()
	live := newInvariantTenant(t, fmt.Sprintf("import-replay-%d", time.Now().UnixNano()))
	live.accept(t, "build.object.create", build.ObjectType, "OBJ1", invariantObject("item", "Item"))
	live.accept(t, "build.object.publish", build.ObjectType, "OBJ1", map[string]any{})
	const typ = "invariantproject.item"
	var csv strings.Builder
	csv.WriteString("id,note\n")
	for i := 0; i < 1000; i++ {
		fmt.Fprintf(&csv, "I%04d,row %d\n", i, i)
	}
	rows, refusal := live.Import(live.member(t), typ, []byte(csv.String()), false, invariantClock)
	if refusal != nil || len(rows) != 1000 {
		t.Fatalf("import: %d %+v", len(rows), refusal)
	}
	for _, row := range rows {
		if row.Outcome != "ok" {
			t.Fatalf("row: %+v", row)
		}
	}
	want := live.recordImage(t)
	if diff := differingTypes(want, live.restore(t, live.entries).recordImage(t)); len(diff) > 0 {
		t.Fatalf("memory replay: %v", diff)
	}
	for _, mode := range []string{"file", "postgres"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			open := func() persistence.StorageBundle {
				var journal persistence.StorageBundle
				var err error
				if mode == "file" {
					journal, err = OpenFileJournal(dir)
				} else {
					url := os.Getenv("PLATFORM_BENCH_DATABASE")
					if url == "" {
						t.Skip("requires isolated PostgreSQL")
					}
					journal, err = OpenJournal(ctx, url)
				}
				if err != nil {
					t.Fatal(err)
				}
				return journal
			}
			journal := open()
			defer func() { journal.Close() }()
			if _, err := journal.Entries(ctx, live.ID, 0); err != nil {
				t.Fatal(err)
			}
			if mode == "postgres" {
				fresh := newInvariantTenantSetup(t, live.ID, nil)
				base, err := fresh.RuntimeStateDelta(fresh.runtimeOwners())
				if err != nil {
					t.Fatal(err)
				}
				if err := journal.InitializeState(ctx, live.ID, 0, base); err != nil {
					t.Fatal(err)
				}
			}
			batchJournal, ok := journal.Journals.(persistence.BatchJournal)
			if !ok {
				t.Fatal("missing batch journal")
			}
			half := len(live.entries) / 2
			for start := 0; start < len(live.entries); {
				end := min(start+128, len(live.entries))
				if start < half && end > half {
					end = half
				}
				batch := make([]persistence.AcceptedAppend, 0, end-start)
				for _, entry := range live.entries[start:end] {
					id, err := acceptedIdentity(entry.Body)
					if err != nil {
						t.Fatal(err)
					}
					batch = append(batch, persistence.AcceptedAppend{Entry: entry, Key: id.Key, Hash: id.Hash})
				}
				if _, err := batchJournal.AppendAcceptedBatch(ctx, live.ID, batch); err != nil {
					t.Fatal(err)
				}
				if journal.Position(live.ID) != int64(end) {
					t.Fatal("discontinuous batch position")
				}
				start = end
				if mode == "file" && end == half {
					fresh := live.restore(t, live.entries[:half])
					raw, pos, err := fresh.Snapshot(func() int64 { return int64(half) })
					if err != nil {
						t.Fatal(err)
					}
					if err := journal.SaveSnapshot(ctx, live.ID, pos, "import-test", raw); err != nil {
						t.Fatal(err)
					}
				}
			}
			journal.Close()
			journal = open()
			entries, err := journal.Entries(ctx, live.ID, 0)
			if err != nil || len(entries) != len(live.entries) {
				t.Fatalf("reopen %d: %v", len(entries), err)
			}
			got := live.restore(t, entries).recordImage(t)
			for _, typ := range differingTypes(want, got) {
				var a, b any
				if mode != "postgres" || json.Unmarshal(want[typ], &a) != nil || json.Unmarshal(got[typ], &b) != nil || !reflect.DeepEqual(a, b) {
					t.Fatalf("%s replay differs: %s", mode, typ)
				}
			}
			if mode == "file" {
				pos, raw, found, err := journal.Snapshot(ctx, live.ID, "import-test")
				if err != nil || !found || pos != int64(half) {
					t.Fatalf("snapshot: %d %v", pos, err)
				}
				fresh := newInvariantTenantSetup(t, live.ID, nil)
				if err := fresh.Restore(raw); err != nil {
					t.Fatal(err)
				}
				tail, err := journal.Entries(ctx, live.ID, pos)
				if err != nil {
					t.Fatal(err)
				}
				if err := fresh.recoverEntries(tail); err != nil {
					t.Fatal(err)
				}
				if diff := differingTypes(want, fresh.recordImage(t)); len(diff) > 0 {
					t.Fatalf("snapshot and tail differs: %v", diff)
				}
			}
		})
	}
}

func TestInvariantK4ImportBatchAuthorizesEachRow(t *testing.T) {
	live := newInvariantTenant(t, "import-auth", protocol.Seat{Subjects: []string{"rep"}, Member: platform.Member{ID: "rep", Roles: map[string]string{"invariantproject": "rep"}}})
	object := invariantObject("item", "Item")
	object["access"] = []ontology.Access{{Role: "rep", Read: "own", Create: true, Edit: true}}
	live.accept(t, "build.object.create", build.ObjectType, "OBJ1", object)
	live.accept(t, "build.object.publish", build.ObjectType, "OBJ1", map[string]any{})
	const typ = "invariantproject.item"
	live.accept(t, typ+".create", typ, "THEIRS", map[string]any{"note": "protected"})
	rep, _ := live.Member("rep")
	data := []byte("id,note\nMINE,first\nTHEIRS,denied\nANOTHER,second\n")
	rows, refusal := live.Import(rep, typ, data, false, invariantClock)
	if refusal != nil || len(rows) != 3 || rows[0].Outcome != "ok" || rows[1].Outcome == "ok" || rows[2].Outcome != "ok" {
		t.Fatalf("row authorization: %+v, %v", rows, refusal)
	}
	view, refusal := live.RecordOf(t.Context(), live.member(t), typ, "THEIRS", invariantClock)
	if refusal != nil || !strings.Contains(string(platform.Raw(view.Record)), "\"note\":\"protected\"") {
		t.Fatalf("unauthorized row changed: %+v, %v", view, refusal)
	}
	before := len(live.entries)
	retry, refusal := live.Import(rep, typ, data, false, invariantClock)
	if refusal != nil || !reflect.DeepEqual(rows, retry) || len(live.entries) != before {
		t.Fatalf("retry: %+v, %v", retry, refusal)
	}
}
