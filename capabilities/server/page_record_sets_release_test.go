package platformserver

import (
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/platform"
	"testing"
	"time"
)

func TestFrozenRecordSetPortsAndRecovery(t *testing.T) {
	compose := func() *Tenant {
		tn, err := NewTenant("record-sets", NewConsole("record-sets", Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder}}}, Seat{Subjects: []string{"reader"}, Member: platform.Member{ID: "reader", Roles: map[string]string{build.ID: build.User}}}), build.New("record-sets"))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := compose()
	builder, _ := tn.Member("builder")
	reader, _ := tn.Member("reader")
	var entries []Entry
	key := 0
	at := time.Now().UTC()
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	submit := func(typ, id, verb string, payload any) {
		t.Helper()
		key++
		_, err := tn.Submit(builder, &pb.Submission{TenantId: tn.ID, PrincipalId: builder.ID, Authority: build.ID, IdempotencyKey: fmt.Sprint(key), Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: typ + "." + verb, Version: 1}, Payload: platform.Raw(payload)}, at)
		if err != nil {
			t.Fatal(err)
		}
	}
	submit(build.ObjectType, "O", "create", map[string]any{"name": "note", "title": "Notes", "fields": []build.Field{{Name: "name", Title: "Name", Type: "text"}, {Name: "secret", Title: "Secret", Type: "text", Read: []string{build.Builder}}}})
	submit(build.ObjectType, "O", "publish", map[string]any{})
	doc := platform.PageDocument{FormatVersion: 2, UIProfile: platform.PageUIProfile(), Root: "root", Nodes: map[string]platform.PageLayoutNode{"root": {Kind: "rows", Children: []string{"table"}}, "table": {Kind: "widget", Section: "table"}}, Variables: map[string]platform.PageVariable{"picked": {Scope: "page", Type: "record-set", Mode: "resource", Source: &platform.PageResourceSource{Kind: "records", Section: "table"}}}}
	doc.Variables["shown"] = platform.PageVariable{Scope: "page", Type: "boolean", Mode: "state", Initial: platform.Raw(false)}
	doc.Events = []platform.PageEventBinding{{Source: "table", Event: "select", Target: "shown", Value: platform.Raw(true)}}
	sections := []build.Section{{ID: "table", Widget: "table", ConfigVersion: 1, Fields: []string{"name", "secret"}, SelectionSetVariable: "picked"}}
	submit(build.PageType, "P", "create", map[string]any{"name": "notes", "title": "Notes", "object": "build.note", "document": doc, "sections": sections})
	preview, err := tn.PreviewRelease(builder, platform.AssetPage, "P")
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("preview %+v %v", preview, err)
	}
	if _, err = tn.SaveReleaseCandidate(builder, platform.AssetPage, "P", preview.CandidateID, "save", at); err != nil {
		t.Fatal(err)
	}
	sections[0].SelectionSetVariable = ""
	doc.Events = nil
	submit(build.PageType, "P", "edit", map[string]any{"sections": sections, "document": doc})
	if _, err = tn.ActivateRelease(builder, preview.CandidateID, "activate", at); err != nil {
		t.Fatal(err)
	}
	check := func(current *Tenant) {
		t.Helper()
		for _, member := range []platform.Member{builder, reader} {
			seen := false
			for _, d := range current.Definitions(member) {
				if d.Page != nil && d.Ref.Name == "notes" {
					seen = true
					if len(d.Page.Document.Events) != 1 || d.Page.Document.Events[0].Event != "select" || d.Page.Document.Events[0].Target != "shown" {
						t.Fatal("frozen selection event changed")
					}
					if d.Page.Sections[0].SelectionSetVariable != "picked" || d.Page.Document.Variables["picked"].Source.Kind != "records" {
						t.Fatal("frozen record-set port changed")
					}
					if member.ID == reader.ID && len(d.Page.Sections[0].Fields) != 1 {
						t.Fatal("member fields were not masked")
					}
				}
			}
			if !seen {
				t.Fatal("record-set page missing")
			}
		}
	}
	check(tn)
	CheckReplay(t, tn, entries, compose)
	image, _, err := tn.Snapshot(func() int64 { return 0 })
	if err != nil {
		t.Fatal(err)
	}
	restored := compose()
	if err = restored.Restore(image); err != nil {
		t.Fatal(err)
	}
	check(restored)
}
