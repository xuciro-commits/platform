package platformserver

import (
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/platform"
	"testing"
	"time"
)

func TestPageQuerySetsFreezeReplayAndPruneMembers(t *testing.T) {
	const tenant = "set-plan-state"
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
		_, problem := tn.Submit(member, &pb.Submission{TenantId: tenant, PrincipalId: member.ID, Authority: build.ID, IdempotencyKey: fmt.Sprint(key), Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: typ + "." + verb, Version: 1}, Payload: platform.Raw(payload)}, at)
		if problem != nil {
			t.Fatal(problem.Message)
		}
	}
	submit(build.ObjectType, "object", "create", map[string]any{"name": "note", "title": "Note", "access": []build.Access{{Role: build.User, Read: "all"}}, "fields": []build.Field{{Name: "note", Title: "Note", Type: "text"}, {Name: "secret", Title: "Secret", Type: "boolean", Read: []string{build.Builder}}}})
	submit(build.ObjectType, "object", "publish", map[string]any{})
	submit(build.QueryType, "query", "create", map[string]any{"name": "fixed", "title": "Fixed", "description": "Fixed set source", "object": "build.note", "domain": []any{[]any{"note", "=", "A"}}, "limit": 1})
	submit(build.QueryType, "query", "publish", map[string]any{})
	submit(build.QueryType, "query", "edit", map[string]any{"domain": []any{[]any{"note", "=", "B"}}})
	submit(build.QueryType, "query", "publish", map[string]any{})
	object := platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: "build.note"}
	plans := map[string]platform.PageQuery{
		"left":    {Object: object, Limit: 1, Query: &platform.AssetBinding{Ref: platform.AssetRef{App: build.ID, Kind: platform.AssetQuery, Name: "fixed"}, SourceVersion: "1.query-1"}},
		"right":   {Object: object, Limit: 1, Conditions: []platform.PageQueryCondition{{Field: "note", Op: "=", Value: platform.PageValue{Literal: platform.Raw("B")}}}},
		"read":    {Object: object, Limit: 20, Set: &platform.PageQuerySet{Op: "union", Inputs: []string{"left", "right"}}},
		"secret":  {Object: object, Limit: 1, Conditions: []platform.PageQueryCondition{{Field: "secret", Op: "=", Value: platform.PageValue{Literal: platform.Raw(true)}}}},
		"private": {Object: object, Limit: 20, Set: &platform.PageQuerySet{Op: "union", Inputs: []string{"left", "secret"}}},
	}
	document := &platform.PageDocument{FormatVersion: 2, UIProfile: platform.PageUIProfile(), Root: "root", Nodes: map[string]platform.PageLayoutNode{"root": {Kind: "rows", Children: []string{"text", "private"}}, "text": {Kind: "widget", Section: "text"}, "private": {Kind: "widget", Section: "private", VisibleWhen: "hasPrivate"}}, Variables: map[string]platform.PageVariable{
		"window":        {Scope: "page", Type: "object-set", Mode: "resource", Source: &platform.PageResourceSource{Kind: "plan", Query: "read"}},
		"privateWindow": {Scope: "page", Type: "object-set", Mode: "resource", Source: &platform.PageResourceSource{Kind: "plan", Query: "private"}},
		"hasPrivate":    {Scope: "page", Type: "boolean", Mode: "derived", Expression: &platform.PageExpression{Op: "present", Args: []platform.PageValue{{Variable: "privateWindow"}}}},
	}, Queries: plans}

	submit(build.PageType, "page", "create", map[string]any{"name": "work", "title": "Work", "object": "build.note", "sections": []build.Section{{ID: "text", Widget: "text", ConfigVersion: 1, Text: "Read window"}, {ID: "private", Widget: "text", ConfigVersion: 1, Text: "Private window"}}, "document": document})
	submit(build.PageType, "page", "publish", map[string]any{})
	submit(build.AppType, "app", "create", map[string]any{"name": "desk", "title": "Desk", "pages": []string{"work"}, "uiProfile": platform.PageUIProfile(), "queries": plans, "variables": map[string]platform.PageVariable{"window": {Scope: "application", Type: "object-set", Mode: "resource", Source: &platform.PageResourceSource{Kind: "plan", Query: "read"}}, "privateWindow": {Scope: "application", Type: "object-set", Mode: "resource", Source: &platform.PageResourceSource{Kind: "plan", Query: "private"}}}})
	preview, err := tn.PreviewRelease(member, platform.AssetApp, "app")
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("preview %+v %v", preview, err)
	}
	if _, err = tn.SaveReleaseCandidate(member, platform.AssetApp, "app", preview.CandidateID, "save", at); err != nil {
		t.Fatal(err)
	}
	submit(build.AppType, "app", "edit", map[string]any{"queries": map[string]platform.PageQuery{}, "variables": map[string]platform.PageVariable{}})
	changed := *document
	changed.Queries = map[string]platform.PageQuery{}
	for id, q := range plans {
		changed.Queries[id] = q
	}
	q := changed.Queries["read"]
	q.Set = &platform.PageQuerySet{Op: "subtract", Inputs: []string{"left", "right"}}
	changed.Queries["read"] = q
	submit(build.PageType, "page", "edit", map[string]any{"document": &changed})

	if _, err = tn.ActivateRelease(member, preview.CandidateID, "activate", at); err != nil {
		t.Fatal(err)
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
	for _, current := range []*Tenant{tn, restored} {
		reader, _ := current.Member("reader")
		foundPage, foundApp := false, false
		for _, d := range current.Definitions(reader) {
			var queries map[string]platform.PageQuery
			if d.Page != nil && d.Ref.Name == "work" {
				foundPage = true
				queries = d.Page.Document.Queries
				if len(d.Page.Sections) != 1 || d.Page.Document.Variables["privateWindow"].Mode != "" {
					t.Fatal("private page set survived")
				}
			}
			if d.Application != nil && d.Ref.Name == "desk" {
				foundApp = true
				queries = d.Application.Queries
				if d.Application.Variables["privateWindow"].Mode != "" {
					t.Fatal("private app set survived")
				}
			}
			if queries != nil {
				if queries["read"].Set == nil || queries["read"].Set.Op != "union" || queries["left"].Query.SourceVersion != "1.query-1" {
					t.Fatal("frozen graph/version changed")
				}
				if _, ok := queries["private"]; ok {
					t.Fatal("set survived missing hidden operand")
				}
			}
		}
		if !foundPage || !foundApp {
			t.Fatal("page/application graph missing")
		}
	}
}
