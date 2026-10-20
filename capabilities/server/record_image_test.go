package platformserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"platformserver/internal/acceptance"
	"reflect"
	"testing"
	"time"

	"platformserver/journal"
	"platformserver/platform"
)

func TestRecordImageRestoreAndReinstallRetainOriginal(t *testing.T) {
	type oldModel struct {
		platform.Record
		Name string `json:"name"`
	}
	type currentModel struct {
		platform.Record
		Title string `json:"title"`
	}
	current, err := platform.Describe("example", platform.Entity{Type: "example.item", Model: currentModel{}, RecordFormat: "item.v2",
		RecordImages: []platform.RecordImageFormat{{Format: "", Model: oldModel{}, Convert: func(raw json.RawMessage) (json.RawMessage, error) {
			var old oldModel
			if err := json.Unmarshal(raw, &old); err != nil {
				return nil, err
			}
			return json.Marshal(currentModel{Record: old.Record, Title: old.Name})
		}}}}, func(reflect.Type) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	store := newRecordStore()
	if err := store.install(current); err != nil {
		t.Fatal(err)
	}
	raw := json.RawMessage(`{ "id":"one", "revision":2, "name":"Original" }`)
	tn := &Tenant{records: store}
	held, err := tn.restoreRecords(map[string][]recordState{"example.item": {{Value: raw}}})
	if err != nil || len(held) != 0 {
		t.Fatalf("old snapshot restore: %+v, %v", held, err)
	}
	check := func() {
		t.Helper()
		saved := store.types["example.item"].rows.Get("one")
		image, err := saved.image()
		value := saved.value.Interface().(currentModel)
		if err != nil || string(image) != string(raw) || saved.format != "" || value.Title != "Original" || value.Revision != 2 {
			t.Fatalf("recovery changed the historical image or format: %s, %+v, %v", image, value, err)
		}
	}
	check()
	if err := store.install(current); err != nil {
		t.Fatalf("reinstall after conversion: %v", err)
	}
	check()
	// A source-release projection has its own format. The current row's format
	// must not be applied to that older publication view.
	view, err := platform.Describe("example", platform.Entity{Type: "example.item", Model: oldModel{}}, func(reflect.Type) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	shape, _ := acceptance.Digest(view.Fields)
	projection := &recordProjection{From: "source-release", Shape: shape, Value: raw}
	actual, _ := json.Marshal(currentModel{Record: platform.Record{ID: "one", Revision: 2}, Title: "Original"})
	decoded, err := rowDecodeFormattedImage(view, actual, projection, "source-release", "item.v2")
	if err != nil || string(decoded) != string(raw) {
		t.Fatalf("projection used the current image's format: %s, %v", decoded, err)
	}
}

func TestPostgresRecordImageConvertedFieldQueryRetainsOriginal(t *testing.T) {
	dsn := os.Getenv("PLATFORM_TEST_DATABASE")
	if dsn == "" {
		t.Skip("PLATFORM_TEST_DATABASE not set")
	}
	ctx := context.Background()
	tenant := fmt.Sprintf("image-query-%d", time.Now().UnixNano())
	pg, err := OpenJournal(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer forgetJournal(t, pg, tenant)
	type oldModel struct {
		platform.Record
		Name string `json:"name"`
	}
	type currentModel struct {
		platform.Record
		Title string `json:"title"`
	}
	info, err := platform.Describe("example", platform.Entity{Type: "example.item", Model: currentModel{}, RecordFormat: "item.v2",
		RecordImages: []platform.RecordImageFormat{{Format: "", Model: oldModel{}, Convert: func(raw json.RawMessage) (json.RawMessage, error) {
			var old oldModel
			if err := json.Unmarshal(raw, &old); err != nil {
				return nil, err
			}
			return json.Marshal(currentModel{Record: old.Record, Title: old.Name})
		}}}}, func(reflect.Type) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	store := newRecordStore()
	if err := store.install(info); err != nil {
		t.Fatal(err)
	}
	var states []recordState
	var delta journal.StateDelta
	var original json.RawMessage
	// The visible match is on the second candidate page. Another matching row
	// is hidden, while the other rows must still fail the original Go predicate.
	for n := 0; n <= 256; n++ {
		id, name := fmt.Sprintf("row-%03d", n), "Other"
		if n == 0 || n == 256 {
			name = "Original"
		}
		raw, err := json.Marshal(oldModel{Record: platform.Record{ID: id, Revision: 2}, Name: name})
		if err != nil {
			t.Fatal(err)
		}
		state := recordState{Value: raw}
		states = append(states, state)
		delta.Records = append(delta.Records, journal.StateRecord{Type: info.Type, ID: id, Image: jsonOf(state), HistoryBase: -1})
		if n == 256 {
			original = raw
		}
	}
	tn := &Tenant{records: store}
	if held, err := tn.restoreRecords(map[string][]recordState{info.Type: states}); err != nil || len(held) != 0 {
		t.Fatalf("restore old rows: %v, held=%d", err, len(held))
	}
	if err := pg.InitializeState(ctx, tenant, 0, delta); err != nil {
		t.Fatal(err)
	}
	store.historyStore, store.tenant, store.appliedPosition = pg, tenant, 0
	matches, refusal := store.matchingQuery(store.types[info.Type], platform.Query{Domain: json.RawMessage(`[["title","=","Original"]]`)},
		func(value reflect.Value) bool { return recordOf(value).ID != "row-000" })
	if refusal != nil || len(matches) != 1 || matches[0].Interface().(currentModel).ID != "row-256" {
		t.Fatalf("converted-field query lost its visible match or bypassed filtering: %v, %v", matches, refusal)
	}
	saved := store.types[info.Type].rows.Get("row-256")
	image, err := saved.image()
	if err != nil || string(image) != string(original) || saved.format != "" {
		t.Fatalf("query rewrote the original Go image: %s, %v", image, err)
	}
	raw, err := pg.StateRecord(ctx, tenant, info.Type, "row-256")
	var stored recordState
	if err != nil || json.Unmarshal(raw, &stored) != nil || string(stored.Value) != string(original) || stored.Format != "" {
		t.Fatalf("query rewrote the original SQL image: %s, %v", raw, err)
	}
}
