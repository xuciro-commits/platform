package platformserver

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"platformserver/journal"
	"platformserver/kernel/records"
	"platformserver/platform"
)

type countedCurrentView struct {
	journal.StateReadView
	loaded map[string]int
}

func (v *countedCurrentView) StateCurrent(ctx context.Context, tenant, typ, id string) (journal.StateCurrent, error) {
	v.loaded[typ+"/"+id]++
	return v.StateReadView.StateCurrent(ctx, tenant, typ, id)
}

func TestPostgresSourcedRowsUsePrivateOverlayWithoutResidentRecords(t *testing.T) {
	dsn := os.Getenv("PLATFORM_TEST_DATABASE")
	if dsn == "" {
		t.Skip("PLATFORM_TEST_DATABASE not set")
	}
	ctx := context.Background()
	tenant := fmt.Sprintf("sourced-rows-%d", time.Now().UnixNano())
	j, err := OpenJournal(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer forgetJournal(t, j, tenant)
	store, rule := uniqueIndexStore(t)
	for i := range 513 {
		parent := "P2"
		if i < 2 {
			parent = "P1"
		}
		putUniqueTest(store, "unique.child", indexedUniqueChild{Record: platform.Record{ID: fmt.Sprintf("C%04d", i)}, Parent: parent, Code: fmt.Sprint(i)})
	}
	putUniqueTest(store, "unique.parent", indexedUniqueParent{Record: platform.Record{ID: "P1"}, Group: "G1"})
	putUniqueTest(store, "unique.parent", indexedUniqueParent{Record: platform.Record{ID: "P2"}, Group: "G2"})
	var delta journal.StateDelta
	for typ, et := range store.types {
		for id, row := range et.rows.All() {
			image, err := row.Image()
			if err != nil {
				t.Fatal(err)
			}
			delta.Records = append(delta.Records, journal.StateRecord{Type: typ, ID: id, Image: jsonOf(recordState{Value: image}), HistoryBase: -1})
			delta.Unique = append(delta.Unique, journal.StateUnique{Type: typ, ID: id, Keys: store.uniqueKeys(typ, id)})
		}
	}
	if _, err = j.Entries(ctx, tenant, 0); err != nil {
		t.Fatal(err)
	}
	if err = j.InitializeState(ctx, tenant, 0, delta); err != nil {
		t.Fatal(err)
	}
	view, err := j.BeginStateRead(ctx, tenant, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	counted := &countedCurrentView{StateReadView: view, loaded: map[string]int{}}
	store.current.Configure(j, tenant, 0, nil)
	store.current.Bind(counted)
	store.useCurrentRows()
	child := store.types["unique.child"]
	if child.rows.Get("C0000") == nil || child.rows.Stored() != 0 {
		t.Fatal("committed fetch retained current rows")
	}
	field, _ := child.info.Field("parent")
	bucket := child.referenceBucket(field, "P1")
	if bucket.IDs.Len() != 2 || referenceIndexCount(child) != 0 {
		t.Fatal("SQL reference lookup differs", bucket.IDs.Len(), referenceIndexCount(child))
	}
	for ref := range counted.loaded {
		if strings.HasPrefix(ref, "unique.child/") && ref != "unique.child/C0000" && ref != "unique.child/C0001" {
			t.Fatal("single reference lookup read an unrelated row", ref)
		}
	}
	draft := store.forkRecords()
	putUniqueTest(draft, "unique.child", indexedUniqueChild{Record: platform.Record{ID: "C0000"}, Parent: "P2", Code: "private"})
	draft.writes["unique.child/C0000"] = true
	if draft.types[child.info.Type].referenceBucket(field, "P1").IDs.Len() != 1 || child.referenceBucket(field, "P1").IDs.Len() != 2 {
		t.Fatal("private reference replacement leaked or kept its predecessor")
	}
	nested := draft.forkRecords()
	putUniqueTest(nested, "unique.child", indexedUniqueChild{Record: platform.Record{ID: "NEW"}, Parent: "P1", Code: "new"})
	nested.writes["unique.child/NEW"] = true
	if nested.types[child.info.Type].referenceBucket(field, "P1").IDs.Len() != 2 || draft.types[child.info.Type].rows.Get("NEW") != nil {
		t.Fatal("nested private overlay changed its parent")
	}
	candidate := indexedUniqueChild{Record: platform.Record{ID: "OTHER"}, Parent: "P2", Code: "private"}
	if ok, err := draft.uniqueConstraint(child.info, rule, records.Copy(reflect.TypeOf(candidate), candidate)); ok || err != nil {
		t.Fatal("private unique key was not occupied", ok, err)
	}
	if _, _, err := draft.find(draft.types[child.info.Type], platform.Query{Domain: platform.Raw([]any{[]any{"id", "=", "C0000"}})}, nil); err != nil {
		t.Fatal(err)
	}
	for _, et := range store.types {
		if et.rows.Stored() != 0 || referenceIndexCount(et) != 0 || uniqueIndexCount(et) != 0 {
			t.Fatal("SQL current state remained resident")
		}
	}
	for _, et := range draft.types {
		if referenceIndexCount(et) != 0 || uniqueIndexCount(et) != 0 {
			t.Fatal("private lookup retained a complete derived index")
		}
	}
	if child.rows.Get("C0000").Value.Interface().(indexedUniqueChild).Code != "0" {
		t.Fatal("discarded draft modified accepted data")
	}
}
