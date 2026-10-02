package platformserver

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/platform"
)

func TestRecordTimelineCandidateRetainsMappingsAndClipsHiddenFields(t *testing.T) {
	compose := func() *Tenant {
		tn, err := NewTenant("timeline", NewConsole("timeline", Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder}}}, Seat{Subjects: []string{"reader"}, Member: platform.Member{ID: "reader", Roles: map[string]string{build.ID: build.User}}}), build.New("timeline"))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := compose()
	builder, _ := tn.Member("builder")
	reader, _ := tn.Member("reader")
	at := time.Date(2026, 10, 2, 15, 0, 0, 0, time.UTC)
	key := 0
	var journal []Entry
	tn.Record = func(e Entry) { journal = append(journal, e) }
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { journal = append(journal, e); return e.Body, nil }
	submit := func(typ, id, schema string, payload any) {
		t.Helper()
		key++
		if _, err := tn.Submit(builder, &pb.Submission{TenantId: tn.ID, PrincipalId: builder.ID, Authority: build.ID, IdempotencyKey: fmt.Sprint(key), Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: platform.Raw(payload)}, at); err != nil {
			t.Fatalf("%s: %s", schema, err.Message)
		}
	}
	submit(build.ObjectType, "O", build.ObjectType+".create", map[string]any{"name": "job", "title": "Job", "fields": []build.Field{{Name: "title", Title: "Title", Type: "text"}, {Name: "start", Title: "Start", Type: "date"}, {Name: "end", Title: "End", Type: "date", Read: []string{build.Builder}}}})
	submit(build.ObjectType, "O", build.SchemaPublish, struct{}{})
	object := platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: "build.job"}
	doc := platform.PageDocument{FormatVersion: 2, UIProfile: platform.PageUIProfile(), Root: "root", Nodes: map[string]platform.PageLayoutNode{"root": {Kind: "rows", Children: []string{"schedule"}}, "schedule": {Kind: "widget", Section: "schedule"}}, Variables: map[string]platform.PageVariable{"window": {Scope: "page", Type: "object-set", Mode: "resource", Source: &platform.PageResourceSource{Kind: "plan", Query: "read"}}}, Queries: map[string]platform.PageQuery{"read": {Object: object, Limit: 1}}}
	sections := []build.Section{{ID: "schedule", Widget: "record-timeline", ConfigVersion: 1, Title: "Jobs", CollectionVariable: "window", TimeStart: "start", TimeEnd: "end", TimeLabel: "title"}}
	submit(build.PageType, "P", build.PageType+".create", map[string]any{"name": "jobs", "title": "Jobs", "object": "build.job", "sections": sections, "document": doc})
	preview, err := tn.PreviewRelease(builder, platform.AssetPage, "P")
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("preview %+v %v", preview, err)
	}
	id, err := tn.SaveReleaseCandidate(builder, platform.AssetPage, "P", preview.CandidateID, "save", at)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := platform.ReadCandidate(id, tn.releaseCandidates[id])
	if err != nil {
		t.Fatal(err)
	}
	for i, a := range candidate.Assets {
		if a.Ref.Kind != platform.AssetPage {
			continue
		}
		var p platform.Page
		_ = json.Unmarshal(a.Body, &p)
		p.Sections[0].TimeStart = "title"
		bad := append([]platform.ReleaseAsset(nil), candidate.Assets...)
		bad[i].Body = platform.Raw(p)
		if _, err := platform.Candidate([]platform.AssetRef{a.Ref}, bad); err == nil {
			t.Fatal("frozen candidate accepted a non-temporal start")
		}
	}
	sections[0].TimeEnd = ""
	submit(build.PageType, "P", build.PageType+".edit", map[string]any{"sections": sections})
	if _, err := tn.ActivateRelease(builder, id, "activate", at); err != nil {
		t.Fatal(err)
	}
	check := func(tn *Tenant) {
		t.Helper()
		var found bool
		for _, d := range tn.Definitions(builder) {
			if d.Page != nil && d.Ref.Name == "jobs" {
				found = true
				if len(d.Page.Sections) != 1 || d.Page.Sections[0].TimeEnd != "end" {
					t.Fatal("draft changed frozen mapping")
				}
			}
		}
		if !found {
			t.Fatal("timeline page disappeared")
		}
		for _, d := range tn.Definitions(reader) {
			if d.Page != nil && slices.ContainsFunc(d.Page.Sections, func(s platform.Section) bool { return s.Widget == "record-timeline" }) {
				t.Fatal("hidden time mapping disclosed a timeline")
			}
		}
	}
	check(tn)
	CheckReplay(t, tn, journal, compose)
	snapshot, _, err := tn.Snapshot(func() int64 { return 0 })
	if err != nil {
		t.Fatal(err)
	}
	restored := compose()
	if err := restored.Restore(snapshot); err != nil {
		t.Fatal(err)
	}
	check(restored)
}
