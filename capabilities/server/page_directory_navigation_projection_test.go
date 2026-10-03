package platformserver

import (
	"fmt"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/platform"
)

func TestDirectoryNavigationMemberProjectionKeepsPublicOriginalItems(t *testing.T) {
	const tenant = "directory-navigation"
	owner := build.New(tenant)
	tn, err := NewTenant(tenant, NewConsole(tenant, Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder}}}, Seat{Subjects: []string{"reader"}, Member: platform.Member{ID: "reader", Roles: map[string]string{build.ID: build.User}}}), owner)
	if err != nil {
		t.Fatal(err)
	}
	builder, _ := tn.Member("builder")
	reader, _ := tn.Member("reader")
	at := time.Date(2026, 10, 4, 1, 0, 0, 0, time.UTC)
	key := 0
	submit := func(id, verb string, payload any) {
		t.Helper()
		key++
		_, issue := tn.Submit(builder, &pb.Submission{TenantId: tenant, PrincipalId: builder.ID, Authority: build.ID, IdempotencyKey: fmt.Sprint(key), Target: &pb.EntityRef{Type: build.ObjectType, Id: id}, Schema: &pb.SchemaRef{Name: build.ObjectType + "." + verb, Version: 1}, Payload: platform.Raw(payload)}, at)
		if issue != nil {
			t.Fatal(issue.Message)
		}
	}
	for _, name := range []string{"public", "private"} {
		payload := map[string]any{"name": name, "title": name, "fields": []build.Field{{Name: "name", Title: "Name", Type: "text"}}}
		if name == "private" {
			payload["access"] = []build.Access{{Role: build.User, Read: "none"}}
		}
		submit(name, "create", payload)
		submit(name, "publish", map[string]any{})
	}
	object := func(name string) platform.AssetRef {
		return platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: "build." + name}
	}
	pageRef := func(name string) platform.AssetRef {
		return platform.AssetRef{App: build.ID, Kind: platform.AssetPage, Name: name}
	}
	for _, name := range []string{"public", "private"} {
		target := platform.Page{Name: name + "target", Title: name + " target", Object: object(name), Layout: "list-detail", ListFields: []string{"name"}, DetailFields: []string{"name"}}
		if err := tn.InstallPage(owner, target); err != nil {
			t.Fatal(err)
		}
	}
	directory := platform.PageAssetDirectory{Items: []platform.PageAssetDirectoryItem{{ID: "public", Label: "Original public entry", Asset: platform.AssetBinding{Ref: pageRef("publictarget"), SourceVersion: "1"}}, {ID: "private", Label: "Original private entry", Asset: platform.AssetBinding{Ref: pageRef("privatetarget"), SourceVersion: "1"}}}}
	source := platform.Page{Name: "directory", Title: "Directory", Object: object("public"), Layout: "composed", Sections: []platform.Section{{ID: "heading", Widget: "text", ConfigVersion: 1, Text: "Directory context"}, {ID: "directory", Widget: "asset-directory", ConfigVersion: 1, AssetDirectory: &directory}, {ID: "privateButton", Widget: "button", ConfigVersion: 1}}, Document: &platform.PageDocument{FormatVersion: 2, UIProfile: platform.PageUIProfile(), Root: "root", Nodes: map[string]platform.PageLayoutNode{"root": {Kind: "rows", Children: []string{"heading", "directory", "privateButton"}}, "heading": {Kind: "widget", Section: "heading"}, "directory": {Kind: "widget", Section: "directory"}, "privateButton": {Kind: "widget", Section: "privateButton"}}, Events: []platform.PageEventBinding{{Source: "directory", Control: "public", Event: "click", Navigate: &platform.PageNavigation{Page: pageRef("publictarget")}}, {Source: "directory", Control: "private", Event: "click", Navigate: &platform.PageNavigation{Page: pageRef("privatetarget")}}, {Source: "privateButton", Event: "click", Navigate: &platform.PageNavigation{Page: pageRef("privatetarget")}}}}}
	if err := tn.InstallPage(owner, source); err != nil {
		t.Fatal(err)
	}
	for _, member := range []platform.Member{builder, reader} {
		found := false
		for _, definition := range tn.Definitions(member) {
			if definition.Page == nil || definition.Ref != pageRef("directory") {
				continue
			}
			found = true
			p := definition.Page
			sections := map[string]platform.Section{}
			for _, s := range p.Sections {
				sections[s.ID] = s
			}
			shown := sections["directory"].AssetDirectory
			if shown == nil || len(shown.Items) == 0 || shown.Items[0].Label != "Original public entry" || shown.Items[0].Asset.Ref != pageRef("publictarget") {
				t.Fatal("hidden navigation target removed public directory items")
			}
			if member.ID == builder.ID {
				if len(shown.Items) != 2 || len(p.Document.Events) != 3 {
					t.Fatal("projection changed original builder controls")
				}
			} else {
				if len(shown.Items) != 1 || len(p.Document.Events) != 1 {
					t.Fatal("hidden directory item or handler survived", shown, p.Document.Events)
				}
				event := p.Document.Events[0]
				if event.Source != "directory" || event.Control != "public" || event.Navigate == nil || event.Navigate.Page != pageRef("publictarget") || len(event.Navigate.Inputs) > 0 || len(event.Navigate.Results) > 0 || event.Target != "" {
					t.Fatal("public directory retained hidden or independent navigation bindings")
				}
				if _, ok := sections["privateButton"]; ok {
					t.Fatal("non-directory navigation stopped failing closed")
				}
				if _, ok := p.Document.Nodes["privateButton"]; ok {
					t.Fatal("hidden ordinary navigation node survived")
				}
				if len(p.Document.Variables) > 0 {
					t.Fatal("directory navigation introduced orphan variable/input bindings")
				}
				if err := p.Document.Check(p.Sections); err != nil {
					t.Fatal("cropped member page is not a valid native document", err)
				}
			}
		}
		if !found {
			t.Fatal("member directory page disappeared")
		}
	}
	if len(source.Sections[1].AssetDirectory.Items) != 2 || len(source.Document.Events) != 3 {
		t.Fatal("member projection mutated original directory")
	}
}
