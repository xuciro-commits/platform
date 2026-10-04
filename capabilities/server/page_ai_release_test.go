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

func TestThreeAIViewsRetainFrozenConfigurationAndFunctionDependencies(t *testing.T) {
	compose := func() *Tenant {
		tn, err := NewTenant("ai-views", NewConsole("ai-views", Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder}}}, Seat{Subjects: []string{"reader"}, Member: platform.Member{ID: "reader", Roles: map[string]string{build.ID: build.User}}}), build.New("ai-views"))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := compose()
	builder, _ := tn.Member("builder")
	reader, _ := tn.Member("reader")
	at := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	var entries []Entry
	key := 0
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	submit := func(typ, id, verb string, payload any) {
		t.Helper()
		key++
		_, err := tn.Submit(builder, &pb.Submission{TenantId: tn.ID, PrincipalId: builder.ID, Authority: build.ID, IdempotencyKey: fmt.Sprint(key), Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: typ + "." + verb, Version: 1}, Payload: platform.Raw(payload)}, at)
		if err != nil {
			t.Fatal(typ, id, verb, err.Message)
		}
	}
	submit(build.ObjectType, "O", "create", map[string]any{"name": "note", "title": "Notes", "fields": []build.Field{{Name: "name", Title: "Name", Type: "text"}}})
	submit(build.ObjectType, "O", "publish", map[string]any{})
	for _, name := range []string{"analysis", "chat"} {
		submit(build.FunctionType, name, "create", map[string]any{"name": name, "title": name, "description": "Authorized record analysis", "object": "build.note", "fields": []string{"name"}, "instructions": "Read the authorized note", "conversation": name == "chat", "output": []platform.Field{{Name: "summary", Type: "string", Required: true, Description: "Typed summary"}}, "roles": []string{build.Builder, build.User}, "maxInputBytes": 8192, "maxOutputBytes": 1024, "maxTokens": 256})
		submit(build.FunctionType, name, "publish", map[string]any{})
	}
	sections := []build.Section{{ID: "table", Widget: "table", ConfigVersion: 1, Fields: []string{"name"}}}
	doc := platform.PageDocument{FormatVersion: 2, UIProfile: platform.PageUIProfile(), Root: "root", Nodes: map[string]platform.PageLayoutNode{"root": {Kind: "rows", Children: []string{"table"}}, "table": {Kind: "widget", Section: "table"}}, Variables: map[string]platform.PageVariable{"record": {Scope: "page", Type: "record", Mode: "resource", Source: &platform.PageResourceSource{Kind: "record", Section: "table"}}, "question": {Scope: "page", Type: "string", Mode: "state", Initial: platform.Raw("")}}}
	for _, kind := range []string{"analyst", "generated", "chatbot"} {
		name := "analysis"
		config := &platform.PageAI{Kind: kind}
		if kind == "chatbot" {
			name = "chat"
			config.QuestionVariable = "question"
			config.ReplyField = "summary"
			config.Suggestions = []string{"Read this note"}
		}
		sections = append(sections, build.Section{ID: kind, Widget: "ai-assistant", ConfigVersion: 1, RecordVariable: "record", Function: &platform.FunctionRef{Name: name, Version: 1}, AI: config})
		doc.Nodes[kind] = platform.PageLayoutNode{Kind: "widget", Section: kind}
		n := doc.Nodes[doc.Root]
		n.Children = append(n.Children, kind)
		doc.Nodes[doc.Root] = n
	}
	submit(build.PageType, "P", "create", map[string]any{"name": "views", "title": "AI views", "object": "build.note", "sections": sections, "document": doc})
	submit(build.PageType, "P", "publish", map[string]any{})
	preview, err := tn.PreviewRelease(builder, platform.AssetPage, "P")
	if err != nil || preview.Diagnostic != "" {
		t.Fatal(preview, err)
	}
	saved, err := tn.SaveReleaseCandidate(builder, platform.AssetPage, "P", preview.CandidateID, "save", at)
	if err != nil {
		t.Fatal(err)
	}
	sections[3].AI.Suggestions = []string{"Later draft"}
	submit(build.PageType, "P", "edit", map[string]any{"sections": sections})
	check := func(current *Tenant) {
		t.Helper()
		candidate, err := platform.ReadCandidate(saved, current.releaseCandidates[saved])
		if err != nil {
			t.Fatal(err)
		}
		functions := 0
		var frozen platform.Page
		for _, a := range candidate.Assets {
			if a.Ref.Kind == platform.AssetFunction {
				functions++
			}
			if a.Ref.Kind == platform.AssetPage {
				if err := json.Unmarshal(a.Body, &frozen); err != nil {
					t.Fatal(err)
				}
			}
		}
		if functions != 2 || len(frozen.Sections) != 4 || frozen.Sections[3].AI.Suggestions[0] != "Read this note" {
			t.Fatal("candidate lost original AI configuration")
		}
		found := false
		for _, d := range current.Definitions(reader) {
			if d.Ref.Name == "views" && d.Page != nil {
				found = true
				if d.Page.Sections[3].AI.QuestionVariable != "question" || d.Page.Sections[3].AI.Suggestions[0] != "Read this note" {
					t.Fatal("draft changed published AI")
				}
			}
		}
		if !found {
			t.Fatal("AI page unavailable to reader")
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
