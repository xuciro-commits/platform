package platformserver

import (
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/platform"
	"testing"
	"time"
)

func TestFrozenRecordWorkPermissionsReplayAndSnapshot(t *testing.T) {
	compose := func() *Tenant {
		tn, err := NewTenant("recordwork", NewConsole("recordwork", Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder}}}, Seat{Subjects: []string{"reader"}, Member: platform.Member{ID: "reader", Roles: map[string]string{build.ID: build.User}}}), build.New("recordwork"))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := compose()
	builder, _ := tn.Member("builder")
	reader, _ := tn.Member("reader")
	at := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	key := 0
	var entries []Entry
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	submit := func(typ, id, verb string, payload any) {
		t.Helper()
		key++
		_, issue := tn.Submit(builder, &pb.Submission{TenantId: tn.ID, PrincipalId: builder.ID, Authority: build.ID, IdempotencyKey: fmt.Sprint(key), Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: typ + "." + verb, Version: 1}, Payload: platform.Raw(payload)}, at)
		if issue != nil {
			t.Fatal(typ, verb, issue.Message)
		}
	}

	fields := []build.Field{{Name: "name", Title: "Name", Type: "text"}, {Name: "qty", Title: "Private quantity", Type: "integer", Read: []string{build.Builder}}}
	submit(build.ObjectType, "asset", "create", map[string]any{"name": "asset", "title": "Assets", "fields": fields, "states": []build.State{{Name: "open", Title: "Open"}}, "actions": []build.Action{{Name: "adjust", Title: "Adjust", From: []string{"open"}, Inputs: []build.Input{{Name: "next", Title: "Next", Type: "integer", Required: true}}, Sets: []build.Set{{Field: "qty", From: "next"}}}}})
	submit(build.ObjectType, "asset", "publish", map[string]any{})
	object := platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: "build.asset"}
	sections := []build.Section{{ID: "actions", Widget: "action-table", ConfigVersion: 1, CollectionVariable: "rows", Actions: []string{"build.asset.adjust"}, ActionTable: &platform.PageActionTable{Parameters: []platform.PageActionParameter{{Parameter: "next", Field: "qty"}}}}, {ID: "tiles", Widget: "record-list", ConfigVersion: 1, CollectionVariable: "tiles", CardLabel: "name", RecordList: &platform.PageRecordList{Layout: "tiles"}}, {ID: "note", Widget: "notepad", ConfigVersion: 1, NotepadVariable: "note"}}
	doc := platform.PageDocument{FormatVersion: 2, UIProfile: platform.PageUIProfile(), Root: "root", Nodes: map[string]platform.PageLayoutNode{"root": {Kind: "rows"}}, Queries: map[string]platform.PageQuery{"rows": {Object: object, Limit: 50, Sort: []string{"id"}}, "tiles": {Object: object, Limit: 8, Sort: []string{"id"}}}, Variables: map[string]platform.PageVariable{"rows": {Scope: "page", Type: "object-set", Mode: "resource", Source: &platform.PageResourceSource{Kind: "plan", Query: "rows"}}, "tiles": {Scope: "page", Type: "object-set", Mode: "resource", Source: &platform.PageResourceSource{Kind: "plan", Query: "tiles"}}, "note": {Scope: "page", Type: "string", Mode: "state", Initial: platform.Raw("Original note")}}}
	for _, s := range sections {
		doc.Nodes[s.ID] = platform.PageLayoutNode{Kind: "widget", Section: s.ID}
		root := doc.Nodes["root"]
		root.Children = append(root.Children, s.ID)
		doc.Nodes["root"] = root
	}
	submit(build.PageType, "page", "create", map[string]any{"name": "recordwork", "title": "Original record work", "object": object.Name, "sections": sections, "document": doc})
	preview, err := tn.PreviewRelease(builder, platform.AssetPage, "page")
	if err != nil || preview.Diagnostic != "" {
		t.Fatal(preview, err)
	}
	saved, err := tn.SaveReleaseCandidate(builder, platform.AssetPage, "page", preview.CandidateID, "save", at)
	if err != nil {
		t.Fatal(err)
	}
	sections[0].ActionTable.Parameters[0].Field = "name"
	v := doc.Variables["note"]
	v.Initial = platform.Raw("later")
	doc.Variables["note"] = v
	submit(build.PageType, "page", "edit", map[string]any{"sections": sections, "document": doc})
	if _, err := tn.ActivateRelease(builder, saved, "activate", at); err != nil {
		t.Fatal(err)
	}
	check := func(current *Tenant) {
		t.Helper()
		for _, member := range []platform.Member{builder, reader} {
			found := false
			for _, d := range current.Definitions(member) {
				if d.Page == nil || d.Ref.Name != "recordwork" {
					continue
				}
				found = true
				seen := map[string]platform.Section{}
				for _, s := range d.Page.Sections {
					seen[s.ID] = s
				}
				if seen["tiles"].RecordList == nil || seen["tiles"].RecordList.Layout != "tiles" || string(d.Page.Document.Variables["note"].Initial) != `"Original note"` {
					t.Fatal("frozen work adopted draft")
				}
				if member.ID == reader.ID {
					if _, ok := seen["actions"]; ok {
						t.Fatal("private parameter field exposed")
					}
					if _, ok := d.Page.Document.Nodes["actions"]; ok {
						t.Fatal("private action node survived")
					}
				} else if seen["actions"].ActionTable == nil || seen["actions"].ActionTable.Parameters[0].Field != "qty" {
					t.Fatal("frozen action map changed")
				}
				if err := d.Page.Document.Check(d.Page.Sections); err != nil {
					t.Fatal(err)
				}
			}
			if !found {
				t.Fatal("record work page disappeared")
			}
		}
	}
	check(tn)
	CheckReplay(t, tn, entries, compose)
	raw, _, err := tn.Snapshot(func() int64 { return int64(len(entries)) })
	if err != nil {
		t.Fatal(err)
	}
	restored := compose()
	if err := restored.Restore(raw); err != nil {
		t.Fatal(err)
	}
	check(restored)
}
