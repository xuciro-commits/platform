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

func TestFacetFrozenDeclarationsMemberFieldsAndRecovery(t *testing.T) {
	compose := func() *Tenant {
		tn, err := NewTenant("facets", NewConsole("facets", Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder}}}, Seat{Subjects: []string{"reader"}, Member: platform.Member{ID: "reader", Roles: map[string]string{build.ID: build.User}}}), build.New("facets"))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := compose()
	builder, _ := tn.Member("builder")
	reader, _ := tn.Member("reader")
	var entries []Entry
	at := time.Now().UTC()
	key := 0
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	submit := func(typ, id, verb string, payload any) {
		t.Helper()
		key++
		_, err := tn.Submit(builder, &pb.Submission{TenantId: tn.ID, PrincipalId: builder.ID, Authority: build.ID, IdempotencyKey: fmt.Sprint(key), Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: typ + "." + verb, Version: 1}, Payload: platform.Raw(payload)}, at)
		if err != nil {
			t.Fatal(err)
		}
	}
	submit(build.ObjectType, "O", "create", map[string]any{"name": "asset", "title": "Assets", "access": []build.Access{{Role: build.User, Read: "all"}}, "fields": []build.Field{{Name: "name", Title: "Name", Type: "text"}, {Name: "secret", Title: "Secret", Type: "text", Read: []string{build.Builder}}}})
	submit(build.ObjectType, "O", "publish", map[string]any{})
	object := platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: "build.asset"}
	doc := platform.PageDocument{FormatVersion: 2, UIProfile: platform.PageUIProfile(), Root: "root", Nodes: map[string]platform.PageLayoutNode{"root": {Kind: "rows", Children: []string{"filter", "table"}}, "filter": {Kind: "widget", Section: "filter"}, "table": {Kind: "widget", Section: "table"}}, Variables: map[string]platform.PageVariable{"picked": {Scope: "page", Type: "string-set", Mode: "state", Initial: json.RawMessage(`{"kind":"string-set","values":[]}`)}, "window": {Scope: "page", Type: "object-set", Mode: "resource", Source: &platform.PageResourceSource{Kind: "plan", Query: "source"}}, "filtered": {Scope: "page", Type: "object-set", Mode: "resource", Source: &platform.PageResourceSource{Kind: "plan", Query: "filtered"}}}, Queries: map[string]platform.PageQuery{"source": {Object: object, Limit: 1}, "filtered": {Object: object, Limit: 20, Conditions: []platform.PageQueryCondition{{Field: "secret", Op: "in", Optional: true, Value: platform.PageValue{Variable: "picked"}}}}}}
	sections := []build.Section{{ID: "filter", Widget: "filter", ConfigVersion: 1, CollectionVariable: "window", Facets: []platform.PageFacet{{Field: "secret", Variable: "picked", Kind: "checkbox"}}}, {ID: "table", Widget: "table", ConfigVersion: 1, CollectionVariable: "filtered", Fields: []string{"name"}}}
	submit(build.PageType, "P", "create", map[string]any{"name": "assets", "title": "Assets", "object": object.Name, "document": doc, "sections": sections})
	preview, err := tn.PreviewRelease(builder, platform.AssetPage, "P")
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("preview %+v %v", preview, err)
	}
	if _, err = tn.SaveReleaseCandidate(builder, platform.AssetPage, "P", preview.CandidateID, "save", at); err != nil {
		t.Fatal(err)
	}
	sections[0].Facets[0].Field = "name"
	submit(build.PageType, "P", "edit", map[string]any{"sections": sections})
	if _, err = tn.ActivateRelease(builder, preview.CandidateID, "activate", at); err != nil {
		t.Fatal(err)
	}
	check := func(current *Tenant) {
		t.Helper()
		seen := false
		for _, d := range current.Definitions(builder) {
			if d.Page != nil && d.Ref.Name == "assets" {
				seen = true
				if d.Page.Sections[0].Facets[0].Field != "secret" || !d.Page.Document.Queries["filtered"].Conditions[0].Optional {
					t.Fatal("new draft replaced frozen facets")
				}
			}
		}
		if !seen {
			t.Fatal("missing frozen page")
		}
		for _, d := range current.Definitions(reader) {
			if d.Page != nil && d.Ref.Name == "assets" {
				for _, s := range d.Page.Sections {
					if s.Widget == "filter" || s.Widget == "table" {
						t.Fatal("hidden facet or predicate retained")
					}
				}
			}
		}
	}
	check(tn)
	CheckReplay(t, tn, entries, compose)
	image, _, err := tn.Snapshot(func() int64 { return 0 })
	if err != nil {
		t.Fatal(err)
	}
	restored := compose()
	if err = restored.Restore(image); err != nil {
		t.Fatal(err)
	}
	check(restored)
}
