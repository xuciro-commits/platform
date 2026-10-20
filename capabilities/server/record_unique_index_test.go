package platformserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"platformserver/internal/records"
	"platformserver/journal"
	"platformserver/platform"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

type indexedUniqueParent struct {
	platform.Record
	Group string `json:"group" type:"reference" ref:"unique.group"`
}
type indexedUniqueChild struct {
	platform.Record
	Parent  string `json:"parent" type:"reference" ref:"unique.parent"`
	Code    string `json:"code"`
	Number  int    `json:"number"`
	Enabled bool   `json:"enabled"`
}

func uniqueIndexStore(t *testing.T) (*recordStore, platform.Constraint) {
	t.Helper()
	rule := platform.Constraint{Name: "code", Kind: "unique", Fields: []string{"code", "number", "enabled"}, Scope: "parent.group"}
	store := newRecordStore()
	for _, entity := range []platform.Entity{{Type: "unique.parent", Model: indexedUniqueParent{}}, {Type: "unique.child", Model: indexedUniqueChild{}, Constraints: []platform.Constraint{rule}}} {
		info, err := platform.Describe("unique", entity, func(reflect.Type) string { return "" })
		if err != nil {
			t.Fatal(err)
		}
		et := &entityType{store: store, info: info}
		store.types[info.Type] = et
		store.byGo[info.Go] = et
	}
	return store, rule
}
func putUniqueTest(store *recordStore, typ string, value any) {
	v := copyOf(reflect.TypeOf(value), value)
	store.types[typ].setRow(records.Header(v).ID, &records.Row{Value: v})
}
func TestUniqueIndexScopeChangesAndPrivateIsolation(t *testing.T) {
	store, rule := uniqueIndexStore(t)
	for i := 1; i <= 3; i++ {
		putUniqueTest(store, "unique.parent", indexedUniqueParent{Record: platform.Record{ID: fmt.Sprint(i)}, Group: fmt.Sprint(i)})
		putUniqueTest(store, "unique.child", indexedUniqueChild{Record: platform.Record{ID: fmt.Sprint(i)}, Parent: fmt.Sprint(i), Code: "same"})
	}
	et := store.types["unique.child"]
	store.prepareUniqueIndexes()
	original := et.uniqueIndexes[rule.Name]
	candidate := indexedUniqueChild{Record: platform.Record{ID: "new"}, Parent: "1", Code: "same"}
	if ok, err := store.uniqueConstraint(et.info, rule, copyOf(reflect.TypeOf(candidate), candidate)); ok || err != nil {
		t.Fatal("zero/false did not occupy scope", ok, err)
	}
	candidate.Code = " "
	if ok, err := store.uniqueConstraint(et.info, rule, copyOf(reflect.TypeOf(candidate), candidate)); !ok || err != nil {
		t.Fatal("blank field occupied a key", ok, err)
	}
	candidate.Code = "same"
	candidate.Archived = true
	if ok, _ := store.uniqueConstraint(et.info, rule, copyOf(reflect.TypeOf(candidate), candidate)); ok {
		t.Fatal("archived record bypassed original rule")
	}
	draft := store.forkRecords()
	putUniqueTest(draft, "unique.parent", indexedUniqueParent{Record: platform.Record{ID: "2"}, Group: "1"})
	draft.writes["unique.parent/2"] = true
	updated := draft.types["unique.child"].uniqueIndexes[rule.Name]
	key, _ := draft.uniqueKey(et.info, rule, draft.types["unique.child"].rows.Get("1").Value)
	if updated.keys.Get(key).Len() != 2 {
		t.Fatal("parent scope change missed dependent key")
	}
	if original.keys.Get(key).Len() != 1 || store.types["unique.child"].uniqueIndexes[rule.Name].keys != original.keys {
		t.Fatal("private index leaked")
	}
	if updated.rows.Get("3") != original.rows.Get("3") {
		t.Fatal("unrelated key changed")
	}
	changes := draft.uniqueChanges()
	if len(changes) != 2 || changes["unique.child/2"][rule.Name] != key {
		t.Fatal("derived changes missed unchanged child", changes)
	}
	if store.types["unique.parent"].rows.Get("2").Value.Interface().(indexedUniqueParent).Group != "2" {
		t.Fatal("discarded change reached live state")
	}
	draft = store.forkRecords()
	putUniqueTest(draft, "unique.child", indexedUniqueChild{Record: platform.Record{ID: "1"}, Parent: "1", Code: "different"})
	if draft.types["unique.child"].uniqueIndexes[rule.Name].keys.Has(key) {
		t.Fatal("replacement retained old key")
	}
	if original.keys.Get(key).Len() != 1 {
		t.Fatal("replacement mutated predecessor")
	}
}
func TestUniqueIndexPreservesHistoricalDuplicatesAndMissingScopes(t *testing.T) {
	store, rule := uniqueIndexStore(t)
	putUniqueTest(store, "unique.parent", indexedUniqueParent{Record: platform.Record{ID: "p"}, Group: "g"})
	for _, id := range []string{"a", "b"} {
		putUniqueTest(store, "unique.child", indexedUniqueChild{Record: platform.Record{ID: id}, Parent: "p", Code: "same"})
	}
	putUniqueTest(store, "unique.child", indexedUniqueChild{Record: platform.Record{ID: "incomplete"}, Parent: "missing", Code: "same"})
	et := store.types["unique.child"]
	store.prepareUniqueIndexes()
	existing := et.rows.Get("a").Value
	if ok, err := store.uniqueConstraint(et.info, rule, existing); ok || err != nil {
		t.Fatal("historical duplicate lost", ok, err)
	}
	if len(store.uniqueKeys(et.info.Type, "incomplete")) != 0 {
		t.Fatal("missing scope occupied a key")
	}
	putUniqueTest(store, "unique.parent", indexedUniqueParent{Record: platform.Record{ID: "missing"}, Group: "g"})
	key, _ := store.uniqueKey(et.info, rule, existing)
	if et.uniqueIndexes[rule.Name].keys.Get(key).Len() != 3 {
		t.Fatal("new parent missed old child")
	}
	et.setRows(et.rows)
	if len(et.uniqueIndexes) != 0 {
		t.Fatal("bulk retained obsolete roots")
	}
	store.prepareUniqueIndexes()
	if et.uniqueIndexes[rule.Name].keys.Get(key).Len() != 3 {
		t.Fatal("bulk changed occupants")
	}
}
func BenchmarkUniqueIndexLookup(b *testing.B) {
	for _, count := range []int{1000, 10000, 100000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			store := newRecordStore()
			rule := platform.Constraint{Name: "code", Kind: "unique", Fields: []string{"code"}, Scope: "tenant"}
			info, err := platform.Describe("unique", platform.Entity{Type: "unique.child", Model: indexedUniqueChild{}, Constraints: []platform.Constraint{rule}}, func(reflect.Type) string { return "" })
			if err != nil {
				b.Fatal(err)
			}
			et := &entityType{store: store, info: info}
			store.types[info.Type] = et
			store.byGo[info.Go] = et
			for i := 0; i < count; i++ {
				putUniqueTest(store, info.Type, indexedUniqueChild{Record: platform.Record{ID: fmt.Sprint(i)}, Code: fmt.Sprint(i)})
			}
			store.prepareUniqueIndexes()
			candidate := indexedUniqueChild{Record: platform.Record{ID: "new"}, Code: "new"}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if ok, err := store.uniqueConstraint(info, rule, copyOf(reflect.TypeOf(candidate), candidate)); !ok || err != nil {
					b.Fatal(ok, err)
				}
			}
		})
	}
}

func TestScopedUniqueParentEditChecksOnlyDependentChildren(t *testing.T) {
	tn := builderTenant(t, "scope-parent-index")
	group := publishObject(t, tn, "builder", "GROUP", map[string]any{"name": "group", "title": "Group", "fields": []map[string]any{{"name": "label", "title": "Label", "type": "text"}}}, txnAt)
	parent := publishObject(t, tn, "builder", "PARENT", map[string]any{"name": "parent", "title": "Parent", "fields": []map[string]any{{"name": "group", "title": "Group", "type": "reference", "ref": group}}}, txnAt)
	rule := platform.Constraint{Name: "scope-code", Kind: "unique", Fields: []string{"code"}, Scope: "parent.group", Message: "Code must be unique within its group."}
	child := publishObject(t, tn, "builder", "CHILD", map[string]any{"name": "child", "title": "Child", "fields": []map[string]any{{"name": "parent", "title": "Parent", "type": "reference", "ref": parent}, {"name": "code", "title": "Code", "type": "text"}}, "constraints": []platform.Constraint{rule}}, txnAt)
	for _, id := range []string{"G1", "G2"} {
		decide(t, tn, "builder", "build", group+".create", group, id, map[string]any{"label": id}, txnAt)
	}
	for i, id := range []string{"P1", "P2"} {
		decide(t, tn, "builder", "build", parent+".create", parent, id, map[string]any{"group": fmt.Sprintf("G%d", i+1)}, txnAt)
		decide(t, tn, "builder", "build", child+".create", child, fmt.Sprintf("C%d", i+1), map[string]any{"parent": id, "code": "same"}, txnAt)
	}
	original := tn.records.types[child].uniqueIndexes[rule.Name].keys
	if result := refuse(t, tn, "builder", "build", parent+".edit", parent, "P2", map[string]any{"group": "G1"}, txnAt); !strings.Contains(result, rule.Message) {
		t.Fatal("parent scope edit escaped unchanged child rule", result)
	}
	if tn.records.types[child].uniqueIndexes[rule.Name].keys != original {
		t.Fatal("refused parent edit published private keys")
	}
	record, err := tn.RecordOf(memberOf(t, tn, "builder"), parent, "P2", txnAt)
	if err != nil {
		t.Fatal(err)
	}
	raw := platform.Raw(record.Record)
	if !strings.Contains(string(raw), `"group":"G2"`) {
		t.Fatal("refused parent edit changed its source", string(raw))
	}
	decide(t, tn, "builder", "build", child+".edit", child, "C2", map[string]any{"code": "other"}, txnAt)
	decide(t, tn, "builder", "build", parent+".edit", parent, "P2", map[string]any{"group": "G1"}, txnAt)
}

type indexedUniqueState struct {
	journal.RuntimeState
	byKey   map[string][]string
	applied int64
}

func (s *indexedUniqueState) StateUnique(_ context.Context, _, _, _, key string, position int64) ([]string, error) {
	if position != s.applied {
		return nil, fmt.Errorf("wrong applied position %d", position)
	}
	return s.byKey[key], nil
}
func TestUniqueSQLReadsPrivateKeyChangesWithoutResidentIndex(t *testing.T) {
	store, rule := uniqueIndexStore(t)
	for i := 1; i <= 2; i++ {
		putUniqueTest(store, "unique.parent", indexedUniqueParent{Record: platform.Record{ID: fmt.Sprint(i)}, Group: fmt.Sprint(i)})
		putUniqueTest(store, "unique.child", indexedUniqueChild{Record: platform.Record{ID: fmt.Sprint(i)}, Parent: fmt.Sprint(i), Code: "same"})
	}
	backend := &indexedUniqueState{byKey: map[string][]string{}, applied: 7}
	for id := range store.types["unique.child"].rows.All() {
		key := store.uniqueKeys("unique.child", id)[rule.Name]
		backend.byKey[key] = append(backend.byKey[key], id)
	}
	store.historyStore = backend
	store.appliedPosition = 7
	candidate := indexedUniqueChild{Record: platform.Record{ID: "new"}, Parent: "1", Code: "same"}
	info := store.types["unique.child"].info
	if ok, err := store.uniqueConstraint(info, rule, copyOf(reflect.TypeOf(candidate), candidate)); ok || err != nil {
		t.Fatal("SQL occupant ignored", ok, err)
	}
	draft := store.forkRecords()
	putUniqueTest(draft, "unique.child", indexedUniqueChild{Record: platform.Record{ID: "1"}, Parent: "1", Code: "different"})
	draft.writes["unique.child/1"] = true
	if ok, err := draft.uniqueConstraint(info, rule, copyOf(reflect.TypeOf(candidate), candidate)); !ok || err != nil {
		t.Fatal("private key replacement retained SQL predecessor", ok, err)
	}
	putUniqueTest(draft, "unique.parent", indexedUniqueParent{Record: platform.Record{ID: "2"}, Group: "1"})
	draft.writes["unique.parent/2"] = true
	if ok, err := draft.uniqueConstraint(info, rule, copyOf(reflect.TypeOf(candidate), candidate)); ok || err != nil {
		t.Fatal("private parent scope omitted child key", ok, err)
	}
	for _, et := range draft.types {
		if len(et.uniqueIndexes) != 0 {
			t.Fatal("SQL lookup built complete resident key table")
		}
	}
	if len(store.types["unique.child"].uniqueIndexes) != 0 {
		t.Fatal("SQL lookup retained complete live key table")
	}
}

func TestPostgresBusinessUniqueKeysSurvivePrivateFailureAndRestart(t *testing.T) {
	dsn := os.Getenv("PLATFORM_TEST_DATABASE")
	if dsn == "" {
		t.Skip("PLATFORM_TEST_DATABASE not set")
	}
	ctx := context.Background()
	id := fmt.Sprintf("business-unique-%d", time.Now().UnixNano())
	j, err := OpenJournal(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer forgetJournal(t, j, id)
	compose := func() *Tenant {
		return composeTenant(t, id, []Seat{seatOf("writer", "invariants:operator")}, newConstraintOwner(id))
	}
	live := compose()
	if _, err = j.Entries(ctx, id, 0); err != nil {
		t.Fatal(err)
	}
	live.attachJournal(ctx, j)
	const typ = "invariants.entry"
	decide(t, live, "writer", "invariants", typ+".create", typ, "A", validConstraintEntry("ORIGINAL"), txnAt)
	if err = live.migrateRuntimeState(ctx, j); err != nil {
		t.Fatal(err)
	}
	checkKeys := func(tn *Tenant, key string, want ...string) {
		t.Helper()
		store, ok := tn.records.historyStore.(journal.UniqueState)
		if !ok {
			t.Fatal("SQL unique-key lookup not installed")
		}
		ids, err := store.StateUnique(ctx, id, typ, "number", string(platform.Raw([]string{key})), tn.records.appliedPosition)
		if err != nil || !slices.Equal(ids, want) {
			t.Fatalf("unique key %s: %v, want %v: %v", key, ids, want, err)
		}
		if len(tn.records.types[typ].uniqueIndexes) != 0 {
			t.Fatal("SQL tenant retained a complete Go unique-key table")
		}
	}
	checkKeys(live, "ORIGINAL", "A")
	decide(t, live, "writer", "invariants", typ+".create", typ, "B", validConstraintEntry("NEXT"), txnAt)
	if result := refuse(t, live, "writer", "invariants", typ+".create", typ, "DUPLICATE", validConstraintEntry("NEXT"), txnAt); !strings.Contains(result, "Number must be unique") {
		t.Fatal("SQL-backed write missed existing owner key", result)
	}
	if _, ok := live.Held(typ + "/DUPLICATE"); ok {
		t.Fatal("refused duplicate became visible")
	}
	checkKeys(live, "NEXT", "B")
	// Put changes the private key before the following quantity rule refuses.
	// The refusal may be durable, but its rejected replacement must reserve no key.
	if result := refuse(t, live, "writer", "invariants", typ+".edit", typ, "B", map[string]any{"number": "NEW", "qty": 0}, txnAt); !strings.Contains(result, "Quantity must be positive") {
		t.Fatal("private invalid edit was not refused", result)
	}
	checkKeys(live, "NEXT", "B")
	checkKeys(live, "NEW")
	decide(t, live, "writer", "invariants", typ+".create", typ, "C", validConstraintEntry("NEW"), txnAt)
	decide(t, live, "writer", "invariants", typ+".edit", typ, "B", map[string]any{"number": "RENAMED"}, txnAt)
	decide(t, live, "writer", "invariants", typ+".create", typ, "D", validConstraintEntry("NEXT"), txnAt)
	checkKeys(live, "RENAMED", "B")
	checkKeys(live, "NEW", "C")
	checkKeys(live, "NEXT", "D")
	restored := compose()
	deployment := Deployment{SnapshotEvery: 1}
	if err = deployment.restoreTenants(ctx, j, "unique-sql", []*Tenant{restored}, map[string]int64{}, map[string]bool{}); err != nil || restored.quarantined() {
		t.Fatalf("SQL tenant restart failed: %v %v", err, restored.fault.Load())
	}
	checkKeys(restored, "ORIGINAL", "A")
	checkKeys(restored, "RENAMED", "B")
	checkKeys(restored, "NEW", "C")
	checkKeys(restored, "NEXT", "D")
	if result := refuse(t, restored, "writer", "invariants", typ+".create", typ, "AFTER-RESTART", validConstraintEntry("ORIGINAL"), txnAt); !strings.Contains(result, "Number must be unique") {
		t.Fatal("restart lost authoritative unique occupants", result)
	}
	decide(t, restored, "writer", "invariants", typ+".create", typ, "FRESH", validConstraintEntry("FRESH"), txnAt)
	checkKeys(restored, "FRESH", "FRESH")
}

func TestPostgresNonRecordResultsAdvanceOnlyAfterApplication(t *testing.T) {
	dsn := os.Getenv("PLATFORM_TEST_DATABASE")
	if dsn == "" {
		t.Skip("PLATFORM_TEST_DATABASE not set")
	}
	ctx := context.Background()
	id := fmt.Sprintf("nonrecord-position-%d", time.Now().UnixNano())
	j, err := OpenJournal(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer forgetJournal(t, j, id)
	worker := newResultWorker(id)
	worker.idle = true
	live := composeTenant(t, id, []Seat{seatOf("writer", "invariants:operator", "a:writer")}, newConstraintOwner(id), newNotes(id, "a"), worker)
	if _, err = j.Entries(ctx, id, 0); err != nil {
		t.Fatal(err)
	}
	live.attachJournal(ctx, j)
	decide(t, live, "writer", "invariants", "invariants.entry.create", "invariants.entry", "A", validConstraintEntry("ORIGINAL"), txnAt)
	if err = live.migrateRuntimeState(ctx, j); err != nil {
		t.Fatal(err)
	}
	original := live.AcceptResult
	mode := "ok"
	var lastKind string
	live.AcceptResult = func(entry Entry, key, hash string) ([]byte, error) {
		before := live.records.appliedPosition
		if mode == "failed" {
			return nil, errors.New("append unavailable")
		}
		raw, err := original(entry, key, hash)
		if err != nil {
			return nil, err
		}
		if live.records.appliedPosition != before {
			t.Fatal("record read view advanced before result application")
		}
		var envelope struct{ Kind string }
		if err = json.Unmarshal(raw, &envelope); err != nil {
			t.Fatal(err)
		}
		lastKind = envelope.Kind
		delta, err := acceptedStorageDelta(raw)
		if err != nil || len(delta.Records) != 0 {
			t.Fatalf("test result unexpectedly changes record rows: %s %d %v", lastKind, len(delta.Records), err)
		}
		if mode == "unknown" {
			return nil, journal.ErrCommitUnknown
		}
		return raw, nil
	}
	check := func(kind string, before int64) {
		t.Helper()
		if lastKind != kind || live.records.appliedPosition != before+1 || j.Position(id) != before+1 {
			t.Fatalf("%s did not publish exactly its applied result position: kind=%s applied=%d journal=%d before=%d", kind, lastKind, live.records.appliedPosition, j.Position(id), before)
		}
		store := live.records.historyStore.(journal.UniqueState)
		ids, err := store.StateUnique(ctx, id, "invariants.entry", "number", `["ORIGINAL"]`, live.records.appliedPosition)
		if err != nil || !slices.Equal(ids, []string{"A"}) {
			t.Fatal("nonrecord result invalidated original unique read view", ids, err)
		}
	}
	before := j.Position(id)
	if _, refusal := live.Input(memberOf(t, live, "writer"), "a-feed", []byte("first"), txnAt); refusal != nil {
		t.Fatal(refusal)
	}
	check("input-result", before)
	before = j.Position(id)
	if _, err = live.openSupport("writer", "inspect", "operator", 30, txnAt); err != nil {
		t.Fatal(err)
	}
	check("host-result", before)
	var task *Task
	for _, candidate := range live.work.jobs {
		if candidate.App == "worker" {
			task = candidate
			break
		}
	}
	if task == nil {
		t.Fatal("idle worker job missing")
	}
	before = j.Position(id)
	live.mu.Lock()
	outcome := live.run(task, txnAt, false)
	live.mu.Unlock()
	if outcome != "ok" {
		t.Fatal(outcome)
	}
	check("work-result", before)
	before = j.Position(id)
	mode = "failed"
	if _, refusal := live.Input(memberOf(t, live, "writer"), "a-feed", []byte("failed"), txnAt); refusal == nil {
		t.Fatal("failed append accepted input")
	}
	if live.records.appliedPosition != before || j.Position(id) != before || live.app("a").(*notes).texts["feed"] != "first" {
		t.Fatal("failed append published state or position")
	}
	mode = "unknown"
	if _, refusal := live.Input(memberOf(t, live, "writer"), "a-feed", []byte("unknown"), txnAt); refusal == nil {
		t.Fatal("unknown commit accepted input")
	}
	if !live.quarantined() || live.records.appliedPosition != before || j.Position(id) != before+1 || live.app("a").(*notes).texts["feed"] != "first" {
		t.Fatal("unapplied unknown commit published read view or did not stop tenant")
	}
}
