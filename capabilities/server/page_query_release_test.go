package platformserver

import (
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/platform"
	"testing"
	"time"
)

func TestIndependentQueryPlanFreezesAndReplays(t *testing.T) {
	const tenant = "query-plan-state"
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
	submit(build.ObjectType, "object", "create", map[string]any{"name": "note", "title": "Note", "fields": []build.Field{{Name: "note", Title: "Note", Type: "text"}}})
	submit(build.ObjectType, "object", "publish", map[string]any{})
	document := &platform.PageDocument{FormatVersion: 2, UIProfile: platform.PageUIProfile(), Root: "root", Nodes: map[string]platform.PageLayoutNode{"root": {Kind: "rows", Children: []string{"text"}}, "text": {Kind: "widget", Section: "text"}}, Variables: map[string]platform.PageVariable{"window": {Scope: "page", Type: "object-set", Mode: "resource", Source: &platform.PageResourceSource{Kind: "plan", Query: "read"}}}, Queries: map[string]platform.PageQuery{"read": {Object: platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: "build.note"}, Limit: 20, Sort: []string{"id"}}}}
	submit(build.PageType, "page", "create", map[string]any{"name": "work", "title": "Work", "object": "build.note", "sections": []build.Section{{ID: "text", Widget: "text", ConfigVersion: 1, Text: "Read window"}}, "document": document})
	submit(build.PageType, "page", "publish", map[string]any{})
	preview, err := tn.PreviewRelease(member, platform.AssetPage, "page")
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("preview %+v %v", preview, err)
	}
	if _, err = tn.SaveReleaseCandidate(member, platform.AssetPage, "page", preview.CandidateID, "save", at); err != nil {
		t.Fatal(err)
	}
	changed := *document
	changed.Queries = map[string]platform.PageQuery{"read": document.Queries["read"]}
	q := changed.Queries["read"]
	q.Limit = 1
	changed.Queries["read"] = q
	submit(build.PageType, "page", "edit", map[string]any{"document": &changed})
	if _, err = tn.ActivateRelease(member, preview.CandidateID, "activate", at); err != nil {
		t.Fatal(err)
	}
	replayed := compose()
	if err = replayed.Replay(entries); err != nil {
		t.Fatal(err)
	}
	for _, current := range []*Tenant{tn, replayed} {
		found := false
		for _, d := range current.definitions {
			if d.Page != nil && d.Ref.Name == "work" {
				found = true
				if d.Page.Document.Queries["read"].Limit != 20 || d.Page.Document.Variables["window"].Source.Query != "read" {
					t.Fatal("query plan or output was not frozen")
				}
			}
		}
		if !found {
			t.Fatal("query page missing")
		}
	}
}
