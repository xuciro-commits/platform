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

func TestCollectionBuilderCandidateFreezesOriginalFieldsAndQueryInputAndRecovers(t *testing.T) {
	compose := func() *Tenant {
		tn, err := NewTenant("buildercollection", NewConsole("buildercollection", Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder}}}, Seat{Subjects: []string{"reader"}, Member: platform.Member{ID: "reader", Roles: map[string]string{build.ID: build.User}}}), build.New("buildercollection"), files.New("buildercollection"))
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

	submit(build.ObjectType, "asset", "create", map[string]any{"name": "asset", "title": "Original assets", "fields": []build.Field{{Name: "name", Title: "Name", Type: "text"}, {Name: "qty", Title: "Quantity", Type: "decimal"}}})
	submit(build.ObjectType, "asset", "publish", map[string]any{})
	submit(build.QueryType, "original", "create", map[string]any{"name": "original", "title": "Original source", "description": "Original bounded source", "object": "build.asset", "sort": []string{"id"}, "limit": 2})
	submit(build.QueryType, "original", "publish", map[string]any{})
	asset := platform.AssetRef{App: "build", Kind: platform.AssetObject, Name: "build.asset"}
	sections := []build.Section{{ID: "builder", Widget: "collection-builder", ConfigVersion: 1, CollectionVariable: "base", CollectionOutputVariable: "output", CollectionBuilder: &platform.PageCollectionBuilder{Fields: []string{"name", "qty"}}}, {ID: "table", Widget: "table", ConfigVersion: 1, CollectionVariable: "result", Fields: []string{"name", "qty"}}, {ID: "count", Widget: "metric", ConfigVersion: 1, CollectionVariable: "result", Measure: "count"}}
	doc := platform.PageDocument{FormatVersion: 2, UIProfile: platform.PageUIProfile(), Root: "root", Nodes: map[string]platform.PageLayoutNode{"root": {Kind: "rows", Children: []string{"builder", "table", "count"}}, "builder": {Kind: "widget", Section: "builder"}, "table": {Kind: "widget", Section: "table"}, "count": {Kind: "widget", Section: "count"}}, Queries: map[string]platform.PageQuery{"base": {Object: asset, Query: &platform.AssetBinding{Ref: platform.AssetRef{App: "build", Kind: platform.AssetQuery, Name: "original"}, SourceVersion: "1.query-1"}, Sort: []string{"id"}, Limit: 2}, "result": {Object: asset, Input: "output", Limit: 3}}, Variables: map[string]platform.PageVariable{"base": {Scope: "page", Type: "object-set", Mode: "resource", Source: &platform.PageResourceSource{Kind: "plan", Query: "base"}}, "output": {Scope: "page", Type: "object-set", Mode: "resource", Source: &platform.PageResourceSource{Kind: "query", Section: "builder"}}, "result": {Scope: "page", Type: "object-set", Mode: "resource", Source: &platform.PageResourceSource{Kind: "plan", Query: "result"}}}}
	submit(build.PageType, "P", "create", map[string]any{"name": "collectiontask", "title": "Collection task", "object": asset.Name, "sections": sections, "document": doc})
	preview, err := tn.PreviewRelease(builder, platform.AssetPage, "P")
	if err != nil || preview.Diagnostic != "" {
		t.Fatal(preview, err)
	}
	saved, err := tn.SaveReleaseCandidate(builder, platform.AssetPage, "P", preview.CandidateID, "save", at)
	if err != nil {
		t.Fatal(err)
	}
	sections[0].CollectionBuilder.Fields = []string{"name"}
	submit(build.PageType, "P", "edit", map[string]any{"sections": sections})
	if _, err := tn.ActivateRelease(builder, saved, "activate", at); err != nil {
		t.Fatal(err)
	}
	check := func(current *Tenant) {
		t.Helper()
		found := false
		for _, d := range current.Definitions(reader) {
			if d.Page == nil || d.Ref.Name != "collectiontask" {
				continue
			}
			found = true
			p := d.Page
			if len(p.Sections[0].CollectionBuilder.Fields) != 2 || p.Document.Queries["result"].Input != "output" || p.Document.Queries["base"].Query.SourceVersion != "1.query-1" {
				t.Fatal("later draft changed frozen collection bindings")
			}
		}
		if !found {
			t.Fatal("collection page disappeared")
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
