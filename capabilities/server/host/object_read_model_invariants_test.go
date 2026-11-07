package host

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	"platformserver/services/resources/authoring"
)

func TestInvariantObjectSectionsReadIndependently(t *testing.T) {
	live := newInvariantTenant(t, "invariant-object-sections", platform.Seat{Subjects: []string{"reader"}, Member: platform.Member{ID: "reader", Roles: map[string]string{build.ID: "user"}}})
	object := invariantObject("alpha", "Alpha")
	object["fields"] = []build.Field{
		{Name: "note", Title: "Note", Type: "text"},
		{Name: "secret", Title: "Secret", Type: "text", Read: []string{build.Builder}},
	}
	live.accept(t, "build.object.create", build.ObjectType, "OBJ", object)
	live.accept(t, "build.object.publish", build.ObjectType, "OBJ", map[string]any{})
	live.accept(t, "build.alpha.create", "build.alpha", "A1", map[string]any{"note": "first", "secret": "private"})
	live.accept(t, "build.alpha.edit", "build.alpha", "A1", map[string]any{"note": "second", "secret": "private"})

	reader, ok := live.Member("reader")
	if !ok {
		t.Fatal("reader is not seated")
	}
	base, refusal := live.RecordOf(t.Context(), reader, "build.alpha", "A1", invariantClock)
	if refusal != nil {
		t.Fatalf("read object: %v", refusal)
	}
	raw, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Record map[string]json.RawMessage `json:"record"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(base.Restricted, "secret") {
		t.Fatalf("field permission is not reported as restricted: %+v", base.Restricted)
	}
	if value := string(wire.Record["secret"]); value == `"private"` {
		t.Fatalf("restricted field leaked in the base record: %s", raw)
	}
	for _, section := range []string{"history", "related", "linked", "activity", "files", "comments", "tasks", "approvals", "processes"} {
		var body map[string]json.RawMessage
		_ = json.Unmarshal(raw, &body)
		if _, present := body[section]; present {
			t.Fatalf("base record carries the %s section: %s", section, raw)
		}
	}

	history, refusal := live.recordSection(live.member(t), "build.alpha", "A1", RecordSectionHistory, invariantClock)
	if refusal != nil || len(history.History) < 2 {
		t.Fatalf("history section: %+v: %v", history, refusal)
	}
	links, refusal := live.recordSection(live.member(t), "build.alpha", "A1", RecordSectionLinks, invariantClock)
	if refusal != nil || len(links.Related) != 0 {
		t.Fatalf("links section: %+v: %v", links, refusal)
	}
	for _, section := range []string{RecordSectionActivity, RecordSectionFiles, RecordSectionWork, RecordSectionComments} {
		value, refusal := live.recordSection(live.member(t), "build.alpha", "A1", section, invariantClock)
		if refusal != nil || value.Section != section {
			t.Fatalf("%s section: %+v: %v", section, value, refusal)
		}
	}
}

func TestInvariantOwnedRelationsReadWithoutNestedRecordLock(t *testing.T) {
	live := newInvariantTenant(t, "invariant-owned-relations", platform.Seat{Subjects: []string{"reader"}, Member: platform.Member{ID: "reader", Roles: map[string]string{build.ID: "user"}}})
	info, err := platform.Describe(build.ID, platform.RelationEntity(build.ID), func(reflect.Type) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if err := live.records.Install(info); err != nil {
		t.Fatal(err)
	}
	caller := platform.NewCaller(runtime{t: live.Tenant}, live.member(t), build.ID, false, false)
	done := make(chan []platform.RelationLink, 1)
	go func() {
		done <- live.ownedRelations(live.records, caller, "", invariantClock)
	}()
	select {
	case links := <-done:
		if len(links) != 0 {
			t.Fatalf("owned relationships on an empty tenant: %+v", links)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("owned relationship read nested the record store lock")
	}
}

func TestInvariantPageModelUsesObjectSetWindow(t *testing.T) {
	live := newInvariantTenant(t, "invariant-page-model-window", platform.Seat{Subjects: []string{"reader"}, Member: platform.Member{ID: "reader", Roles: map[string]string{build.ID: "user"}}})
	object := invariantObject("bulk", "Bulk")
	object["fields"] = []build.Field{
		{Name: "name", Title: "Name", Type: "text", Search: true},
		{Name: "rank", Title: "Rank", Type: "integer"},
		{Name: "secret", Title: "Secret", Type: "text", Read: []string{build.Builder}},
	}
	live.accept(t, "build.object.create", build.ObjectType, "BULK", object)
	live.accept(t, "build.object.publish", build.ObjectType, "BULK", map[string]any{})
	live.records.Lock()
	entity := live.records.Type("build.bulk")
	live.records.Unlock()
	if entity == nil {
		t.Fatal("bulk object type is unavailable")
	}
	caller := platform.NewCaller(runtime{t: live.Tenant}, live.member(t), build.ID, false, false)
	for index := 0; index <= platform.RecordSetMaxRows; index++ {
		name := fmt.Sprintf("row %d", index)
		if index%1000 == 0 {
			name = "needle " + fmt.Sprint(index)
		}
		id := fmt.Sprintf("B%05d", index)
		value := reflect.New(entity.Info().Go).Elem()
		value.FieldByName("ID").SetString(id)
		for field, item := range map[string]any{"name": name, "rank": index, "secret": "do not project"} {
			info, ok := entity.Info().Field(field)
			if !ok {
				t.Fatalf("bulk field %s is unavailable", field)
			}
			value := value.FieldByIndex(info.Index)
			if value.Kind() == reflect.Pointer {
				value.Set(reflect.New(value.Type().Elem()))
				value = value.Elem()
			}
			switch item := item.(type) {
			case string:
				value.SetString(item)
			case int:
				value.SetInt(int64(item))
			}
		}
		submission := live.submission("build.bulk.create", "build.bulk", id, map[string]any{})
		receipt := &pb.ChangeRecord{ChangeId: "bulk-" + id, Submission: submission, RecordedTime: timestamppb.New(invariantClock), Revision: 1}
		if refusal := live.records.Put(caller, receipt, value.Interface()); refusal != nil {
			t.Fatalf("seed bulk record %s: %v", id, refusal)
		}
	}
	model := &platform.ModelBinding{Type: "build.bulk", Visibility: "mark", Fields: []platform.FieldRef{{Name: "name"}, {Name: "rank"}, {Name: "secret"}}}
	objectRef := platform.ModelObject(model.Type)
	page := &platform.Page{
		Format: 3, Name: "bulkpage", Title: "Bulk", Object: objectRef,
		DataBindings: []platform.PageDataBinding{{
			ID: "rows", Kind: "records", Object: objectRef, Model: model,
			Fields: []platform.FieldRef{{Name: "name"}, {Name: "rank"}},
			Ports:  map[string]string{"widget": "table"},
		}},
	}
	request := PageModelRead{Draft: page, Section: "rows", Search: "needle", Sort: []string{"-rank"}, Limit: 5}
	reader, ok := live.Member("reader")
	if !ok {
		t.Fatal("reader is not seated")
	}
	result, refusal := live.readPageModelSource(reader, *page, page.DataBindings[0], request, invariantClock)
	if refusal != nil {
		t.Fatalf("page model read: %v", refusal)
	}
	if result.Total != 11 || len(result.Records) != 5 || result.Hidden {
		t.Fatalf("page model window: total=%d records=%d hidden=%v partial=%v", result.Total, len(result.Records), result.Hidden, result.Partial)
	}
	if !result.Partial || slices.ContainsFunc(result.Fields, func(field platform.FieldInfo) bool { return field.Name == "secret" }) {
		t.Fatalf("restricted page field was not confined: partial=%v fields=%+v", result.Partial, result.Fields)
	}
	if fmt.Sprint(result.Records[0]["rank"]) != fmt.Sprint(platform.RecordSetMaxRows) {
		t.Fatalf("first sorted record: %v", result.Records[0])
	}
	for _, row := range result.Records {
		if _, leaked := row["secret"]; leaked {
			t.Fatalf("unprojected field leaked: %v", row)
		}
	}
	request.Offset, request.Limit = 5, 200
	result, refusal = live.readPageModelSource(reader, *page, page.DataBindings[0], request, invariantClock)
	if refusal != nil || result.Total != 11 || len(result.Records) != 6 {
		t.Fatalf("page model second window: total=%d records=%d: %v", result.Total, len(result.Records), refusal)
	}
	request.Search, request.Offset, request.Limit = "", 9999, 2
	result, refusal = live.readPageModelSource(reader, *page, page.DataBindings[0], request, invariantClock)
	if refusal != nil || result.Total != platform.RecordSetMaxRows+1 || len(result.Records) != 2 {
		t.Fatalf("page model tail window: total=%d records=%d: %v", result.Total, len(result.Records), refusal)
	}
}
