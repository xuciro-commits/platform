package platformserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"platformserver/journal"
	"reflect"
	"testing"
	"time"

	"platformserver/kernel/records"
	"platformserver/platform"
)

type retainedProjectionSource struct {
	rows  records.Rows
	reads int
}

func (s *retainedProjectionSource) Get(id string) *records.Row { s.reads++; return s.rows.Get(id) }
func (s *retainedProjectionSource) IDs(after string, limit int) []string {
	return s.rows.IDs(after, limit)
}

func TestRetainedFlowReadShapeConvertsOnlyRequestedRows(t *testing.T) {
	type oldModel struct {
		platform.Record
		Name string `json:"name"`
	}
	type newModel struct {
		platform.Record
		Name  string `json:"name"`
		Title string `json:"title"`
	}
	describe := func(model any) platform.EntityInfo {
		t.Helper()
		info, err := platform.Describe("example", platform.Entity{Type: "example.item", Model: model}, func(reflect.Type) string { return "" })
		if err != nil {
			t.Fatal(err)
		}
		return info
	}
	old, current := describe(oldModel{}), describe(newModel{})
	for i := range current.Fields {
		if current.Fields[i].Name == "name" {
			current.Fields[i].AliasOf = "title"
		}
	}
	var index records.Index[*records.Row]
	for i := range 513 {
		id := fmt.Sprintf("%04d", i)
		value := records.Copy(current.Go, newModel{Record: platform.Record{ID: id}, Name: "Old", Title: "Current"})
		index.Set(id, &records.Row{Value: value})
	}
	source := &retainedProjectionSource{rows: records.MemoryRows(index)}
	store := records.NewStore()
	store.ReplaceReadType(current, records.SourceRows(source), false)
	runtime := &flowExecutionRuntime{stagedDecision: &stagedDecision{records: store}, view: &Tenant{records: store}, release: "original", shapes: map[string]platform.EntityInfo{old.Type: old}}
	view, err := runtime.readView()
	if err != nil || source.reads != 0 {
		t.Fatal("building a retained read view decoded rows", err, source.reads)
	}
	projected := view.records.Type(old.Type)
	if !projected.Rows().Sourced() || projected.Rows().Stored() != 0 {
		t.Fatal("retained view materialized a complete current table")
	}
	row := projected.Rows().Get("0001")
	if row.Value.Interface().(oldModel).Name != "Current" || source.reads != 1 {
		t.Fatal("lazy conversion lost the current alias", row, source.reads)
	}
	if _, err := runtime.readView(); err != nil || source.reads != 1 {
		t.Fatal("reusing definition/source identities converted more rows", err, source.reads)
	}
	if ids := projected.Rows().IDs("0255", 256); len(ids) != 256 || source.reads != 1 {
		t.Fatal("ID paging decoded retained values", len(ids), source.reads)
	}
	// A malformed stored image is still an explicit migration refusal. It must
	// never turn into a missing record or escape as a storage/process failure.
	bad := *index.Get("0001")
	bad.Original = json.RawMessage(`{"id":"0001","name":42,"title":42}`)
	source.rows.Set("bad", &bad)
	caller := platform.As("example", platform.Member{ID: "reader"})
	refusal := runtime.Check(caller, oldModel{Record: platform.Record{ID: "bad"}})
	if refusal == nil || runtime.failure != refusal {
		t.Fatal("invalid retained row became missing or failed to stop acceptance", refusal)
	}
}

func TestPostgresRetainedFlowAliasQueryUsesConvertedRows(t *testing.T) {
	dsn := os.Getenv("PLATFORM_TEST_DATABASE")
	if dsn == "" {
		t.Skip("PLATFORM_TEST_DATABASE not set")
	}
	type oldModel struct {
		platform.Record
		Name string `json:"name"`
	}
	type newModel struct {
		platform.Record
		Name  string `json:"name"`
		Title string `json:"title"`
	}
	old, err := platform.Describe("example", platform.Entity{Type: "example.item", Model: oldModel{}}, func(reflect.Type) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	current, err := platform.Describe("example", platform.Entity{Type: "example.item", Model: newModel{}}, func(reflect.Type) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	for i := range current.Fields {
		if current.Fields[i].Name == "name" {
			current.Fields[i].AliasOf = "title"
		}
	}
	ctx := context.Background()
	tenant := fmt.Sprintf("retained-source-%d", time.Now().UnixNano())
	j, err := OpenJournal(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer forgetJournal(t, j, tenant)
	if _, err = j.Entries(ctx, tenant, 0); err != nil {
		t.Fatal(err)
	}
	// Original bytes may retain the previous alias value. The selected owner
	// interprets it through title; SQL must not prefilter by the stale name.
	raw := jsonOf(newModel{Record: platform.Record{ID: "one"}, Name: "Old", Title: "Current"})
	delta := journal.StateDelta{Records: []journal.StateRecord{{Type: current.Type, ID: "one", Image: jsonOf(records.StoredState{Value: raw}), HistoryBase: -1}}}
	if err = j.InitializeState(ctx, tenant, 0, delta); err != nil {
		t.Fatal(err)
	}
	read, err := j.BeginStateRead(ctx, tenant, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	store := records.NewStore()
	et := store.ReplaceReadType(current, records.Rows{}, false)
	store.Current().Configure(j, tenant, 0, nil)
	store.Current().Bind(read)
	store.UseCurrentRows()
	runtime := &flowExecutionRuntime{stagedDecision: &stagedDecision{records: store}, view: &Tenant{records: store}, release: "original", shapes: map[string]platform.EntityInfo{old.Type: old}}
	projected, refusal := runtime.readView()
	if refusal != nil {
		t.Fatal(refusal)
	}
	view, _ := viewOf(platform.Member{}, projected.records.Type(old.Type))
	rows, total, refusal := projected.records.Find(view, platform.Query{Domain: json.RawMessage(`[["name","=","Current"]]`)}, nil)
	if refusal != nil || total != 1 || len(rows) != 1 || rows[0].Interface().(oldModel).Name != "Current" {
		t.Fatal("SQL field prefilter discarded a valid retained alias", rows, total, refusal)
	}
	if et.Rows().Stored() != 0 || projected.records.Type(old.Type).Rows().Stored() != 0 {
		t.Fatal("retained query cached a current record")
	}
	// A new lane can use a new SQL snapshot at the same generation and position.
	// Reusing the old metadata cache must not reuse its now-closed SQL reader.
	beforeGeneration := store.Generation()
	if err := read.Close(); err != nil {
		t.Fatal(err)
	}
	next, err := j.BeginStateRead(ctx, tenant, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	store.Current().Bind(next)
	second, refusal := runtime.readView()
	if refusal != nil || second.records == projected.records || second.records.Current().View() != next || store.Generation() != beforeGeneration {
		t.Fatal("retained cache reused a closed SQL view without writes", refusal)
	}
	rows, total, refusal = second.records.Find(second.records.Type(old.Type), platform.Query{Domain: json.RawMessage(`[["name","=","Current"]]`)}, nil)
	if refusal != nil || total != 1 || len(rows) != 1 {
		t.Fatal("second SQL scope did not use its own candidates", total, refusal)
	}
}
