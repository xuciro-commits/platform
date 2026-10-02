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
	search := false
	sections[0].ShowSearch = &search
	sections[0].TableColumns = []platform.PageTableColumn{{Field: "name", Title: "Frozen name", Width: 170, Formatter: "text"}, {Field: "secret", Title: "Private column", Width: 110}}
	doc.Nodes["detail"] = platform.PageLayoutNode{Kind: "widget", Section: "detail"}
	root := doc.Nodes[doc.Root]
	root.Children = append(root.Children, "detail")
	doc.Nodes[doc.Root] = root
	sections = append(sections, build.Section{ID: "detail", Widget: "detail", ConfigVersion: 1, Fields: []string{"name", "secret"}, DetailPresentation: &platform.PageDetailPresentation{Columns: 2, HideNull: true}})
	doc.Variables["active"] = platform.PageVariable{Scope: "page", Type: "record", Mode: "resource", Source: &platform.PageResourceSource{Kind: "record", Section: "table"}}
	doc.Nodes["view"] = platform.PageLayoutNode{Kind: "widget", Section: "view"}
	root = doc.Nodes[doc.Root]
	root.Children = append(root.Children, "view")
	doc.Nodes[doc.Root] = root
	sections = append(sections, build.Section{ID: "view", Widget: "record-view", ConfigVersion: 1, RecordVariable: "active", Fields: []string{"name", "secret"}, RecordView: &platform.PageRecordView{Tabs: []string{"overview", "properties", "links", "history"}}})
	submit(build.PageType, "P", "create", map[string]any{"name": "notes", "title": "Notes", "object": "build.note", "document": doc, "sections": sections})
	preview, err := tn.PreviewRelease(builder, platform.AssetPage, "P")
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("preview %+v %v", preview, err)
	}
	if _, err = tn.SaveReleaseCandidate(builder, platform.AssetPage, "P", preview.CandidateID, "save", at); err != nil {
		t.Fatal(err)
	}
	sections[0].SelectionSetVariable = ""
	sections[0].TableColumns = nil
	sections[0].ShowSearch = nil
	sections[1].DetailPresentation = nil
	sections[2].RecordView = &platform.PageRecordView{Tabs: []string{"properties"}}
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
					if len(d.Page.Sections[2].RecordView.Tabs) != 4 || d.Page.Sections[2].RecordVariable != "active" {
						t.Fatal("frozen record view changed")
					}
					if member.ID == reader.ID && len(d.Page.Sections[2].Fields) != 1 {
						t.Fatal("record view fields were not masked")
					}
					if d.Page.Sections[1].DetailPresentation == nil || !d.Page.Sections[1].DetailPresentation.HideNull || d.Page.Sections[1].DetailPresentation.Columns != 2 {
						t.Fatal("frozen detail presentation changed")
					}
					if member.ID == reader.ID && len(d.Page.Sections[1].Fields) != 1 {
						t.Fatal("detail fields were not masked")
					}
					if d.Page.Sections[0].ShowSearch == nil || *d.Page.Sections[0].ShowSearch || d.Page.Sections[0].TableColumns[0].Title != "Frozen name" {
						t.Fatal("frozen table presentation changed")
					}
					if member.ID == reader.ID && len(d.Page.Sections[0].TableColumns) != 1 {
						t.Fatal("hidden column presentation escaped member projection")
					}
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
