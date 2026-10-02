package platformserver

import (
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/platform"
	"testing"
	"time"
)

func TestFrozenTableEditFieldsMemberProjectionAndRecovery(t *testing.T) {
	compose := func() *Tenant {
		tn, err := NewTenant("table-edit", NewConsole("table-edit", Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder}}}, Seat{Subjects: []string{"writer"}, Member: platform.Member{ID: "writer", Roles: map[string]string{build.ID: build.User}}}, Seat{Subjects: []string{"reader"}, Member: platform.Member{ID: "reader", Roles: map[string]string{build.ID: "viewer"}}}), build.New("table-edit"))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := compose()
	builder, _ := tn.Member("builder")
	writer, _ := tn.Member("writer")
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
	submit(build.ObjectType, "O", "create", map[string]any{"name": "note", "title": "Notes", "access": []build.Access{{Role: build.User, Read: "all", Edit: true}, {Role: "viewer", Read: "all"}}, "fields": []build.Field{{Name: "name", Title: "Name", Type: "text"}, {Name: "protected", Title: "Protected", Type: "text", Write: []string{build.Builder}}}})
	submit(build.ObjectType, "O", "publish", map[string]any{})
	sections := []build.Section{{ID: "table", Widget: "table", ConfigVersion: 1, Fields: []string{"name", "protected"}, InlineEdit: &build.InlineEdit{Action: "build.note.edit", Fields: []string{"name", "protected"}}}}
	doc := platform.PageDocument{FormatVersion: 2, UIProfile: platform.PageUIProfile(), Root: "root", Nodes: map[string]platform.PageLayoutNode{"root": {Kind: "rows", Children: []string{"table"}}, "table": {Kind: "widget", Section: "table"}}}
	submit(build.PageType, "P", "create", map[string]any{"name": "notes", "title": "Notes", "object": "build.note", "sections": sections, "document": doc})
	preview, err := tn.PreviewRelease(builder, platform.AssetPage, "P")
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("preview %+v %v", preview, err)
	}
	if _, err = tn.SaveReleaseCandidate(builder, platform.AssetPage, "P", preview.CandidateID, "save", at); err != nil {
		t.Fatal(err)
	}
	sections[0].InlineEdit = nil
	submit(build.PageType, "P", "edit", map[string]any{"sections": sections})
	if _, err = tn.ActivateRelease(builder, preview.CandidateID, "activate", at); err != nil {
		t.Fatal(err)
	}
	check := func(current *Tenant) {
		t.Helper()
		for _, member := range []platform.Member{builder, writer, reader} {
			seen := false
			for _, d := range current.Definitions(member) {
				if d.Page != nil && d.Ref.Name == "notes" {
					seen = true
					edit := d.Page.Sections[0].InlineEdit
					if member.ID == reader.ID {
						if edit != nil {
							t.Fatal("reader retained edit port")
						}
					} else {
						if edit == nil || edit.Action.Name != "build.note.edit" {
							t.Fatal("frozen edit port missing")
						}
						want := 2
						if member.ID == writer.ID {
							want = 1
						}
						if len(edit.Fields) != want {
							t.Fatal("member edit fields were not projected")
						}
					}
				}
			}
			if !seen {
				t.Fatal("readable table disappeared")
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
