package platformserver

import (
	"fmt"
	"slices"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/apps/work"
	"platformserver/platform"
)

func TestFrozenWorkViewsAndHistoryMemberProjection(t *testing.T) {
	compose := func() *Tenant {
		tn, err := NewTenant("work-views", NewConsole("work-views", Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder}}}, Seat{Subjects: []string{"reader"}, Member: platform.Member{ID: "reader", Roles: map[string]string{build.ID: build.User}}}), build.New("work-views"), work.New("work-views"))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := compose()
	builder, _ := tn.Member("builder")
	reader, _ := tn.Member("reader")
	at := time.Date(2026, 10, 3, 18, 0, 0, 0, time.UTC)
	var entries []Entry
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
	submit(build.ObjectType, "O", "create", map[string]any{"name": "note", "title": "Notes", "fields": []build.Field{{Name: "name", Title: "Name", Type: "text"}, {Name: "secret", Title: "Secret", Type: "text", Read: []string{build.Builder}}}})
	submit(build.ObjectType, "O", "publish", map[string]any{})
	submit(build.ObjectType, "Private", "create", map[string]any{"name": "private", "title": "Private notes", "fields": []build.Field{{Name: "name", Title: "Name", Type: "text"}}, "access": []build.Access{{Role: build.User, Read: "none"}}})
	submit(build.ObjectType, "Private", "publish", map[string]any{})
	variables := map[string]platform.PageVariable{"active": {Scope: "page", Type: "record", Mode: "resource", Source: &platform.PageResourceSource{Kind: "record", Section: "table"}}, "privateActive": {Scope: "page", Type: "record", Mode: "resource", Source: &platform.PageResourceSource{Kind: "record", Section: "privateTable"}}, "enabled": {Scope: "page", Type: "boolean", Mode: "state", Initial: platform.Raw(false)}}
	sections := []build.Section{{ID: "table", Widget: "table", ConfigVersion: 1, Fields: []string{"name", "secret"}}, {ID: "history", Widget: "timeline", ConfigVersion: 1, RecordVariable: "active", HistoryLimit: 18}, {ID: "approvals", Widget: "approval-inbox", ConfigVersion: 1, Title: "Original approval inbox"}, {ID: "notifications", Widget: "notification-feed", ConfigVersion: 1, Title: "Original notification feed"}, {ID: "privateTable", Widget: "table", ConfigVersion: 1, Object: "build.private", Fields: []string{"name"}}, {ID: "privateHistory", Widget: "timeline", ConfigVersion: 1, Object: "build.private", RecordVariable: "privateActive", HistoryLimit: 18}}
	doc := platform.PageDocument{FormatVersion: 2, UIProfile: platform.PageUIProfile(), Root: "root", Variables: variables, Nodes: map[string]platform.PageLayoutNode{"root": {Kind: "rows"}}}
	for _, s := range sections {
		doc.Nodes[s.ID] = platform.PageLayoutNode{Kind: "widget", Section: s.ID}
		n := doc.Nodes[doc.Root]
		n.Children = append(n.Children, s.ID)
		doc.Nodes[doc.Root] = n
	}
	n := doc.Nodes["approvals"]
	n.EnabledWhen = "enabled"
	doc.Nodes["approvals"] = n
	submit(build.PageType, "P", "create", map[string]any{"name": "workviews", "title": "Work views", "object": "build.note", "document": doc, "sections": sections})
	preview, err := tn.PreviewRelease(builder, platform.AssetPage, "P")
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("preview %+v %v", preview, err)
	}
	saved, err := tn.SaveReleaseCandidate(builder, platform.AssetPage, "P", preview.CandidateID, "save", at)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := platform.ReadCandidate(saved, tn.releaseCandidates[saved])
	if err != nil {
		t.Fatal(err)
	}
	wanted := []platform.AssetRef{{App: "work", Kind: platform.AssetObject, Name: work.TaskType}, {App: "work", Kind: platform.AssetObject, Name: work.ApprovalType}, {App: "work", Kind: platform.AssetAction, Name: work.ApprovalType + ".approve"}, {App: "work", Kind: platform.AssetAction, Name: work.ApprovalType + ".reject"}, {App: PlatformApp, Kind: platform.AssetAction, Name: SchemaNotificationRead}}
	for _, ref := range wanted {
		if !slices.ContainsFunc(candidate.Assets, func(a platform.ReleaseAsset) bool { return a.Ref == ref }) {
			t.Fatal("fixed service omitted", ref)
		}
	}
	if slices.ContainsFunc(candidate.Assets, func(a platform.ReleaseAsset) bool {
		return a.Ref.Kind == platform.AssetObject && a.Ref.Name == NotificationType
	}) {
		t.Fatal("notification's ledger class became an entity asset")
	}
	sections[1].HistoryLimit = 99
	sections[2].Title = "Later approval draft"
	n = doc.Nodes["approvals"]
	n.EnabledWhen = ""
	doc.Nodes["approvals"] = n
	submit(build.PageType, "P", "edit", map[string]any{"document": doc, "sections": sections})
	if _, err := tn.ActivateRelease(builder, saved, "activate", at); err != nil {
		t.Fatal(err)
	}
	submit("build.note", "N1", "create", map[string]any{"name": "Visible note", "secret": "Private change"})
	check := func(current *Tenant) {
		t.Helper()
		for _, member := range []platform.Member{builder, reader} {
			found := false
			for _, definition := range current.Definitions(member) {
				if definition.Page == nil || definition.Ref.Name != "workviews" {
					continue
				}
				found = true
				p := definition.Page
				seen := map[string]platform.Section{}
				for _, s := range p.Sections {
					seen[s.ID] = s
				}
				if seen["history"].HistoryLimit != 18 || seen["history"].RecordVariable != "active" || seen["approvals"].Widget != "approval-inbox" || seen["approvals"].Title != "Original approval inbox" || seen["notifications"].Widget != "notification-feed" || p.Document.Nodes["approvals"].EnabledWhen != "enabled" {
					t.Fatal("later draft changed original work views/history")
				}
				if member.ID == reader.ID {
					if _, ok := seen["privateHistory"]; ok {
						t.Fatal("denied record retained history consumer")
					}
					if _, ok := p.Document.Variables["privateActive"]; ok {
						t.Fatal("denied producer retained resource")
					}
				}
			}
			if !found {
				t.Fatal("ordinary member work page disappeared")
			}
			view, err := current.RecordOf(member, "build.note", "N1", at)
			if err != nil || len(view.History) != 1 {
				t.Fatalf("original history %+v %v", view.History, err)
			}
			secret := slices.ContainsFunc(view.History[0].Fields, func(f FieldChange) bool { return f.Field == "secret" })
			if secret != (member.ID == builder.ID) {
				t.Fatal("history field projection changed")
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
