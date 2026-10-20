package platform

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestRecordImageOwnerConversion(t *testing.T) {
	type oldModel struct {
		Record
		Name string `json:"name"`
	}
	type currentModel struct {
		Record
		Title string `json:"title"`
	}
	calls := 0
	convert := func(raw json.RawMessage) (json.RawMessage, error) {
		calls++
		var old oldModel
		if err := json.Unmarshal(raw, &old); err != nil {
			return nil, err
		}
		return json.Marshal(currentModel{Record: old.Record, Title: old.Name})
	}
	declaration := Entity{Type: "example.item", Model: currentModel{}, RecordFormat: "item.v2",
		RecordImages: []RecordImageFormat{{Format: "", Model: oldModel{}, Convert: convert}}}
	info, err := Describe("example", declaration, func(reflect.Type) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	stamp := Stamp{By: "member", At: time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC), Change: "chg-2"}
	old := oldModel{Record: Record{ID: "one", Revision: 2, Created: stamp, Changed: stamp, Archived: true}, Name: "Saved"}
	raw, _ := json.Marshal(old)
	before := string(raw)
	decoded, err := DecodeRecordImage(info, "", raw)
	var current currentModel
	if err != nil || json.Unmarshal(decoded, &current) != nil || current.Title != "Saved" || !reflect.DeepEqual(current.Record, old.Record) || string(raw) != before || calls != 1 {
		t.Fatalf("conversion changed content, identity or original bytes: %s, %v", decoded, err)
	}
	if _, err := DecodeRecordImage(info, "item.v2", decoded); err != nil || calls != 1 {
		t.Fatalf("current image was converted again: %v", err)
	}
	for _, invalid := range []json.RawMessage{
		json.RawMessage(`{"id":"one","name":"Saved","unknown":true}`),
		json.RawMessage(`{"id":"one","name":"Saved"} {}`),
	} {
		if _, err := DecodeRecordImage(info, "", invalid); err == nil || calls != 1 {
			t.Fatalf("invalid old image reached the converter: %s, %v", invalid, err)
		}
	}
	if _, err := DecodeRecordImage(info, "item.future", raw); err == nil || calls != 1 {
		t.Fatalf("unknown format reached the converter: %v", err)
	}
	info.RecordImages[0].Convert = func(raw json.RawMessage) (json.RawMessage, error) {
		var old oldModel
		_ = json.Unmarshal(raw, &old)
		old.Revision++
		return json.Marshal(currentModel{Record: old.Record, Title: old.Name})
	}
	if _, err := DecodeRecordImage(info, "", raw); err == nil {
		t.Fatal("conversion changed the saved record revision")
	}
	info.RecordImages[0].Convert = func(raw json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"id":"one","title":"Saved","unknown":true}`), nil
	}
	if _, err := DecodeRecordImage(info, "", raw); err == nil {
		t.Fatal("conversion returned an invalid current shape")
	}
}

func TestRecordImageFormatDeclarations(t *testing.T) {
	type item struct{ Record }
	convert := func(raw json.RawMessage) (json.RawMessage, error) { return raw, nil }
	for _, images := range [][]RecordImageFormat{
		{{Format: "current", Model: item{}, Convert: convert}},
		{{Format: "old", Model: item{}, Convert: convert}, {Format: "old", Model: item{}, Convert: convert}},
		{{Format: "old", Model: struct{}{}, Convert: convert}},
		{{Format: "old", Model: item{}}},
	} {
		if _, err := Describe("example", Entity{Type: "example.item", Model: item{}, RecordFormat: "current", RecordImages: images}, func(reflect.Type) string { return "" }); err == nil {
			t.Fatalf("invalid record image declaration accepted: %+v", images)
		}
	}
}
