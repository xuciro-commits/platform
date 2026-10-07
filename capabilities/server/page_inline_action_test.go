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

func TestInlineActionFrozenBindingAndMemberRetirement(t *testing.T) {
	compose := func() *Tenant {
		tn, err := NewTenant("inline", NewConsole("inline", Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder}}}, Seat{Subjects: []string{"reader"}, Member: platform.Member{ID: "reader", Roles: map[string]string{build.ID: build.User}}}), build.New("inline"))
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
	submit(build.ObjectType, "O", build.ObjectType+".create", map[string]any{"name": "task", "title": "Task", "fields": []build.Field{{Name: "title", Title: "Title", Type: "text"}}, "states": []build.State{{Name: "open", Title: "Open"}, {Name: "done", Title: "Done"}}, "actions": []build.Action{{Name: "close", Title: "Close", From: []string{"open"}, To: "done", Roles: []string{build.Builder}}}})
	submit(build.ObjectType, "O", build.SchemaPublish, struct{}{})
	doc := platform.PageDocument{FormatVersion: 2, UIProfile: platform.PageUIProfile(), Root: "root", Nodes: map[string]platform.PageLayoutNode{"root": {Kind: "rows", Children: []string{"inline", "table"}}, "inline": {Kind: "widget", Section: "inline"}, "table": {Kind: "widget", Section: "table"}}}
	sections := []build.Section{{ID: "inline", Widget: "inline-action", ConfigVersion: 1, Title: "Close task", Selection: "selected", Actions: []string{"build.task.close"}}, {ID: "table", Widget: "table", ConfigVersion: 1, Selection: "selected", Fields: []string{"title"}}}
	submit(build.PageType, "P", build.PageType+".create", map[string]any{"name": "tasks", "title": "Tasks", "object": "build.task", "sections": sections, "document": doc, "selections": []platform.SelectionVariable{{Name: "selected", Object: platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: "build.task"}}}})
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
	found := false
	for i, asset := range candidate.Assets {
		if asset.Ref.Kind != platform.AssetAction || asset.Ref.Name != "build.task.close" {
			continue
		}
		found = true
		var action platform.Action
		_ = json.Unmarshal(asset.Body, &action)
		action.New = true
		bad := append([]platform.ReleaseAsset(nil), candidate.Assets...)
		bad[i].Body = platform.Raw(action)
		if _, err := platform.Candidate([]platform.AssetRef{{App: build.ID, Kind: platform.AssetPage, Name: "tasks"}}, bad); err == nil {
			t.Fatal("frozen candidate accepted a creating action")
		}
	}
	if !found {
		t.Fatal("candidate did not freeze its original action dependency")
	}
	sections[0].Actions = []string{"build.task.edit"}
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
				if len(d.Page.Sections) != 2 || len(d.Page.Sections[0].Actions) != 1 || d.Page.Sections[0].Actions[0].Name != "build.task.close" || d.Page.Sections[0].Selection != "selected" {
					t.Fatal("draft replaced frozen action")
				}
			}
		}
		if !seen {
			t.Fatal("builder inline action disappeared")
		}
		for _, d := range tn.Definitions(reader) {
			if d.Page != nil && d.Ref.Name == "tasks" {
				for _, s := range d.Page.Sections {
					if s.Widget == "inline-action" {
						t.Fatal("member without original action retained its form")
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
	if err := restored.Restore(image); err != nil {
		t.Fatal(err)
	}
	check(restored)
}
