package build

import (
	"encoding/json"
	"reflect"
	"testing"

	"platformserver/platform"
)

func TestCodeAndBuilderPageCandidatesShareDescriptor(t *testing.T) {
	for _, tc := range []struct{ app, object, action, page string }{
		{"crm", "crm.account", "crm.account.create", "accounts"},
		{"mes", "mes.order", "mes.order.create", "orders"},
	} {
		t.Run(tc.app, func(t *testing.T) {
			obj := platform.AssetRef{App: tc.app, Kind: platform.AssetObject, Name: tc.object}
			act := platform.AssetRef{App: tc.app, Kind: platform.AssetAction, Name: tc.action}
			ref := platform.AssetRef{App: tc.app, Kind: platform.AssetPage, Name: tc.page}
			builderPage := descriptor(Page{Name: tc.page, Title: "Orders", Object: tc.object, List: []string{"name"}, Detail: []string{"name"}, Actions: []string{tc.action}})
			codePage := platform.Page{Name: tc.page, Title: "Orders", Object: obj, Layout: "list-detail", ListFields: []string{"name"}, DetailFields: []string{"name"}, Actions: []platform.AssetRef{act}}
			fromBuilder, err := platform.PageReleaseAsset(tc.app, "1", builderPage)
			if err != nil {
				t.Fatal(err)
			}
			fromCode, err := platform.PageReleaseAsset(tc.app, "1", codePage)
			if err != nil {
				t.Fatal(err)
			}
			available := []platform.ReleaseAsset{
				{Ref: obj, ContractVersion: 1, SourceVersion: "1", Body: json.RawMessage(`{"type":"` + tc.object + `"}`)},
				{Ref: act, ContractVersion: 1, SourceVersion: "1", Requires: []platform.AssetRef{obj}, Body: json.RawMessage(`{"schema":"` + tc.action + `","target":"` + tc.object + `"}`)},
				fromBuilder,
			}
			built, err := platform.Candidate([]platform.AssetRef{ref}, available)
			if err != nil {
				t.Fatal(err)
			}
			available[2] = fromCode
			coded, err := platform.Candidate([]platform.AssetRef{ref}, available)
			if err != nil || built.ID != coded.ID {
				t.Fatalf("code and builder diverged: %s %s, %v", built.ID, coded.ID, err)
			}
		})
	}
}

// A newly added widget option must survive both page representations without
// being added to separate conversion lists. The flattened encoding is historical.
func TestPageWidgetConfigRoundTripsAllFieldsAndKeepsFlatJSON(t *testing.T) {
	var section Section
	config := reflect.ValueOf(&section.PageWidgetConfig).Elem()
	for i := range config.NumField() {
		field := config.Field(i)
		switch field.Kind() {
		case reflect.Pointer:
			field.Set(reflect.New(field.Type().Elem()))
		case reflect.Slice:
			field.Set(reflect.MakeSlice(field.Type(), 1, 1))
		case reflect.String:
			field.SetString(config.Type().Field(i).Name)
		case reflect.Int:
			field.SetInt(7)
		default:
			t.Fatalf("populate widget field %s (%s)", config.Type().Field(i).Name, field.Kind())
		}
	}
	section.Widget, section.ID, section.ConfigVersion = "table", "table", 1
	page := Page{Name: "widgets", Object: "build.note", Sections: []Section{section}}
	installed := descriptor(page)
	restored, err := pageDraft(installed)
	if err != nil || !reflect.DeepEqual(restored.Sections[0], section) {
		t.Fatalf("widget configuration did not survive install and restore: %v", err)
	}
	for _, value := range []any{section, installed.Sections[0]} {
		typ, current := reflect.TypeOf(value), reflect.ValueOf(value)
		fields := make([]reflect.StructField, 0, typ.NumField()+config.NumField()-1)
		values := make([]reflect.Value, 0, typ.NumField()+config.NumField()-1)
		for i := range typ.NumField() {
			field := typ.Field(i)
			if field.Anonymous {
				for j := range field.Type.NumField() {
					fields = append(fields, field.Type.Field(j))
					values = append(values, current.Field(i).Field(j))
				}
			} else {
				fields = append(fields, field)
				values = append(values, current.Field(i))
			}
		}
		flat := reflect.New(reflect.StructOf(fields)).Elem()
		for i, field := range values {
			flat.Field(i).Set(field)
		}
		before, err := json.Marshal(flat.Interface())
		if err != nil {
			t.Fatal(err)
		}
		after, err := json.Marshal(value)
		if err != nil || string(before) != string(after) {
			t.Fatalf("flattened section JSON changed for %s: %v", typ, err)
		}
	}
}
