package platformserver

import (
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/platform"
	"testing"
	"time"
)

func TestNestedLoopFreezeReplayAndSnapshot(t *testing.T) {
	const tenant = "nested-plan-state"
	compose := func() *Tenant {
		tn, err := NewTenant(tenant, NewConsole(tenant, Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder}}}), build.New(tenant))
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
		_, problem := tn.Submit(member, &pb.Submission{TenantId: tenant, PrincipalId: member.ID, Authority: build.ID, IdempotencyKey: fmt.Sprint(key), Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: typ + "." + verb, Version: 1}, Payload: platform.Raw(payload)}, at)
		if problem != nil {
			t.Fatal(problem.Message)
		}
	}
	for _, name := range []string{"parent", "child"} {
		fields := []build.Field{{Name: "note", Title: "Note", Type: "text"}}
		if name == "child" {
			fields = append(fields, build.Field{Name: "parent", Title: "Parent", Type: "reference", Ref: "build.parent", Required: true})
		}
		submit(build.ObjectType, name, "create", map[string]any{"name": name, "title": name, "fields": fields})
		submit(build.ObjectType, name, "publish", map[string]any{})
	}
	submit(build.QueryType, "query", "create", map[string]any{"name": "children", "title": "Children", "description": "Fixed children by parent", "object": "build.child", "by": "parent", "limit": 3})
	submit(build.QueryType, "query", "publish", map[string]any{})
	parent := platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: "build.parent"}
	child := platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: "build.child"}
	document := &platform.PageDocument{FormatVersion: 2, UIProfile: platform.PageUIProfile(), Root: "root", Nodes: map[string]platform.PageLayoutNode{
		"root": {Kind: "rows", Children: []string{"parents"}}, "parents": {Kind: "loop", Children: []string{"children"}, Loop: &platform.PageLoop{Collection: "parentWindow", ItemVariable: "parentItem", Limit: 2}}, "children": {Kind: "loop", Children: []string{"detail"}, Loop: &platform.PageLoop{Collection: "childWindow", ItemVariable: "childItem", Limit: 3}}, "detail": {Kind: "widget", Section: "detail"}}, Variables: map[string]platform.PageVariable{
		"parentWindow": {Scope: "page", Type: "object-set", Mode: "resource", Source: &platform.PageResourceSource{Kind: "plan", Query: "parents"}}, "parentItem": {Scope: "loop-item", Owner: "parents", Type: "record", Mode: "resource", Source: &platform.PageResourceSource{Kind: "item", Node: "parents"}}, "childWindow": {Scope: "loop-item", Owner: "parents", Type: "object-set", Mode: "resource", Source: &platform.PageResourceSource{Kind: "plan", Query: "children"}}, "childItem": {Scope: "loop-item", Owner: "children", Type: "record", Mode: "resource", Source: &platform.PageResourceSource{Kind: "item", Node: "children"}}}, Queries: map[string]platform.PageQuery{
		"parents": {Object: parent, Limit: 2}, "children": {Object: child, ItemOwner: "parents", Limit: 3, Query: &platform.AssetBinding{Ref: platform.AssetRef{App: build.ID, Kind: platform.AssetQuery, Name: "children"}, SourceVersion: "1.query-1"}, For: &platform.PageValue{Variable: "parentItem"}}}}
	submit(build.PageType, "page", "create", map[string]any{"name": "work", "title": "Work", "object": "build.parent", "sections": []build.Section{{ID: "detail", Widget: "detail", ConfigVersion: 1, Object: "build.child", Fields: []string{"note"}, RecordVariable: "childItem"}}, "document": document})
	submit(build.PageType, "page", "publish", map[string]any{})
	preview, err := tn.PreviewRelease(member, platform.AssetPage, "page")
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("preview %+v %v", preview, err)
	}
	if _, err = tn.SaveReleaseCandidate(member, platform.AssetPage, "page", preview.CandidateID, "save", at); err != nil {
		t.Fatal(err)
	}
	changed := *document
	changed.Queries = map[string]platform.PageQuery{"parents": document.Queries["parents"], "children": document.Queries["children"]}
	q := changed.Queries["children"]
	q.Limit = 1
	changed.Queries["children"] = q
	submit(build.PageType, "page", "edit", map[string]any{"document": &changed})
	submit(build.QueryType, "query", "edit", map[string]any{"limit": 1})
	submit(build.QueryType, "query", "publish", map[string]any{})
	if _, err = tn.ActivateRelease(member, preview.CandidateID, "activate", at); err != nil {
		t.Fatal(err)
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
		found := false
		for _, d := range current.definitions {
			if d.Page != nil && d.Ref.Name == "work" {
				found = true
				if d.Page.Document.Queries["children"].Limit != 3 || d.Page.Document.Queries["children"].ItemOwner != "parents" || d.Page.Document.Queries["children"].For.Variable != "parentItem" || d.Page.Document.Queries["children"].Query.SourceVersion != "1.query-1" || d.Page.Document.Variables["childWindow"].Owner != "parents" || d.Page.Document.Nodes["children"].Loop.Collection != "childWindow" {
					t.Fatal("query plan or output was not frozen")
				}
			}
		}
		if !found {
			t.Fatal("query page missing")
		}
	}
}
