package platformserver

import (
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/platform"
	"testing"
	"time"
)

func TestApplicationHeaderFreezeBindingsProjectionAndRecovery(t *testing.T) {
	const tenant = "application-header-state"
	compose := func() *Tenant {
		tn, err := NewTenant(tenant, NewConsole(tenant, Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder}}}, Seat{Subjects: []string{"reader"}, Member: platform.Member{ID: "reader", Roles: map[string]string{build.ID: build.User}}}), build.New(tenant))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := compose()
	member, _ := tn.Member("builder")
	var entries []Entry
	key := 0
	at := time.Now().UTC()
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	submit := func(typ, id, verb string, payload any) {
		t.Helper()
		key++
		_, err := tn.Submit(member, &pb.Submission{TenantId: tenant, PrincipalId: member.ID, Authority: build.ID, IdempotencyKey: fmt.Sprint(key), Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: typ + "." + verb, Version: 1}, Payload: platform.Raw(payload)}, at)
		if err != nil {
			t.Fatal(typ, verb, err)
		}
	}
	submit(build.ObjectType, "object", "create", map[string]any{"name": "note", "title": "Note", "access": []build.Access{{Role: build.User, Read: "all"}}, "fields": []build.Field{{Name: "bucket", Title: "Bucket", Type: "text"}, {Name: "secret", Title: "Secret", Type: "text", Read: []string{build.Builder}}}})
	submit(build.ObjectType, "object", "publish", map[string]any{})
	object := platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: "build.note"}
	submit(build.QueryType, "query", "create", platform.NamedQuery{Name: "shared", Title: "Shared", Description: "Fixed query", Object: object.Name, Domain: json.RawMessage(`[["bucket","=","A"]]`), Limit: 20})
	submit(build.QueryType, "query", "publish", map[string]any{})
	doc := &platform.PageDocument{FormatVersion: 2, UIProfile: platform.PageUIProfile(), Root: "root", Nodes: map[string]platform.PageLayoutNode{"root": {Kind: "rows", Children: []string{"table"}}, "table": {Kind: "widget", Section: "table"}}, Variables: map[string]platform.PageVariable{"window": {Scope: "application", Type: "object-set", Mode: "shared", Source: &platform.PageResourceSource{Kind: "application", Variable: "window", Object: &object}}}}
	for _, id := range []string{"first", "second"} {
		submit(build.PageType, id, "create", map[string]any{"name": id, "title": id, "object": object.Name, "sections": []build.Section{{ID: "table", Widget: "table", ConfigVersion: 1, Fields: []string{"bucket"}, CollectionVariable: "window"}}, "document": doc})
		submit(build.PageType, id, "publish", map[string]any{})
	}
	variables := map[string]platform.PageVariable{"window": {Scope: "application", Type: "object-set", Mode: "resource", Source: &platform.PageResourceSource{Kind: "plan", Query: "read"}}}
	queries := map[string]platform.PageQuery{"read": {Object: object, Limit: 10, Query: &platform.AssetBinding{Ref: platform.AssetRef{App: build.ID, Kind: platform.AssetQuery, Name: "shared"}, SourceVersion: "1.query-1"}}}
	submit(build.AppType, "app", "create", map[string]any{"name": "notes", "title": "Notes", "pages": []string{"first", "second"}, "uiProfile": platform.PageUIProfile(), "variables": variables, "queries": queries, "header": platform.ApplicationHeader{Variant: "horizontal", Title: "Frozen application", Items: []platform.ApplicationHeaderItem{{Kind: "title"}, {Kind: "tabs", Pages: []string{"second", "first"}}, {Kind: "button", Label: "Refresh", Action: "refresh"}, {Kind: "button", Label: "Theme", Action: "theme"}}}})
	preview, err := tn.PreviewRelease(member, platform.AssetApp, "app")
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("preview %+v %v", preview, err)
	}
	if _, err = tn.SaveReleaseCandidate(member, platform.AssetApp, "app", preview.CandidateID, "freeze", at); err != nil {
		t.Fatal(err)
	}
	changed := map[string]platform.PageQuery{"read": queries["read"]}
	q := changed["read"]
	q.Limit = 1
	changed["read"] = q
	submit(build.AppType, "app", "edit", map[string]any{"queries": changed, "header": platform.ApplicationHeader{Variant: "vertical", Title: "Later application", Items: []platform.ApplicationHeaderItem{{Kind: "tabs", Pages: []string{"first"}}}}})
	if _, err = tn.ActivateRelease(member, preview.CandidateID, "activate", at); err != nil {
		t.Fatal(err)
	}
	submit(build.QueryType, "query", "edit", map[string]any{"domain": [][]any{{"bucket", "=", "B"}}})
	submit(build.QueryType, "query", "publish", map[string]any{})
	candidate, err := tn.ReleaseCandidate([]platform.AssetRef{{App: build.ID, Kind: platform.AssetApp, Name: "notes"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, asset := range candidate.Assets {
		if asset.Ref.Kind == platform.AssetQuery && asset.SourceVersion != "1.query-1" {
			t.Fatal("app query silently upgraded")
		}
	}
	CheckReplay(t, tn, entries, compose)
	raw, _, err := tn.Snapshot(func() int64 { return int64(len(entries)) })
	if err != nil {
		t.Fatal(err)
	}
	restored := compose()
	if err = restored.Restore(raw); err != nil {
		t.Fatal(err)
	}
	for _, current := range []*Tenant{tn, restored} {
		for _, d := range current.definitions {
			if d.Application != nil && d.Ref.Name == "notes" {
				if d.Application.Header == nil || d.Application.Header.Title != "Frozen application" || d.Application.Header.Variant != "horizontal" || d.Application.Header.Items[1].Pages[0] != "second" {
					t.Fatal("frozen application header changed")
				}
				if d.Application.Queries["read"].Limit != 10 || d.Application.Queries["read"].Query.SourceVersion != "1.query-1" {
					t.Fatal("frozen app query drifted")
				}
			}
		}
	}
	// Private fields cannot leak through application query metadata or resources.
	private := platform.Application{Name: "private", Title: "Private", Pages: []string{"first"}, UIProfile: platform.PageUIProfile(), Variables: variables, Queries: map[string]platform.PageQuery{"read": {Object: object, Limit: 10, Conditions: []platform.PageQueryCondition{{Field: "secret", Op: "=", Value: platform.PageValue{Literal: json.RawMessage(`"private"`)}}}}}}
	if err := tn.InstallApplication(tn.app(build.ID), private); err != nil {
		t.Fatal(err)
	}
	reader, _ := tn.Member("reader")
	for _, d := range tn.Definitions(reader) {
		if d.Application != nil && d.Ref.Name == "private" {
			if len(d.Application.Queries) != 0 || len(d.Application.Variables) != 0 {
				t.Fatal("private app query metadata exposed")
			}
		}
	}
}
