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

func TestApplicationSharedDeclarationsFreezeActivateAndReplay(t *testing.T) {
	const tenant = "application-state"
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
	declarations := func(initial string) map[string]platform.PageVariable {
		return map[string]platform.PageVariable{"draft": {Scope: "application", Type: "string", Mode: "state", Initial: platform.Raw(initial)}}
	}
	submit(build.ObjectType, "object", "create", map[string]any{"name": "note", "title": "Note", "fields": []build.Field{{Name: "note", Title: "Note", Type: "text"}}})
	submit(build.ObjectType, "object", "publish", map[string]any{})
	document := &platform.PageDocument{FormatVersion: 2, UIProfile: platform.PageUIProfile(), Root: "root", Nodes: map[string]platform.PageLayoutNode{"root": {Kind: "rows", Children: []string{"input"}}, "input": {Kind: "widget", Section: "input", ValueVariable: "draft"}}, Variables: map[string]platform.PageVariable{"draft": {Scope: "application", Type: "string", Mode: "shared", Writable: true, Source: &platform.PageResourceSource{Kind: "application", Variable: "draft"}}}}
	submit(build.PageType, "page", "create", map[string]any{"name": "work", "title": "Work", "object": "build.note", "sections": []build.Section{{ID: "input", Widget: "input", ConfigVersion: 1}}, "document": document})
	submit(build.PageType, "page", "publish", map[string]any{})
	submit(build.AppType, "app", "create", map[string]any{"name": "desk", "title": "Desk", "pages": []string{"work"}, "uiProfile": platform.PageUIProfile(), "variables": declarations("frozen")})
	submit(build.AppType, "app", "publish", map[string]any{})
	preview, err := tn.PreviewRelease(member, platform.AssetApp, "app")
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("preview %+v %v", preview, err)
	}
	if _, err = tn.SaveReleaseCandidate(member, platform.AssetApp, "app", preview.CandidateID, "save", at); err != nil {
		t.Fatal(err)
	}
	submit(build.AppType, "app", "edit", map[string]any{"variables": declarations("later")})
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
			if d.Application != nil && d.Ref.Name == "desk" {
				found = true
				var initial string
				if json.Unmarshal(d.Application.Variables["draft"].Initial, &initial) != nil || initial != "frozen" {
					t.Fatal("frozen application declaration was lost")
				}
			}
		}
		if !found {
			t.Fatal("application missing")
		}
	}
	// A standalone replacement page cannot silently invalidate the installed app.
	changed := *document
	changed.Variables = map[string]platform.PageVariable{"draft": {Scope: "application", Type: "string", Mode: "shared", Writable: true, Source: &platform.PageResourceSource{Kind: "application", Variable: "missing"}}}
	var owner platform.App
	for _, app := range tn.apps {
		if app.Manifest().ID == build.ID {
			owner = app
			break
		}
	}
	page := platform.Page{Name: "work", Title: "Work", Object: platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: "build.note"}, Layout: "composed", Sections: []platform.Section{{ID: "input", Widget: "input", ConfigVersion: 1}}, Document: &changed}
	if err = tn.InstallPage(owner, page); err == nil {
		t.Fatal("page invalidated application binding")
	}
}
