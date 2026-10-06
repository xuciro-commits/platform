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

func TestFrozenExplorationPortsPermissionsReplayAndSnapshot(t *testing.T) {
	testFrozenExploration(t, false)
}
func TestFrozenOverlayExplorationPortsPermissionsReplayAndSnapshot(t *testing.T) {
	testFrozenExploration(t, true)
}
func testFrozenExploration(t *testing.T, overlay bool) {
	compose := func() *Tenant {
		tn, err := NewTenant("exploration", NewConsole("exploration", Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder}}}, Seat{Subjects: []string{"reader"}, Member: platform.Member{ID: "reader", Roles: map[string]string{build.ID: build.User}}}), build.New("exploration"))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := compose()
	builder, _ := tn.Member("builder")
	reader, _ := tn.Member("reader")
	at := time.Date(2026, 10, 3, 22, 0, 0, 0, time.UTC)
	key := 0
	var entries []Entry
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	submit := func(typ, id, verb string, payload any) {
		t.Helper()
		key++
		_, err := tn.Submit(builder, &pb.Submission{TenantId: tn.ID, PrincipalId: builder.ID, Authority: build.ID, IdempotencyKey: fmt.Sprint(key), Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: typ + "." + verb, Version: 1}, Payload: platform.Raw(payload)}, at)
		if err != nil {
			t.Fatalf("%s %s %s: %s", typ, id, verb, err.Message)
		}
	}
	submit(build.ObjectType, "asset", "create", map[string]any{"name": "asset", "title": "Assets", "fields": []build.Field{{Name: "name", Title: "Name", Type: "text"}, {Name: "status", Title: "Status", Type: "choice", Choices: "Active,Offline"}}})
	submit(build.ObjectType, "asset", "publish", map[string]any{})
	submit(build.ObjectType, "sensor", "create", map[string]any{"name": "sensor", "title": "Sensors", "fields": []build.Field{{Name: "name", Title: "Name", Type: "text"}, {Name: "asset", Title: "Private original asset", Type: "reference", Ref: "build.asset", Read: []string{build.Builder}}, {Name: "publicasset", Title: "Later public asset", Type: "reference", Ref: "build.asset"}}})
	submit(build.ObjectType, "sensor", "publish", map[string]any{})
	submit(build.ObjectType, "alert", "create", map[string]any{"name": "alert", "title": "Alerts", "fields": []build.Field{{Name: "name", Title: "Name", Type: "text"}, {Name: "privatetitle", Title: "Private title", Type: "text", Read: []string{build.Builder}}, {Name: "asset", Title: "Asset", Type: "reference", Ref: "build.asset"}}})
	submit(build.ObjectType, "alert", "publish", map[string]any{})
	submit(build.ObjectType, "private", "create", map[string]any{"name": "private", "title": "Private records", "fields": []build.Field{{Name: "name", Title: "Name", Type: "text"}}, "access": []build.Access{{Role: build.User, Read: "none"}}})
	submit(build.ObjectType, "private", "publish", map[string]any{})
	for _, link := range []struct{ name, child string }{{"sensors", "build.sensor"}, {"alerts", "build.alert"}} {
		submit(build.LinkTypeType, link.name, "create", map[string]any{"name": link.name, "title": link.name, "description": "Original retained relationship", "parent": "build.asset", "child": link.child, "via": "asset", "forward": link.name, "reverse": "asset"})
		submit(build.LinkTypeType, link.name, "publish", map[string]any{})
	}
	ref := func(kind platform.AssetKind, name string) platform.AssetRef {
		return platform.AssetRef{App: build.ID, Kind: kind, Name: name}
	}
	asset, sensor, alert := ref(platform.AssetObject, "build.asset"), ref(platform.AssetObject, "build.sensor"), ref(platform.AssetObject, "build.alert")
	binding := func(name string) platform.AssetBinding {
		return platform.AssetBinding{Ref: ref(platform.AssetLinkType, name), SourceVersion: "1.link-1"}
	}
	graph := platform.PageGraphExplorer{Objects: []platform.PageGraphObject{{Object: asset, LabelField: "name"}, {Object: sensor, LabelField: "name"}, {Object: alert, LabelField: "privatetitle"}}, Relations: []platform.PageGraphRelation{{ID: "sensors", Binding: binding("sensors")}, {ID: "alerts", Binding: binding("alerts")}}, Outputs: []platform.PageGraphOutput{{ID: "asset", Object: asset, Variable: "assetOutput"}, {ID: "sensor", Object: sensor, Variable: "sensorOutput"}, {ID: "alert", Object: alert, Variable: "alertOutput"}}}
	sections := []build.Section{
		{ID: "resources", Widget: "resource-list", ConfigVersion: 1, CollectionVariable: "assets", ResourceList: &platform.PageResourceList{LabelField: "name", StatusField: "status", StatusTones: []platform.PageEventTone{{Value: "Active", Tone: "success"}}}},
		{ID: "graph", Widget: "graph-explorer", ConfigVersion: 1, RecordVariable: "active", GraphExplorer: &graph},
		{ID: "vertex", Widget: "vertex-graph", ConfigVersion: 1, RecordVariable: "active", VertexGraph: &platform.PageVertexGraph{Groups: []platform.PageNeighborhoodGroup{{ID: "sensors", Binding: binding("sensors"), Direction: "forward", Limit: 4, Badge: "S", Tone: "success"}, {ID: "alerts", Binding: binding("alerts"), Direction: "forward", Limit: 3, Badge: "A", Tone: "danger"}}}},
		{ID: "directory", Widget: "asset-directory", ConfigVersion: 1, AssetDirectory: &platform.PageAssetDirectory{Items: []platform.PageAssetDirectoryItem{{ID: "assets", Label: "Operations / Assets", Asset: platform.AssetBinding{Ref: asset, SourceVersion: "1"}}, {ID: "private", Label: "Operations / Alerts", Asset: platform.AssetBinding{Ref: ref(platform.AssetObject, "build.private"), SourceVersion: "1"}}, {ID: "playbook", Label: "Playbooks / SOP-14", Asset: binding("sensors")}, {ID: "dataset", Label: "Datasets / telemetry_q3", Asset: binding("alerts")}}}},
		{ID: "sensorCard", Widget: "record-card", ConfigVersion: 1, Object: sensor.Name, RecordVariable: "sensorOutput", RecordCard: &platform.PageRecordCard{LabelField: "name", Tone: "neutral"}},
		{ID: "alertCard", Widget: "record-card", ConfigVersion: 1, Object: alert.Name, RecordVariable: "alertOutput", RecordCard: &platform.PageRecordCard{LabelField: "name", Tone: "neutral"}},
	}
	doc := platform.PageDocument{FormatVersion: 2, UIProfile: platform.PageUIProfile(), Root: "root", Nodes: map[string]platform.PageLayoutNode{"root": {Kind: "rows"}}, Variables: map[string]platform.PageVariable{
		"assets": {Scope: "page", Type: "object-set", Mode: "resource", Source: &platform.PageResourceSource{Kind: "plan", Query: "assets"}}, "active": {Scope: "page", Type: "record", Mode: "resource", Source: &platform.PageResourceSource{Kind: "record", Section: "resources"}},
	}, Queries: map[string]platform.PageQuery{"assets": {Object: asset, Limit: 12, Sort: []string{"id"}}}}
	for _, s := range sections {
		doc.Nodes[s.ID] = platform.PageLayoutNode{Kind: "widget", Section: s.ID}
		root := doc.Nodes["root"]
		root.Children = append(root.Children, s.ID)
		doc.Nodes["root"] = root
	}
	for _, o := range graph.Outputs {
		doc.Variables[o.Variable] = platform.PageVariable{Scope: "page", Type: "record", Mode: "resource", Source: &platform.PageResourceSource{Kind: "record", Section: "graph", Port: o.ID}}
	}
	if overlay {
		doc.Nodes["panelRoot"] = doc.Nodes["root"]
		doc.Nodes["root"] = platform.PageLayoutNode{Kind: "rows", Children: []string{"trigger"}}
		doc.Nodes["trigger"] = platform.PageLayoutNode{Kind: "widget", Section: "trigger"}
		sections = append(sections, build.Section{ID: "trigger", Widget: "button", ConfigVersion: 1})
		for id, v := range doc.Variables {
			v.Scope = "overlay"
			v.Owner = "panel"
			doc.Variables[id] = v
		}
		q := doc.Queries["assets"]
		q.Owner = "panel"
		doc.Queries["assets"] = q
		doc.Variables["open"] = platform.PageVariable{Scope: "page", Type: "boolean", Mode: "state", Initial: platform.Raw(false)}
		doc.Overlays = map[string]platform.PageOverlay{"panel": {Root: "panelRoot", Kind: "drawer", Title: "Original exploration", OpenVariable: "open"}}
		doc.Events = []platform.PageEventBinding{{Source: "trigger", Event: "click", Effects: []platform.PageEffect{{Kind: "set", Target: "open", Value: platform.Raw(true)}}}}
	}
	submit(build.PageType, "page", "create", map[string]any{"name": "explore", "title": "Original exploration", "object": asset.Name, "sections": sections, "document": doc})
	preview, err := tn.PreviewRelease(builder, platform.AssetPage, "page")
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("preview %+v %v", preview, err)
	}
	saved, err := tn.SaveReleaseCandidate(builder, platform.AssetPage, "page", preview.CandidateID, "save", at)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := platform.ReadCandidate(saved, tn.releaseCandidates[saved])
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range []platform.AssetBinding{binding("sensors"), binding("alerts")} {
		if !slices.ContainsFunc(candidate.Assets, func(a platform.ReleaseAsset) bool { return a.Ref == b.Ref && a.SourceVersion == b.SourceVersion }) {
			t.Fatal("original relation omitted", b)
		}
	}
	sections[0].ResourceList.StatusTones = []platform.PageEventTone{{Value: "Active", Tone: "warning"}}
	sections[3].AssetDirectory.Items[0].Label = "Later label"
	sections[2].VertexGraph.Groups[0].Tone = "info"
	submit(build.PageType, "page", "edit", map[string]any{"sections": sections})
	submit(build.LinkTypeType, "sensors", "edit", map[string]any{"title": "Later relationship title"})
	submit(build.LinkTypeType, "sensors", "publish", map[string]any{})
	if _, err := tn.ActivateRelease(builder, saved, "activate", at); err != nil {
		t.Fatal(err)
	}
	submit(asset.Name, "A", "create", map[string]any{"name": "Original asset", "status": "Active"})
	submit(sensor.Name, "S", "create", map[string]any{"name": "Original sensor", "asset": "A", "publicasset": ""})
	check := func(current *Tenant) {
		t.Helper()
		for _, member := range []platform.Member{builder, reader} {
			found := false
			for _, d := range current.Definitions(member) {
				if d.Page == nil || d.Ref.Name != "explore" {
					continue
				}
				found = true
				seen := map[string]platform.Section{}
				for _, s := range d.Page.Sections {
					seen[s.ID] = s
				}
				r, g, v, dir := seen["resources"].ResourceList, seen["graph"].GraphExplorer, seen["vertex"].VertexGraph, seen["directory"].AssetDirectory
				if r == nil || r.StatusTones[0].Tone != "success" || g == nil || v == nil || dir == nil || dir.Items[0].Label != "Operations / Assets" {
					t.Fatal("later draft changed original native exploration")
				}
				if q := d.Page.Document.Queries["assets"]; q.Limit != 12 || !slices.Equal(q.Sort, []string{"id"}) {
					t.Fatal("resource window changed")
				}
				if member.ID == builder.ID {
					if len(g.Objects) != 3 || len(g.Relations) != 2 || len(g.Outputs) != 3 || len(v.Groups) != 2 || v.Groups[0].Tone != "success" || len(dir.Items) != 4 || d.Page.RecordResourceObject("sensorOutput") != sensor {
						t.Fatal("original typed builder graph changed")
					}
					for _, relation := range g.Relations {
						if relation.Binding.SourceVersion != "1.link-1" {
							t.Fatal("latest relationship replaced original binding")
						}
					}
				} else {
					if len(g.Objects) != 1 || len(g.Relations) != 0 || len(g.Outputs) != 1 || len(v.Groups) != 1 || v.Groups[0].Badge != "A" || len(dir.Items) != 2 {
						t.Fatal("hidden relation/title/directory dependencies survived", g, v, dir)
					}
					for _, id := range []string{"sensorOutput", "alertOutput"} {
						if _, ok := d.Page.Document.Variables[id]; ok {
							t.Fatal("unreachable typed output retained", id)
						}
					}
					for _, id := range []string{"sensorCard", "alertCard"} {
						if _, ok := seen[id]; ok {
							t.Fatal("orphan graph output consumer retained", id)
						}
					}
				}
				if overlay && d.Page.Document.Variables["active"].Owner != "panel" {
					t.Fatal("original overlay owner changed")
				}
			}
			if !found {
				t.Fatal("original exploration page missing")
			}
		}
		rows, issue := current.TraverseLink(builder, binding("sensors"), "forward", "A", platform.Query{Limit: 4}, at)
		if issue != nil || rows.Total != 1 || len(rows.Records) != 1 {
			t.Fatal("original retained traversal changed", issue, rows)
		}
		if _, issue := current.TraverseLink(reader, binding("sensors"), "forward", "A", platform.Query{Limit: 4}, at); issue == nil {
			t.Fatal("private original relation fell back to later public relation")
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
