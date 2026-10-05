package platformserver

import (
	"fmt"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/platform"
)

func TestFrozenWidgetSlotsSurviveActivationAndReplay(t *testing.T) {
	compose := func() *Tenant {
		tn, err := NewTenant("widget-slots-release", NewConsole("widget-slots-release", Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder}}}, Seat{Subjects: []string{"writer"}, Member: platform.Member{ID: "writer", Roles: map[string]string{build.ID: build.User}}}), build.New("widget-slots-release"))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := compose()
	builder, _ := tn.Member("builder")
	writer, _ := tn.Member("writer")
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
	submit(build.ObjectType, "O", "create", map[string]any{"name": "note", "title": "Notes", "access": []build.Access{{Role: build.User, Read: "all", Edit: true}}, "fields": []build.Field{{Name: "name", Title: "Name", Type: "text"}}})
	submit(build.ObjectType, "O", "publish", map[string]any{})
	sections := []build.Section{{ID: "table", Widget: "table", ConfigVersion: 1, Fields: []string{"name"}}, {ID: "detail", Widget: "detail", ConfigVersion: 1, RecordVariable: "record", Fields: []string{"name"}}, {ID: "card", Widget: "record-card", ConfigVersion: 1, RecordVariable: "record", RecordCard: &platform.PageRecordCard{LabelField: "name", Tone: "info"}, Fields: []string{"name"}}, {ID: "control", Widget: "text", ConfigVersion: 1, Text: "Original slot"}}
	doc := platform.PageDocument{FormatVersion: 2, UIProfile: platform.PageUIProfile(), Root: "root", Nodes: map[string]platform.PageLayoutNode{
		"root": {Kind: "rows", Children: []string{"table", "detail", "card"}}, "table": {Kind: "widget", Section: "table", Children: []string{"toolbar", "footer"}}, "toolbar": {Kind: "toolbar", Slot: "toolbar", Children: []string{"control"}}, "footer": {Kind: "flow", Slot: "footer"}, "control": {Kind: "widget", Section: "control"}, "detail": {Kind: "widget", Section: "detail", Children: []string{"detailactions"}}, "detailactions": {Kind: "flow", Slot: "actions"}, "card": {Kind: "widget", Section: "card", Children: []string{"cardactions"}}, "cardactions": {Kind: "flow", Slot: "actions"},
	}, Variables: map[string]platform.PageVariable{"record": {Scope: "page", Type: "record", Mode: "resource", Source: &platform.PageResourceSource{Kind: "record", Section: "table"}}}}
	submit(build.PageType, "P", "create", map[string]any{"name": "notes", "title": "Notes", "object": "build.note", "sections": sections, "document": doc})
	preview, err := tn.PreviewRelease(builder, platform.AssetPage, "P")
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("preview %+v %v", preview, err)
	}
	if _, err = tn.SaveReleaseCandidate(builder, platform.AssetPage, "P", preview.CandidateID, "save", at); err != nil {
		t.Fatal(err)
	}
	sections[3].Text = "Later slot draft"
	submit(build.PageType, "P", "edit", map[string]any{"sections": sections})
	if _, err = tn.ActivateRelease(builder, preview.CandidateID, "activate", at); err != nil {
		t.Fatal(err)
	}
	check := func(current *Tenant) {
		t.Helper()
		for _, member := range []platform.Member{builder, writer} {
			found := false
			for _, d := range current.Definitions(member) {
				if d.Page != nil && d.Ref.Name == "notes" {
					found = true
					for _, item := range []struct{ id, slot string }{{"toolbar", "toolbar"}, {"footer", "footer"}, {"detailactions", "actions"}, {"cardactions", "actions"}} {
						if d.Page.Document.Nodes[item.id].Slot != item.slot {
							t.Fatalf("frozen slot %s was lost", item.id)
						}
					}
					if d.Page.Sections[3].Text != "Original slot" {
						t.Fatal("later draft replaced the frozen slot child")
					}

				}
			}
			if !found {
				t.Fatal("frozen page was not offered")
			}
		}
	}
	check(tn)
	restored := compose()
	if err = restored.Replay(entries); err != nil {
		t.Fatal(err)
	}
	check(restored)
}
