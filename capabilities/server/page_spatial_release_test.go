package platformserver

import (
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/apps/files"
	"platformserver/platform"
	"testing"
	"time"
)

func TestSpatialCandidateFreezesSceneMapAndFileBindingsAndRecovers(t *testing.T) {
	compose := func() *Tenant {
		tn, err := NewTenant("spatial", NewConsole("spatial", Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder}}}, Seat{Subjects: []string{"reader"}, Member: platform.Member{ID: "reader", Roles: map[string]string{build.ID: build.User}}}), build.New("spatial"), files.New("spatial"))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := compose()
	builder, _ := tn.Member("builder")
	reader, _ := tn.Member("reader")
	at := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	var entries []Entry
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	key := 0
	submit := func(typ, id, verb string, payload any) {
		t.Helper()
		key++
		_, err := tn.Submit(builder, &pb.Submission{TenantId: tn.ID, PrincipalId: builder.ID, Authority: build.ID, IdempotencyKey: fmt.Sprint(key), Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: typ + "." + verb, Version: 1}, Payload: platform.Raw(payload)}, at)
		if err != nil {
			t.Fatal(typ, id, verb, err.Message)
		}
	}
	submit(build.ObjectType, "asset", "create", map[string]any{"name": "asset", "title": "Assets", "fields": []build.Field{{Name: "name", Title: "Name", Type: "text"}, {Name: "lat", Title: "Latitude", Type: "decimal"}, {Name: "lng", Title: "Longitude", Type: "decimal"}}})
	submit(build.ObjectType, "asset", "publish", map[string]any{})
	submit(build.ObjectType, "sample", "create", map[string]any{"name": "sample", "title": "Samples", "fields": []build.Field{{Name: "asset", Title: "Asset", Type: "reference", Ref: "build.asset"}, {Name: "at", Title: "Event time", Type: "datetime"}, {Name: "signal", Title: "Signal", Type: "decimal"}}})
	submit(build.ObjectType, "sample", "publish", map[string]any{})
	submit(build.QueryType, "latest", "create", map[string]any{"name": "latest", "title": "Latest sample", "description": "Original asset samples", "object": "build.sample", "by": "asset", "sort": []string{"-at", "id"}, "limit": 1})
	submit(build.QueryType, "latest", "publish", map[string]any{})
	asset := platform.AssetRef{App: "build", Kind: platform.AssetObject, Name: "build.asset"}
	sample := platform.AssetRef{App: "build", Kind: platform.AssetObject, Name: "build.sample"}
	sections := []build.Section{{ID: "map", Widget: "record-map", ConfigVersion: 1, CollectionVariable: "assets", Map: &platform.PageRecordMap{LatitudeField: "lat", LongitudeField: "lng", LabelField: "name", ClusterEnabled: true}}, {ID: "image", Widget: "image-annotation", ConfigVersion: 1, RecordVariable: "active", FileVariable: "imageFile"}, {ID: "scene", Widget: "scene-3d", ConfigVersion: 1, RecordVariable: "active", FileVariable: "modelFile", SceneSampleCollectionVariable: "samples", ScenePartVariable: "part", Scene: &platform.PageSceneConfig{Background: "dark", Quality: "balanced", Layers: []platform.PageSceneLayer{{ID: "layer", Name: "Original layer", Nodes: []string{"Part"}, Visible: true, Opacity: .8}}, Mappings: []platform.PageSceneMapping{{ID: "drive", Node: "Part", Source: "sample", Field: "signal", Mode: "position", Axis: "x", InputMin: 0, InputMax: 20, OutputMin: 0, OutputMax: 1, Enabled: true}}, SampleAssetField: "asset", SampleTimeField: "at"}}}
	doc := platform.PageDocument{FormatVersion: 2, UIProfile: platform.PageUIProfile(), Root: "root", Nodes: map[string]platform.PageLayoutNode{"root": {Kind: "rows", Children: []string{"map", "image", "scene"}}, "map": {Kind: "widget", Section: "map"}, "image": {Kind: "widget", Section: "image"}, "scene": {Kind: "widget", Section: "scene"}}, Queries: map[string]platform.PageQuery{"assets": {Object: asset, Sort: []string{"id"}, Limit: 100}, "samples": {Object: sample, Query: &platform.AssetBinding{Ref: platform.AssetRef{App: "build", Kind: platform.AssetQuery, Name: "latest"}, SourceVersion: "1.query-1"}, For: &platform.PageValue{Variable: "active"}, Sort: []string{"-at", "id"}, Limit: 1}}, Variables: map[string]platform.PageVariable{"assets": {Scope: "page", Type: "object-set", Mode: "resource", Source: &platform.PageResourceSource{Kind: "plan", Query: "assets"}}, "samples": {Scope: "page", Type: "object-set", Mode: "resource", Source: &platform.PageResourceSource{Kind: "plan", Query: "samples"}}, "active": {Scope: "page", Type: "record", Mode: "resource", Source: &platform.PageResourceSource{Kind: "record", Section: "map"}}}}
	for _, id := range []string{"imageFile", "modelFile", "part"} {
		doc.Variables[id] = platform.PageVariable{Scope: "page", Type: "string", Mode: "state", Initial: platform.Raw("")}
	}
	submit(build.PageType, "P", "create", map[string]any{"name": "spatial", "title": "Spatial task", "object": asset.Name, "sections": sections, "document": doc})
	preview, err := tn.PreviewRelease(builder, platform.AssetPage, "P")
	if err != nil || preview.Diagnostic != "" {
		t.Fatal(preview, err)
	}
	saved, err := tn.SaveReleaseCandidate(builder, platform.AssetPage, "P", preview.CandidateID, "save", at)
	if err != nil {
		t.Fatal(err)
	}
	sections[2].Scene.Layers[0].Opacity = .1
	submit(build.PageType, "P", "edit", map[string]any{"sections": sections})
	if _, err := tn.ActivateRelease(builder, saved, "activate", at); err != nil {
		t.Fatal(err)
	}
	check := func(current *Tenant) {
		t.Helper()
		found := false
		for _, d := range current.Definitions(reader) {
			if d.Page == nil || d.Ref.Name != "spatial" {
				continue
			}
			found = true
			p := d.Page
			if p.Sections[0].Map.LatitudeField != "lat" || p.Sections[1].FileVariable != "imageFile" || p.Sections[2].Scene.Layers[0].Opacity != .8 || p.Document.Queries["samples"].Query.SourceVersion != "1.query-1" {
				t.Fatal("later draft changed frozen spatial bindings")
			}
		}
		if !found {
			t.Fatal("member spatial page disappeared")
		}
	}
	check(tn)
	CheckReplay(t, tn, entries, compose)
	raw, _, err := tn.Snapshot(func() int64 { return int64(len(entries)) })
	if err != nil {
		t.Fatal(err)
	}
	again := compose()
	if err := again.Restore(raw); err != nil {
		t.Fatal(err)
	}
	check(again)
}
