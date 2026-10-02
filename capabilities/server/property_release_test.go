package platformserver

import (
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/platform"
	"testing"
	"time"
)

func TestRecordPropertyFreezeProjectionAndRecovery(t *testing.T) {
	const tenant = "record-property-state"
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

	submit(build.ObjectType, "object", "create", map[string]any{"name": "note", "title": "Note", "access": []build.Access{{Role: build.User, Read: "all"}}, "fields": []build.Field{{Name: "active", Title: "Active", Type: "boolean"}, {Name: "secret", Title: "Secret", Type: "boolean", Read: []string{build.Builder}}}})
	submit(build.ObjectType, "object", "publish", map[string]any{})
	object := platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: "build.note"}
	doc := &platform.PageDocument{FormatVersion: 2, UIProfile: platform.PageUIProfile(), Root: "root", Nodes: map[string]platform.PageLayoutNode{"root": {Kind: "rows", Children: []string{"table", "notice"}}, "table": {Kind: "widget", Section: "table"}, "notice": {Kind: "widget", Section: "notice", VisibleWhen: "secret"}}, Variables: map[string]platform.PageVariable{"selected": {Scope: "page", Type: "record", Mode: "resource", Source: &platform.PageResourceSource{Kind: "record", Section: "table"}}, "secret": {Scope: "page", Type: "boolean", Mode: "property", Source: &platform.PageResourceSource{Kind: "property", Variable: "selected", Object: &object, Field: "secret"}}}}
	submit(build.PageType, "page", "create", map[string]any{"name": "notes", "title": "Notes", "object": object.Name, "sections": []build.Section{{ID: "table", Widget: "table", ConfigVersion: 1, Fields: []string{"active"}}, {ID: "notice", Widget: "text", ConfigVersion: 1, Text: "Secret value"}}, "document": doc})
	submit(build.PageType, "page", "publish", map[string]any{})
	vars := map[string]platform.PageVariable{"selected": {Scope: "application", Type: "record", Mode: "resource", Source: &platform.PageResourceSource{Kind: "record", Object: &object}}, "active": {Scope: "application", Type: "boolean", Mode: "property", Source: &platform.PageResourceSource{Kind: "property", Variable: "selected", Object: &object, Field: "active"}}, "private": {Scope: "application", Type: "boolean", Mode: "property", Source: &platform.PageResourceSource{Kind: "property", Variable: "selected", Object: &object, Field: "secret"}}}
	submit(build.AppType, "app", "create", map[string]any{"name": "desk", "title": "Desk", "pages": []string{"notes"}, "uiProfile": platform.PageUIProfile(), "variables": vars})
	preview, err := tn.PreviewRelease(member, platform.AssetApp, "app")
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("preview %+v %v", preview, err)
	}
	if _, err = tn.SaveReleaseCandidate(member, platform.AssetApp, "app", preview.CandidateID, "freeze", at); err != nil {
		t.Fatal(err)
	}
	submit(build.AppType, "app", "edit", map[string]any{"variables": map[string]platform.PageVariable{}})
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
		reader, _ := current.Member("reader")
		for _, d := range current.Definitions(reader) {
			if d.Page != nil && d.Ref.Name == "notes" {
				if len(d.Page.Sections) != 1 || d.Page.Document.Variables["secret"].Mode != "" {
					t.Fatal("private page property leaked")
				}
			}
			if d.Application != nil && d.Ref.Name == "desk" {
				if d.Application.Variables["private"].Mode != "" || d.Application.Variables["active"].Source.Field != "active" {
					t.Fatal("property schema drifted or private app property leaked")
				}
			}
		}
	}
}
