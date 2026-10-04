package platformserver

import (
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/platform"
	"testing"
	"time"
)

func TestThreeEmbeddedModesFreezeOldChildContentsAndRecover(t *testing.T) {
	compose := func() *Tenant {
		tn, err := NewTenant("embeds", NewConsole("embeds", Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder}}}, Seat{Subjects: []string{"reader"}, Member: platform.Member{ID: "reader", Roles: map[string]string{build.ID: build.User}}}), build.New("embeds"))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := compose()
	builder, _ := tn.Member("builder")
	reader, _ := tn.Member("reader")
	at := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	key := 0
	var entries []Entry
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	submit := func(typ, id, verb string, payload any) {
		t.Helper()
		key++
		_, issue := tn.Submit(builder, &pb.Submission{TenantId: tn.ID, PrincipalId: builder.ID, Authority: build.ID, IdempotencyKey: fmt.Sprint(key), Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: typ + "." + verb, Version: 1}, Payload: platform.Raw(payload)}, at)
		if issue != nil {
			t.Fatal(typ, id, verb, issue.Message)
		}
	}
	submit(build.ObjectType, "note", "create", map[string]any{"name": "note", "title": "Notes", "fields": []build.Field{{Name: "name", Title: "Name", Type: "text"}}})
	submit(build.ObjectType, "note", "publish", map[string]any{})
	doc := func(ids ...string) *platform.PageDocument {
		d := &platform.PageDocument{FormatVersion: 2, UIProfile: platform.PageUIProfile(), Root: "root", Nodes: map[string]platform.PageLayoutNode{"root": {Kind: "rows", Children: ids}}}
		for _, id := range ids {
			d.Nodes[id] = platform.PageLayoutNode{Kind: "widget", Section: id}
		}
		return d
	}
	childSections := []build.Section{{ID: "text", Widget: "text", ConfigVersion: 1, Text: "ORIGINAL CHILD"}}
	submit(build.PageType, "child", "create", map[string]any{"name": "child", "title": "Child", "object": "build.note", "sections": childSections, "document": doc("text")})
	submit(build.PageType, "child", "publish", map[string]any{})
	var original platform.Definition
	for _, d := range tn.Definitions(builder) {
		if d.Page != nil && d.Ref.Name == "child" {
			original = d
		}
	}
	if original.ContentVersion == "" {
		t.Fatal("child missing")
	}
	childSections[0].Text = "LATER CHILD"
	submit(build.PageType, "child", "edit", map[string]any{"sections": childSections})
	submit(build.PageType, "child", "publish", map[string]any{})
	sections := []build.Section{}
	for _, kind := range []string{"module", "custom", "dashboard"} {
		sections = append(sections, build.Section{ID: kind, Widget: "embedded-page", ConfigVersion: 1, Embedding: &platform.PageEmbedding{Kind: kind, ReadOnly: kind != "module", Page: platform.AssetBinding{Ref: original.Ref, SourceVersion: original.Version}, ContentVersion: original.ContentVersion}})
	}
	sections = append(sections, build.Section{ID: "external", Widget: "external-frame", ConfigVersion: 1, ExternalFrame: &platform.PageExternalFrame{URL: "https://docs.example.com/report", Origin: "https://docs.example.com"}})
	submit(build.PageType, "parent", "create", map[string]any{"name": "parent", "title": "Parent", "object": "build.note", "sections": sections, "document": doc("module", "custom", "dashboard", "external")})
	preview, err := tn.PreviewRelease(builder, platform.AssetPage, "parent")
	if err != nil || preview.Diagnostic != "" {
		t.Fatal(preview, err)
	}
	saved, err := tn.SaveReleaseCandidate(builder, platform.AssetPage, "parent", preview.CandidateID, "save", at)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tn.ActivateRelease(builder, saved, "activate", at); err != nil {
		t.Fatal(err)
	}
	check := func(current *Tenant) {
		t.Helper()
		found := false
		for _, d := range current.Definitions(reader) {
			if d.Page == nil || d.Ref.Name != "parent" {
				continue
			}
			found = true
			if len(d.Page.Sections) != 4 {
				t.Fatal("embedded modes were removed")
			}
			for _, s := range d.Page.Sections {
				if s.ID == "external" {
					if s.ExternalFrame == nil || s.ExternalFrame.Origin != "https://docs.example.com" || s.ExternalFrame.URL != "https://docs.example.com/report" {
						t.Fatal("external configuration changed")
					}
					continue
				}
				if s.Embedding == nil || s.Embedding.ContentVersion != original.ContentVersion {
					t.Fatal("parent adopted latest child")
				}
				if s.Embedding.ReadOnly != (s.ID != "module") {
					t.Fatal("read-only presentation changed")
				}
				child, err := current.PageContentDefinition(reader, s.Embedding.Page.Ref, s.Embedding.ContentVersion)
				if err != nil || child.Page.Sections[0].Text != "ORIGINAL CHILD" {
					t.Fatal(child, err)
				}
			}
		}
		if !found {
			t.Fatal("parent missing")
		}
	}
	check(tn)
	// A dependent can be encountered after a page through another root. The
	// retained descriptor must produce the same closure in either order.
	available, err := tn.releaseAssetsLocked(nil, false)
	if err != nil {
		t.Fatal(err)
	}
	latest, err := platform.PageReleaseAsset(build.ID, original.Version, platform.Page{Name: "child", Object: original.Page.Object, Layout: "composed", Sections: []platform.Section{{ID: "text", Widget: "text", ConfigVersion: 1, Text: "LATER CHILD"}}, Document: doc("text")})
	if err != nil {
		t.Fatal(err)
	}
	for i, asset := range available {
		if asset.Ref == original.Ref {
			available[i] = latest
		}
	}
	parent := platform.AssetRef{App: build.ID, Kind: platform.AssetPage, Name: "parent"}
	for _, roots := range [][]platform.AssetRef{{original.Ref, parent}, {parent, original.Ref}} {
		candidate, err := tn.candidateWithBindings(roots, available, []platform.AssetRef{parent})
		if err != nil {
			t.Fatal("root-order dependent closure", err)
		}
		for _, asset := range candidate.Assets {
			if asset.Ref == original.Ref && string(asset.Body) == string(latest.Body) {
				t.Fatal("candidate adopted latest child")
			}
		}
	}
	if _, err := tn.candidateWithBindings([]platform.AssetRef{parent, original.Ref}, available, []platform.AssetRef{parent, original.Ref}); err == nil {
		t.Fatal("a content pin replaced an explicit release root")
	}
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
