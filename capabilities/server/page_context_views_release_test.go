package platformserver

import (
	"fmt"
	"slices"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/platform"
)

func TestFrozenContextViewsMemberProjectionReplayAndSnapshot(t *testing.T) {
	testFrozenContextViewsMemberProjectionReplayAndSnapshot(t, false)
}
func TestFrozenOverlayContextViewsMemberProjectionReplayAndSnapshot(t *testing.T) {
	testFrozenContextViewsMemberProjectionReplayAndSnapshot(t, true)
}
func testFrozenContextViewsMemberProjectionReplayAndSnapshot(t *testing.T, overlay bool) {
	compose := func() *Tenant {
		tn, err := NewTenant("context-views", NewConsole("context-views", Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder}}}, Seat{Subjects: []string{"reader"}, Member: platform.Member{ID: "reader", Roles: map[string]string{build.ID: build.User}}}), build.New("context-views"))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := compose()
	builder, _ := tn.Member("builder")
	reader, _ := tn.Member("reader")
	at := time.Date(2026, 10, 3, 20, 0, 0, 0, time.UTC)
	var entries []Entry
	key := 0
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	submit := func(typ, id, verb string, payload any) {
		t.Helper()
		key++
		_, err := tn.Submit(builder, &pb.Submission{TenantId: tn.ID, PrincipalId: builder.ID, Authority: build.ID, IdempotencyKey: fmt.Sprint(key), Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: typ + "." + verb, Version: 1}, Payload: platform.Raw(payload)}, at)
		if err != nil {
			t.Fatalf("%s %s %s: %s", typ, id, verb, err.Message)
		}
	}
	submit(build.ObjectType, "asset", "create", map[string]any{"name": "asset", "title": "Assets", "fields": []build.Field{{Name: "name", Title: "Name", Type: "text"}, {Name: "secret", Title: "Secret", Type: "text", Read: []string{build.Builder}}}})
	submit(build.ObjectType, "asset", "publish", map[string]any{})
	submit(build.ObjectType, "private", "create", map[string]any{"name": "private", "title": "Private assets", "fields": []build.Field{{Name: "name", Title: "Name", Type: "text"}}, "access": []build.Access{{Role: build.User, Read: "none"}}})
	submit(build.ObjectType, "private", "publish", map[string]any{})
	submit(build.ObjectType, "person", "create", map[string]any{"name": "person", "title": "People", "fields": []build.Field{{Name: "name", Title: "Name", Type: "text"}, {Name: "asset", Title: "Asset", Type: "reference", Ref: "build.asset"}, {Name: "private", Title: "Private asset", Type: "reference", Ref: "build.private"}, {Name: "shift", Title: "Shift", Type: "text"}, {Name: "secret", Title: "Secret", Type: "text", Read: []string{build.Builder}}}})
	submit(build.ObjectType, "person", "publish", map[string]any{})
	for _, q := range []struct{ name, by string }{{"allpeople", ""}, {"assetpeople", "asset"}, {"privatepeople", "private"}} {
		submit(build.QueryType, q.name, "create", map[string]any{"name": q.name, "title": q.name, "description": "Original person query", "object": "build.person", "by": q.by, "sort": []string{"id"}, "limit": 6})
		submit(build.QueryType, q.name, "publish", map[string]any{})
	}
	for _, home := range []struct{ name, object string }{{"home", "build.asset"}, {"privatehome", "build.private"}} {
		submit(build.PageType, home.name, "create", map[string]any{"name": home.name, "title": home.name, "object": home.object, "list": []string{"name"}, "detail": []string{"name"}})
		submit(build.PageType, home.name, "publish", map[string]any{})
	}
	caption := ""
	height := 0.5
	sections := []build.Section{
		{ID: "table", Widget: "table", ConfigVersion: 1, Fields: []string{"name"}, CollectionVariable: "assets"},
		{ID: "crumb", Widget: "breadcrumb", ConfigVersion: 1, RecordVariable: "active", Breadcrumb: &platform.PageBreadcrumb{HomeLabel: "Original home", PageLabel: "Assets", LabelField: "name"}},
		{ID: "secretCrumb", Widget: "breadcrumb", ConfigVersion: 1, RecordVariable: "active", Breadcrumb: &platform.PageBreadcrumb{HomeLabel: "Home", PageLabel: "Assets", LabelField: "secret"}},
		{ID: "privateHomeCrumb", Widget: "breadcrumb", ConfigVersion: 1, Breadcrumb: &platform.PageBreadcrumb{HomeLabel: "Private home", PageLabel: "Assets"}},
		{ID: "avatars", Widget: "avatar-stack", ConfigVersion: 1, Object: "build.person", CollectionVariable: "all", Avatar: &platform.PageAvatarStack{LabelField: "name", DetailFields: []string{"shift", "secret"}, ContextVariable: "active", ContextCollectionVariable: "related"}},
		{ID: "privateTitleAvatars", Widget: "avatar-stack", ConfigVersion: 1, Object: "build.person", CollectionVariable: "all", Avatar: &platform.PageAvatarStack{LabelField: "secret"}},
		{ID: "image", Widget: "static-image", ConfigVersion: 1, Image: &platform.PageStaticImage{URL: "https://example.com/original.png", Caption: &caption, Height: &height}},
		{ID: "privateTable", Widget: "table", ConfigVersion: 1, Object: "build.private", Fields: []string{"name"}, CollectionVariable: "privateAssets"},
		{ID: "privateContextAvatars", Widget: "avatar-stack", ConfigVersion: 1, Object: "build.person", CollectionVariable: "all", Avatar: &platform.PageAvatarStack{LabelField: "name", ContextVariable: "privateActive", ContextCollectionVariable: "privateRelated"}},
	}
	asset := platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: "build.asset"}
	person := platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: "build.person"}
	doc := platform.PageDocument{FormatVersion: 2, UIProfile: platform.PageUIProfile(), Root: "root", Nodes: map[string]platform.PageLayoutNode{"root": {Kind: "rows"}}, Variables: map[string]platform.PageVariable{
		"active":        {Scope: "page", Type: "record", Mode: "resource", Source: &platform.PageResourceSource{Kind: "record", Section: "table"}},
		"privateActive": {Scope: "page", Type: "record", Mode: "resource", Source: &platform.PageResourceSource{Kind: "record", Section: "privateTable"}},
	}, Queries: map[string]platform.PageQuery{}}
	for _, s := range sections {
		doc.Nodes[s.ID] = platform.PageLayoutNode{Kind: "widget", Section: s.ID}
		root := doc.Nodes["root"]
		root.Children = append(root.Children, s.ID)
		doc.Nodes["root"] = root
	}
	for _, q := range []struct{ id, name, parent string }{{"all", "allpeople", ""}, {"related", "assetpeople", "active"}, {"privateRelated", "privatepeople", "privateActive"}} {
		plan := platform.PageQuery{Object: person, Query: &platform.AssetBinding{Ref: platform.AssetRef{App: build.ID, Kind: platform.AssetQuery, Name: q.name}, SourceVersion: "1.query-1"}, Limit: 6, Sort: []string{"id"}}
		if q.parent != "" {
			plan.For = &platform.PageValue{Variable: q.parent}
		}
		doc.Queries[q.id] = plan
		doc.Variables[q.id] = platform.PageVariable{Scope: "page", Type: "object-set", Mode: "resource", Source: &platform.PageResourceSource{Kind: "plan", Query: q.id}}
	}
	for _, source := range []struct{ id, typ string }{{"assets", asset.Name}, {"privateAssets", "build.private"}} {
		doc.Queries[source.id] = platform.PageQuery{Object: platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: source.typ}, Limit: 6, Sort: []string{"id"}}
		doc.Variables[source.id] = platform.PageVariable{Scope: "page", Type: "object-set", Mode: "resource", Source: &platform.PageResourceSource{Kind: "plan", Query: source.id}}
	}
	if overlay {
		original := doc.Nodes["root"]
		doc.Nodes["panelRoot"] = original
		doc.Nodes["root"] = platform.PageLayoutNode{Kind: "rows", Children: []string{"trigger"}}
		doc.Nodes["trigger"] = platform.PageLayoutNode{Kind: "widget", Section: "trigger"}
		sections = append(sections, build.Section{ID: "trigger", Widget: "button", ConfigVersion: 1})
		for id, v := range doc.Variables {
			v.Scope = "overlay"
			v.Owner = "panel"
			doc.Variables[id] = v
		}
		for id, q := range doc.Queries {
			q.Owner = "panel"
			doc.Queries[id] = q
		}
		doc.Variables["open"] = platform.PageVariable{Scope: "page", Type: "boolean", Mode: "state", Initial: platform.Raw(false)}
		doc.Overlays = map[string]platform.PageOverlay{"panel": {Root: "panelRoot", Kind: "drawer", Title: "Original context panel", OpenVariable: "open"}}
		doc.Events = append(doc.Events, platform.PageEventBinding{Source: "trigger", Event: "click", Effects: []platform.PageEffect{{Kind: "set", Target: "open", Value: platform.Raw(true)}}})
	}
	for _, crumb := range []struct{ id, home string }{{"crumb", "home"}, {"secretCrumb", "home"}, {"privateHomeCrumb", "privatehome"}} {
		doc.Events = append(doc.Events, platform.PageEventBinding{Source: crumb.id, Event: "click", Control: "home", Effects: []platform.PageEffect{{Kind: "navigate", Navigate: &platform.PageNavigation{Page: platform.AssetRef{App: build.ID, Kind: platform.AssetPage, Name: crumb.home}}}}})
	}
	submit(build.PageType, "context", "create", map[string]any{"name": "context", "title": "Context views", "object": asset.Name, "document": doc, "sections": sections})
	preview, err := tn.PreviewRelease(builder, platform.AssetPage, "context")
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("preview %+v %v", preview, err)
	}
	saved, err := tn.SaveReleaseCandidate(builder, platform.AssetPage, "context", preview.CandidateID, "save", at)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := platform.ReadCandidate(saved, tn.releases.candidates[saved])
	if err != nil {
		t.Fatal(err)
	}
	for _, wanted := range []platform.AssetRef{{App: build.ID, Kind: platform.AssetPage, Name: "home"}, {App: build.ID, Kind: platform.AssetPage, Name: "privatehome"}, {App: build.ID, Kind: platform.AssetQuery, Name: "allpeople"}, {App: build.ID, Kind: platform.AssetQuery, Name: "assetpeople"}} {
		if !slices.ContainsFunc(candidate.Assets, func(a platform.ReleaseAsset) bool { return a.Ref == wanted }) {
			t.Fatal("original navigation/query dependency omitted", wanted)
		}
	}
	sections[1].Breadcrumb.HomeLabel = "Later home"
	sections[4].Avatar.DetailFields = []string{"shift"}
	*sections[6].Image.Caption = "Later caption"
	*sections[6].Image.Height = 99
	sections[6].Image.URL = "/later.png"
	submit(build.PageType, "context", "edit", map[string]any{"document": doc, "sections": sections})
	submit(build.QueryType, "assetpeople", "edit", map[string]any{"limit": 1})
	submit(build.QueryType, "assetpeople", "publish", map[string]any{})
	if _, err := tn.ActivateRelease(builder, saved, "activate", at); err != nil {
		t.Fatal(err)
	}
	check := func(current *Tenant) {
		t.Helper()
		for _, m := range []platform.Member{builder, reader} {
			found := false
			for _, d := range current.Definitions(m) {
				if d.Page == nil || d.Ref.Name != "context" {
					continue
				}
				found = true
				seen := map[string]platform.Section{}
				for _, s := range d.Page.Sections {
					seen[s.ID] = s
				}
				if seen["crumb"].Breadcrumb == nil || seen["crumb"].Breadcrumb.HomeLabel != "Original home" || seen["image"].Image == nil || seen["image"].Image.URL != "https://example.com/original.png" || seen["image"].Image.Caption == nil || *seen["image"].Image.Caption != "" || seen["image"].Image.Height == nil || *seen["image"].Image.Height != 0.5 {
					t.Fatal("later draft replaced frozen presentation")
				}
				if a := seen["avatars"].Avatar; a == nil || a.ContextVariable != "active" || a.ContextCollectionVariable != "related" {
					t.Fatal("original avatar bindings lost")
				}
				if overlay && (d.Page.Document.Queries["related"].Owner != "panel" || d.Page.Document.Variables["active"].Owner != "panel" || d.Page.Document.Variables["related"].Owner != "panel") {
					t.Fatal("frozen original overlay owner changed")
				}
				if q := d.Page.Document.Queries["related"]; q.Limit != 6 || q.Query == nil || q.Query.SourceVersion != "1.query-1" || q.For == nil || q.For.Variable != "active" {
					t.Fatal("later query replaced retained contextual source")
				}
				wantDetails := 2
				if m.ID == reader.ID {
					wantDetails = 1
					for _, id := range []string{"secretCrumb", "privateHomeCrumb", "privateTitleAvatars", "privateContextAvatars", "privateTable"} {
						if _, ok := seen[id]; ok {
							t.Fatal("private dependency retained", id)
						}
					}
					for _, id := range []string{"privateActive", "privateRelated"} {
						if _, ok := d.Page.Document.Variables[id]; ok {
							t.Fatal("private provenance resource retained", id)
						}
					}
				}
				if len(seen["avatars"].Avatar.DetailFields) != wantDetails {
					t.Fatal("private avatar detail was not cropped")
				}
			}
			if !found {
				t.Fatal("member context page disappeared")
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
