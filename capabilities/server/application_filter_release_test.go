package platformserver

import (
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/platform"
	"testing"
	"time"
)

func TestApplicationFilterFreezeProjectionAndRecovery(t *testing.T) {
	const tenant = "application-filter-state"
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
	doc := &platform.PageDocument{FormatVersion: 2, UIProfile: platform.PageUIProfile(), Root: "root", Nodes: map[string]platform.PageLayoutNode{"root": {Kind: "rows", Children: []string{"filter", "table"}}, "filter": {Kind: "widget", Section: "filter"}, "table": {Kind: "widget", Section: "table"}}, Variables: map[string]platform.PageVariable{"filter": {Scope: "application", Type: "filter", Mode: "shared", Writable: true, Source: &platform.PageResourceSource{Kind: "application", Variable: "filter", Object: &object}}}}
	for _, id := range []string{"first", "second"} {
		submit(build.PageType, id, "create", map[string]any{"name": id, "title": id, "object": object.Name, "sections": []build.Section{{ID: "filter", Widget: "filter", ConfigVersion: 1, Fields: []string{"active", "secret"}, FilterVariable: "filter"}, {ID: "table", Widget: "table", ConfigVersion: 1, Fields: []string{"active"}, FilterVariable: "filter"}}, "document": doc})
		submit(build.PageType, id, "publish", map[string]any{})
	}
	variables := map[string]platform.PageVariable{"filter": {Scope: "application", Type: "filter", Mode: "resource", Source: &platform.PageResourceSource{Kind: "filter", Object: &object, Fields: []string{"active", "secret"}}}, "private": {Scope: "application", Type: "filter", Mode: "resource", Source: &platform.PageResourceSource{Kind: "filter", Object: &object, Fields: []string{"secret"}}}, "hasPrivate": {Scope: "application", Type: "boolean", Mode: "derived", Expression: &platform.PageExpression{Op: "present", Args: []platform.PageValue{{Variable: "private"}}}}}
	submit(build.AppType, "app", "create", map[string]any{"name": "notes", "title": "Notes", "pages": []string{"first", "second"}, "uiProfile": platform.PageUIProfile(), "variables": variables})
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
			if d.Application != nil && d.Ref.Name == "notes" {
				v := d.Application.Variables["filter"]
				if v.Source == nil || len(v.Source.Fields) != 1 || v.Source.Fields[0] != "active" || len(d.Application.Variables) != 1 {
					t.Fatal("private filter metadata exposed")
				}
			}
			if d.Page != nil && (d.Ref.Name == "first" || d.Ref.Name == "second") {
				if d.Page.Sections[0].FilterVariable != "filter" || len(d.Page.Sections[0].Fields) != 1 {
					t.Fatal("filter port or member fields drifted")
				}
			}
		}
		for _, d := range current.definitions {
			if d.Application != nil && d.Ref.Name == "notes" {
				v := d.Application.Variables["filter"]
				if v.Source == nil || len(v.Source.Fields) != 2 {
					t.Fatal("member projection changed frozen filter")
				}
			}
		}
	}
}
