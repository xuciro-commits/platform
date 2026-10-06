package platformserver

import (
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/platform"
	"testing"
	"time"
)

func TestOverlayQueryFreezeProjectionAndReplay(t *testing.T) {
	const tenant = "overlay-query-state"
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
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	at := time.Now().UTC()
	key := 0
	submit := func(typ, id, verb string, payload any) {
		t.Helper()
		key++
		_, err := tn.Submit(member, &pb.Submission{TenantId: tenant, PrincipalId: member.ID, Authority: build.ID, IdempotencyKey: fmt.Sprint(key), Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: typ + "." + verb, Version: 1}, Payload: platform.Raw(payload)}, at)
		if err != nil {
			t.Fatal(err)
		}
	}
	submit(build.ObjectType, "object", "create", map[string]any{"name": "note", "title": "Note", "access": []build.Access{{Role: build.User, Read: "all"}}, "fields": []build.Field{{Name: "note", Title: "Note", Type: "text"}, {Name: "secret", Title: "Secret", Type: "text", Read: []string{build.Builder}}, {Name: "active", Title: "Active", Type: "boolean", Read: []string{build.Builder}}}})
	submit(build.ObjectType, "object", "publish", map[string]any{})
	object := platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: "build.note"}
	doc := &platform.PageDocument{FormatVersion: 2, UIProfile: platform.PageUIProfile(), Root: "root", Nodes: map[string]platform.PageLayoutNode{
		"root": {Kind: "rows", Children: []string{"trigger"}}, "trigger": {Kind: "widget", Section: "trigger"}, "picker": {Kind: "rows", Children: []string{"input", "table", "detail"}}, "input": {Kind: "widget", Section: "input", ValueVariable: "local"}, "table": {Kind: "widget", Section: "table"}, "detail": {Kind: "widget", Section: "detail"}},
		Overlays: map[string]platform.PageOverlay{"picker": {Root: "picker", Kind: "modal", Title: "Picker", OpenVariable: "open"}}, Variables: map[string]platform.PageVariable{
			"open": {Scope: "page", Type: "boolean", Mode: "state", Initial: json.RawMessage(`false`)}, "local": {Scope: "overlay", Owner: "picker", Type: "string", Mode: "state", Initial: json.RawMessage(`"initial"`)}, "window": {Scope: "overlay", Owner: "picker", Type: "object-set", Mode: "resource", Source: &platform.PageResourceSource{Kind: "plan", Query: "read"}}, "selected": {Scope: "overlay", Owner: "picker", Type: "record", Mode: "resource", Source: &platform.PageResourceSource{Kind: "record", Section: "table"}}},
		Queries: map[string]platform.PageQuery{"read": {Owner: "picker", Object: object, Search: &platform.PageValue{Variable: "local"}, Conditions: []platform.PageQueryCondition{{Field: "secret", Op: "=", Value: platform.PageValue{Literal: json.RawMessage(`"private"`)}}}, Limit: 20}}, Events: []platform.PageEventBinding{{Source: "trigger", Event: "click", Effects: []platform.PageEffect{{Kind: "set", Target: "open", Value: json.RawMessage(`true`)}}}}}
	doc.Nodes["picker"] = platform.PageLayoutNode{Kind: "rows", Children: []string{"input", "filters", "table", "detail"}}
	doc.Nodes["filters"] = platform.PageLayoutNode{Kind: "widget", Section: "filters"}
	doc.Variables["localFilters"] = platform.PageVariable{Scope: "overlay", Owner: "picker", Type: "filter", Mode: "resource", Source: &platform.PageResourceSource{Kind: "filter", Section: "filters"}}
	sections := []build.Section{{ID: "trigger", Widget: "button", ConfigVersion: 1}, {ID: "input", Widget: "input", ConfigVersion: 1}, {ID: "table", Widget: "table", ConfigVersion: 1, Fields: []string{"note"}, CollectionVariable: "window"}, {ID: "detail", Widget: "detail", ConfigVersion: 1, Fields: []string{"note"}, RecordVariable: "selected"}}
	sections = append(sections, build.Section{ID: "filters", Widget: "filter", ConfigVersion: 1, Fields: []string{"active"}})
	submit(build.PageType, "page", "create", map[string]any{"name": "work", "title": "Work", "object": object.Name, "sections": sections, "document": doc})
	preview, err := tn.PreviewRelease(member, platform.AssetPage, "page")
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("preview %+v %v", preview, err)
	}
	if _, err = tn.SaveReleaseCandidate(member, platform.AssetPage, "page", preview.CandidateID, "freeze", at); err != nil {
		t.Fatal(err)
	}
	changed := *doc
	changed.Queries = map[string]platform.PageQuery{"read": doc.Queries["read"]}
	q := changed.Queries["read"]
	q.Limit = 1
	changed.Queries["read"] = q
	submit(build.PageType, "page", "edit", map[string]any{"document": changed})
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
		for _, d := range current.definitions {
			if d.Page != nil && d.Ref.Name == "work" {
				if d.Page.Document.Queries["read"].Owner != "picker" || d.Page.Document.Queries["read"].Limit != 20 || d.Page.Document.Variables["selected"].Owner != "picker" || d.Page.Document.Variables["localFilters"].Source.Kind != "filter" || d.Page.Document.Variables["localFilters"].Owner != "picker" {
					t.Fatal("overlay ownership or frozen plan drifted")
				}
			}
		}
		reader, _ := current.Member("reader")
		for _, d := range current.Definitions(reader) {
			if d.Page != nil && d.Ref.Name == "work" {
				if _, ok := d.Page.Document.Variables["localFilters"]; ok {
					t.Fatal("hidden filter resource exposed")
				}
				if len(d.Page.Document.Queries) != 0 {
					t.Fatal("private overlay plan exposed")
				}
				for _, s := range d.Page.Sections {
					if s.ID == "table" || s.ID == "detail" {
						t.Fatal("consumer of hidden query remained")
					}
				}
			}
		}
	}
}
