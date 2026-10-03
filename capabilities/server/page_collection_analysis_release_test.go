package platformserver

import (
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/platform"
	"testing"
	"time"
)

func TestFrozenCollectionAnalysisPermissionsReplayAndSnapshot(t *testing.T) {
	compose := func() *Tenant {
		tn, err := NewTenant("analysis", NewConsole("analysis", Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder}}}, Seat{Subjects: []string{"reader"}, Member: platform.Member{ID: "reader", Roles: map[string]string{build.ID: build.User}}}), build.New("analysis"))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := compose()
	builder, _ := tn.Member("builder")
	reader, _ := tn.Member("reader")
	at := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	key := 0
	var entries []Entry
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	submit := func(typ, id, verb string, payload any) {
		t.Helper()
		key++
		_, issue := tn.Submit(builder, &pb.Submission{TenantId: tn.ID, PrincipalId: builder.ID, Authority: build.ID, IdempotencyKey: fmt.Sprint(key), Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: typ + "." + verb, Version: 1}, Payload: platform.Raw(payload)}, at)
		if issue != nil {
			t.Fatal(typ, verb, issue.Message)
		}
	}
	fields := []build.Field{{Name: "status", Title: "Original status", Type: "choice", Choices: "ready,warning,maintenance,offline"}, {Name: "pressure", Title: "Original pressure", Type: "decimal"}, {Name: "temperature", Title: "Private temperature", Type: "decimal", Read: []string{build.Builder}}, {Name: "availability", Title: "Availability", Type: "decimal"}, {Name: "exposure", Title: "Exposure", Type: "decimal"}}
	submit(build.ObjectType, "asset", "create", map[string]any{"name": "asset", "title": "Assets", "fields": fields})
	submit(build.ObjectType, "asset", "publish", map[string]any{})
	object := platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: "build.asset"}
	sections := []build.Section{{ID: "bars", Widget: "collection-analysis", ConfigVersion: 1, CollectionVariable: "window", Analysis: &platform.PageCollectionAnalysis{Kind: "status-bars", GroupField: "status"}}, {ID: "signed", Widget: "collection-analysis", ConfigVersion: 1, CollectionVariable: "window", Analysis: &platform.PageCollectionAnalysis{Kind: "signed-counts", GroupField: "status", Steps: []platform.PageAnalysisStep{{Value: "ready", Label: "Active", Positive: true}, {Value: "warning", Label: "Warning"}, {Value: "maintenance", Label: "Maint."}, {Value: "offline", Label: "Offline"}}}}, {ID: "mean", Widget: "collection-analysis", ConfigVersion: 1, CollectionVariable: "window", AnalysisCountVariable: "count", AnalysisMeanVariable: "mean", Analysis: &platform.PageCollectionAnalysis{Kind: "derived-mean", Field: "pressure", Unit: "bar"}}, {ID: "axes", Widget: "collection-analysis", ConfigVersion: 1, CollectionVariable: "axes", AnalysisXVariable: "x", AnalysisYVariable: "y", Analysis: &platform.PageCollectionAnalysis{Kind: "record-axes", Fields: []string{"pressure", "temperature", "availability", "exposure"}}}}
	doc := platform.PageDocument{FormatVersion: 2, UIProfile: platform.PageUIProfile(), Root: "root", Nodes: map[string]platform.PageLayoutNode{"root": {Kind: "rows"}}, Variables: map[string]platform.PageVariable{"window": {Scope: "page", Type: "object-set", Mode: "resource", Source: &platform.PageResourceSource{Kind: "plan", Query: "read"}}, "axes": {Scope: "page", Type: "object-set", Mode: "resource", Source: &platform.PageResourceSource{Kind: "plan", Query: "axes"}}, "count": {Scope: "page", Type: "decimal", Mode: "aggregate", Source: &platform.PageResourceSource{Kind: "count", Query: "read"}}, "mean": {Scope: "page", Type: "number", Mode: "aggregate", Source: &platform.PageResourceSource{Kind: "aggregate", Query: "read", Measure: "avg:pressure"}}, "x": {Scope: "page", Type: "string", Mode: "state", Initial: platform.Raw("pressure")}, "y": {Scope: "page", Type: "string", Mode: "state", Initial: platform.Raw("temperature")}}, Queries: map[string]platform.PageQuery{"read": {Object: object, Limit: 100, Sort: []string{"id"}}, "axes": {Object: object, Limit: 80, Sort: []string{"id"}}}}
	for _, s := range sections {
		doc.Nodes[s.ID] = platform.PageLayoutNode{Kind: "widget", Section: s.ID}
		root := doc.Nodes["root"]
		root.Children = append(root.Children, s.ID)
		doc.Nodes["root"] = root
	}
	submit(build.PageType, "page", "create", map[string]any{"name": "analysis", "title": "Original analysis", "object": object.Name, "sections": sections, "document": doc})
	preview, err := tn.PreviewRelease(builder, platform.AssetPage, "page")
	if err != nil || preview.Diagnostic != "" {
		t.Fatal(preview, err)
	}
	saved, err := tn.SaveReleaseCandidate(builder, platform.AssetPage, "page", preview.CandidateID, "save", at)
	if err != nil {
		t.Fatal(err)
	}
	sections[2].Analysis.Unit = "later"
	submit(build.PageType, "page", "edit", map[string]any{"sections": sections})
	if _, err := tn.ActivateRelease(builder, saved, "activate", at); err != nil {
		t.Fatal(err)
	}
	check := func(current *Tenant) {
		t.Helper()
		for _, member := range []platform.Member{builder, reader} {
			found := false
			for _, d := range current.Definitions(member) {
				if d.Page == nil || d.Ref.Name != "analysis" {
					continue
				}
				found = true
				seen := map[string]platform.Section{}
				for _, s := range d.Page.Sections {
					seen[s.ID] = s
				}
				if seen["mean"].Analysis == nil || seen["mean"].Analysis.Unit != "bar" || seen["signed"].Analysis == nil || seen["signed"].Analysis.Steps[0].Value != "ready" {
					t.Fatal("frozen analysis lost original configuration")
				}
				if member.ID == reader.ID {
					if _, ok := seen["axes"]; ok {
						t.Fatal("private coordinate field was exposed")
					}
					if _, ok := d.Page.Document.Nodes["axes"]; ok {
						t.Fatal("orphan private axes node survived")
					}
					if _, ok := d.Page.Document.Variables["x"]; ok {
						t.Fatal("orphan private coordinate state survived")
					}
				} else if seen["axes"].Analysis == nil || d.Page.Document.Queries["axes"].Limit != 80 || string(d.Page.Document.Variables["x"].Initial) != `"pressure"` {
					t.Fatal("original builder axes changed")
				}
				if err := d.Page.Document.Check(d.Page.Sections); err != nil {
					t.Fatal(err)
				}
			}
			if !found {
				t.Fatal("analysis page disappeared")
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
