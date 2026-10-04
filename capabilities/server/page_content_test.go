package platformserver

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/platform"
	"testing"
	"time"
)

func TestPublishedPageContentIdentityPermissionsReplayAndSnapshot(t *testing.T) {
	compose := func() *Tenant {
		tn, err := NewTenant("page-content", NewConsole("page-content", Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder}}}, Seat{Subjects: []string{"reader"}, Member: platform.Member{ID: "reader", Roles: map[string]string{build.ID: build.User}}}), build.New("page-content"))
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
	submit(build.ObjectType, "note", "create", map[string]any{"name": "note", "title": "Notes", "fields": []build.Field{{Name: "name", Title: "Name", Type: "text"}, {Name: "secret", Title: "Private", Type: "text", Read: []string{build.Builder}}}})
	submit(build.ObjectType, "note", "publish", map[string]any{})
	object := platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: "build.note"}
	ref := platform.AssetRef{App: build.ID, Kind: platform.AssetPage, Name: "child"}
	sections := []build.Section{{ID: "text", Widget: "text", ConfigVersion: 1, Text: "FIRST"}, {ID: "detail", Widget: "detail", ConfigVersion: 1, RecordVariable: "input", Fields: []string{"name", "secret"}}}
	doc := platform.PageDocument{FormatVersion: 2, UIProfile: platform.PageUIProfile(), Root: "root", Nodes: map[string]platform.PageLayoutNode{"root": {Kind: "rows", Children: []string{"text", "detail"}}, "text": {Kind: "widget", Section: "text"}, "detail": {Kind: "widget", Section: "detail"}}, Variables: map[string]platform.PageVariable{"input": {Scope: "page", Type: "record", Mode: "input"}}, Interface: &platform.PageInterface{Version: 1, Inputs: map[string]platform.PagePort{"record": {Variable: "input", Type: "record", Object: &object, Required: true}}}}
	submit(build.PageType, "page", "create", map[string]any{"name": "child", "title": "Original child", "object": object.Name, "sections": sections, "document": doc})
	submit(build.PageType, "page", "publish", map[string]any{})
	current := func(current *Tenant) platform.Definition {
		t.Helper()
		for _, d := range current.Definitions(builder) {
			if d.Ref == ref {
				return d
			}
		}
		t.Fatal("child missing")
		return platform.Definition{}
	}
	first := current(tn)
	if platform.CheckPageContentVersion(first.ContentVersion) != nil {
		t.Fatal("missing original content identity")
	}
	sections[0].Text = "UNPUBLISHED"
	submit(build.PageType, "page", "edit", map[string]any{"sections": sections})
	old, err := tn.PageContentDefinition(reader, ref, first.ContentVersion)
	if err != nil || old.Page.Sections[0].Text != "FIRST" {
		t.Fatal(old, err)
	}
	preview, err2 := tn.PreviewRelease(builder, platform.AssetPage, "page")
	if err2 != nil || preview.Diagnostic != "" {
		t.Fatal(preview, err2)
	}
	candidate, err2 := tn.SaveReleaseCandidate(builder, platform.AssetPage, "page", preview.CandidateID, "candidate", at)
	if err2 != nil {
		t.Fatal(err2)
	}
	rawCandidate, err2 := platform.ReadCandidate(candidate, tn.releaseCandidates[candidate])
	if err2 != nil {
		t.Fatal(err2)
	}
	var unpublished platform.Page
	for _, asset := range rawCandidate.Assets {
		if asset.Ref == ref {
			if err := json.Unmarshal(asset.Body, &unpublished); err != nil {
				t.Fatal(err)
			}
		}
	}
	draftVersion, _ := platform.PageContentVersion(unpublished)
	if _, err := tn.PageContentDefinition(reader, ref, draftVersion); err == nil {
		t.Fatal("unactivated candidate became a public version")
	}
	if _, err := tn.ActivateRelease(builder, candidate, "activate", at); err != nil {
		t.Fatal(err)
	}
	second := current(tn)
	if second.ContentVersion == first.ContentVersion || second.ContentVersion != draftVersion {
		t.Fatal("activation lost the exact descriptor identity")
	}
	check := func(current *Tenant) {
		t.Helper()
		for _, member := range []platform.Member{builder, reader} {
			d, err := current.PageContentDefinition(member, ref, first.ContentVersion)
			if err != nil || d.Page.Sections[0].Text != "FIRST" || d.ContentVersion != first.ContentVersion || d.Version != first.Version {
				t.Fatal(d, err)
			}
			fields := d.Page.Sections[1].Fields
			if member.ID == reader.ID && (len(fields) != 1 || fields[0] != "name") {
				t.Fatal("old content revived private fields")
			}
			if member.ID == builder.ID && len(fields) != 2 {
				t.Fatal("member projection modified original snapshot")
			}
			if err := d.Page.Document.Check(d.Page.Sections); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := current.PageContentDefinition(platform.Member{ID: "outsider"}, ref, first.ContentVersion); err == nil {
			t.Fatal("digest granted membership")
		}
		if _, err := current.PageContentDefinition(reader, ref, "page.sha256."+fmt.Sprintf("%064x", 1)); err == nil {
			t.Fatal("unknown version fell back to latest")
		}
	}
	check(tn)
	handler := NewHost(Tokens(map[string]string{"reader": "reader", "builder": "builder"}), tn).Handler()
	for _, version := range []string{first.ContentVersion, "page.sha256." + fmt.Sprintf("%064x", 1), "latest"} {
		request := httptest.NewRequest("GET", "/v1/pages/build/child/"+version, nil)
		request.Header.Set("Authorization", "Bearer reader")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if version == first.ContentVersion {
			var d platform.Definition
			if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &d) != nil || d.ContentVersion != version || d.Page.Sections[0].Text != "FIRST" || len(d.Page.Sections[1].Fields) != 1 {
				t.Fatal(response.Code, response.Body.String())
			}
		} else if response.Code == 200 {
			t.Fatal("HTTP reader returned a different page version")
		}
	}
	submit(build.PageType, "page", "publish", map[string]any{})
	check(tn)
	CheckReplay(t, tn, entries, compose)
	raw, _, err2 := tn.Snapshot(func() int64 { return int64(len(entries)) })
	if err2 != nil {
		t.Fatal(err2)
	}
	restored := compose()
	if err := restored.Restore(raw); err != nil {
		t.Fatal(err)
	}
	check(restored)
}
