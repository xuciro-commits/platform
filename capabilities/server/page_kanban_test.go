package platformserver

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/platform"
)

func TestKanbanReleaseRetainsMappingsAndMemberMoves(t *testing.T) {
	compose := func() *Tenant {
		tn, err := NewTenant("kanban", NewConsole("kanban", Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder}}}, Seat{Subjects: []string{"reader"}, Member: platform.Member{ID: "reader", Roles: map[string]string{build.ID: build.User}}}), build.New("kanban"))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := compose()
	builder, _ := tn.Member("builder")
	reader, _ := tn.Member("reader")
	at := time.Date(2026, 10, 2, 16, 0, 0, 0, time.UTC)
	key := 0
	var entries []Entry
	tn.Record = func(e Entry) { entries = append(entries, e) }
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	submit := func(typ, id, schema string, payload any) {
		t.Helper()
		key++
		if _, err := tn.Submit(builder, &pb.Submission{TenantId: tn.ID, PrincipalId: builder.ID, Authority: build.ID, IdempotencyKey: fmt.Sprint(key), Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: platform.Raw(payload)}, at); err != nil {
			t.Fatalf("%s: %s", schema, err.Message)
		}
	}
	submit(build.ObjectType, "O", build.ObjectType+".create", map[string]any{"name": "task", "title": "Task", "fields": []build.Field{{Name: "title", Title: "Title", Type: "text", Read: []string{build.Builder}}}, "states": []build.State{{Name: "open", Title: "Open"}, {Name: "done", Title: "Done"}}, "actions": []build.Action{{Name: "close", Title: "Close", From: []string{"open"}, To: "done", Roles: []string{build.Builder}}}})
	submit(build.ObjectType, "O", build.SchemaPublish, struct{}{})
	object := platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: "build.task"}
	doc := platform.PageDocument{FormatVersion: 2, UIProfile: platform.PageUIProfile(), Root: "root", Nodes: map[string]platform.PageLayoutNode{"root": {Kind: "rows", Children: []string{"board"}}, "board": {Kind: "widget", Section: "board"}}, Variables: map[string]platform.PageVariable{"window": {Scope: "page", Type: "object-set", Mode: "resource", Source: &platform.PageResourceSource{Kind: "plan", Query: "read"}}}, Queries: map[string]platform.PageQuery{"read": {Object: object, Limit: 1}}}
	sections := []build.Section{{ID: "board", Widget: "kanban", ConfigVersion: 1, Title: "Tasks", CollectionVariable: "window", CardLabel: "id", Actions: []string{"build.task.close"}}}
	submit(build.PageType, "P", build.PageType+".create", map[string]any{"name": "tasks", "title": "Tasks", "object": "build.task", "sections": sections, "document": doc})
	preview, err := tn.PreviewRelease(builder, platform.AssetPage, "P")
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("preview %+v %v", preview, err)
	}
	saved, err := tn.SaveReleaseCandidate(builder, platform.AssetPage, "P", preview.CandidateID, "save", at)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := platform.ReadCandidate(saved, tn.releases.candidates[saved])
	if err != nil {
		t.Fatal(err)
	}
	for i, a := range candidate.Assets {
		if a.Ref.Kind != platform.AssetPage {
			continue
		}
		var p platform.Page
		_ = json.Unmarshal(a.Body, &p)
		p.Sections[0].Actions = []platform.AssetRef{{App: build.ID, Kind: platform.AssetAction, Name: "build.task.edit"}}
		bad := append([]platform.ReleaseAsset(nil), candidate.Assets...)
		bad[i].Body = platform.Raw(p)
		if _, err := platform.Candidate([]platform.AssetRef{a.Ref}, bad); err == nil {
			t.Fatal("candidate accepted a direct state/edit move")
		}
	}
	sections[0].Actions = nil
	sections[0].CardLabel = "title"
	submit(build.PageType, "P", build.PageType+".edit", map[string]any{"sections": sections})
	if _, err := tn.ActivateRelease(builder, saved, "activate", at); err != nil {
		t.Fatal(err)
	}
	check := func(tn *Tenant) {
		t.Helper()
		seen := false
		for _, d := range tn.Definitions(builder) {
			if d.Page != nil && d.Ref.Name == "tasks" {
				seen = true
				if len(d.Page.Sections) != 1 || d.Page.Sections[0].CardLabel != "id" || len(d.Page.Sections[0].Actions) != 1 {
					t.Fatal("mutable draft replaced frozen board")
				}
			}
		}
		if !seen {
			t.Fatal("builder board disappeared")
		}
		for _, d := range tn.Definitions(reader) {
			if d.Page != nil && d.Ref.Name == "tasks" {
				if len(d.Page.Sections) != 1 || len(d.Page.Sections[0].Actions) != 0 {
					t.Fatal("private move action exposed")
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
	if err := restored.Restore(image); err != nil {
		t.Fatal(err)
	}
	check(restored)
	// Publishing a different board with a hidden title does not substitute ID.
	sections[0].CardLabel = "title"
	submit(build.PageType, "PRIVATE", build.PageType+".create", map[string]any{"name": "privatetasks", "title": "Private tasks", "object": "build.task", "sections": sections, "document": doc})
	submit(build.PageType, "PRIVATE", build.SchemaRelease, struct{}{})
	for _, d := range tn.Definitions(reader) {
		if d.Page != nil && d.Ref.Name == "privatetasks" && len(d.Page.Sections) > 0 {
			t.Fatal("hidden card title fell back to an ID board")
		}
	}
}
