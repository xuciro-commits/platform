package platformserver

import (
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/platform"
	"testing"
	"time"
)

func TestApplicationRecordFreezeBindingsProjectionAndRecovery(t *testing.T) {
	for _, kind := range []string{"record", "record-set"} {
		t.Run(kind, func(t *testing.T) {
			const tenant = "application-record-state"
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
				_, err := tn.Submit(member, &pb.Submission{TenantId: tenant, PrincipalId: member.ID, Authority: build.ID, IdempotencyKey: fmt.Sprint(key), Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: typ + "." + verb, Version: 1}, Payload: platform.Raw(payload)}, at)
				if err != nil {
					t.Fatal(typ, verb, err)
				}
			}
			submit(build.ObjectType, "object", "create", map[string]any{"name": "note", "title": "Note", "access": []build.Access{{Role: build.User, Read: "all"}}, "fields": []build.Field{{Name: "bucket", Title: "Bucket", Type: "text"}, {Name: "secret", Title: "Secret", Type: "text", Read: []string{build.Builder}}}})
			submit(build.ObjectType, "object", "publish", map[string]any{})
			object := platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: "build.note"}
			doc := &platform.PageDocument{FormatVersion: 2, UIProfile: platform.PageUIProfile(), Root: "root", Nodes: map[string]platform.PageLayoutNode{"root": {Kind: "rows", Children: []string{"table"}}, "table": {Kind: "widget", Section: "table"}}, Variables: map[string]platform.PageVariable{"selected": {Scope: "application", Type: kind, Mode: "shared", Writable: true, Source: &platform.PageResourceSource{Kind: "application", Variable: "selected", Object: &object}}}}
			for _, id := range []string{"first", "second"} {
				submit(build.PageType, id, "create", map[string]any{"name": id, "title": id, "object": object.Name, "sections": func() []build.Section {
					s := build.Section{ID: "table", Widget: "table", ConfigVersion: 1, Fields: []string{"bucket"}}
					if kind == "record" {
						s.SelectionVariable = "selected"
					} else {
						s.SelectionSetVariable = "selected"
					}
					return []build.Section{s}
				}(), "document": doc})
				submit(build.PageType, id, "publish", map[string]any{})
			}
			variables := map[string]platform.PageVariable{"selected": {Scope: "application", Type: kind, Mode: "resource", Source: &platform.PageResourceSource{Kind: kind, Object: &object}}}
			submit(build.AppType, "app", "create", map[string]any{"name": "notes", "title": "Notes", "pages": []string{"first", "second"}, "uiProfile": platform.PageUIProfile(), "variables": variables})
			preview, err := tn.PreviewRelease(member, platform.AssetApp, "app")
			if err != nil || preview.Diagnostic != "" {
				t.Fatalf("preview %+v %v", preview, err)
			}
			if _, err = tn.SaveReleaseCandidate(member, platform.AssetApp, "app", preview.CandidateID, "freeze", at); err != nil {
				t.Fatal(err)
			}
			submit(build.AppType, "app", "edit", map[string]any{"variables": map[string]platform.PageVariable{}})
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
					if d.Application != nil && d.Ref.Name == "notes" {
						if d.Application.Variables["selected"].Source.Object == nil || *d.Application.Variables["selected"].Source.Object != object {
							t.Fatal("frozen app record drifted")
						}
					}
				}
			}
			// Unreadable object identities cannot leak through an application record or its dependencies.
			submit(build.ObjectType, "private-object", "create", map[string]any{"name": "private", "title": "Private", "access": []build.Access{{Role: build.User, Read: "none"}}, "fields": []build.Field{{Name: "note", Title: "Note", Type: "text"}}})
			submit(build.ObjectType, "private-object", "publish", map[string]any{})
			privateObject := platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: "build.private"}
			private := platform.Application{Name: "private", Title: "Private", Pages: []string{"first"}, UIProfile: platform.PageUIProfile(), Variables: map[string]platform.PageVariable{"selected": variables["selected"], "private": {Scope: "application", Type: kind, Mode: "resource", Source: &platform.PageResourceSource{Kind: kind, Object: &privateObject}}, "present": {Scope: "application", Type: "boolean", Mode: "derived", Expression: &platform.PageExpression{Op: "present", Args: []platform.PageValue{{Variable: "private"}}}}}}
			if kind == "record-set" {
				delete(private.Variables, "present")
			}
			if err := tn.InstallApplication(tn.app(build.ID), private); err != nil {
				t.Fatal(err)
			}

			privateDoc := &platform.PageDocument{FormatVersion: 2, UIProfile: platform.PageUIProfile(), Root: "root", Nodes: map[string]platform.PageLayoutNode{"root": {Kind: "rows", Children: []string{"table", "notice"}}, "table": {Kind: "widget", Section: "table"}, "notice": {Kind: "widget", Section: "notice", VisibleWhen: "hasPrivate"}}, Variables: map[string]platform.PageVariable{"private": {Scope: "application", Type: kind, Mode: "shared", Source: &platform.PageResourceSource{Kind: "application", Variable: "private", Object: &privateObject}}, "hasPrivate": {Scope: "page", Type: "boolean", Mode: "derived", Expression: &platform.PageExpression{Op: "present", Args: []platform.PageValue{{Variable: "private"}}}}}}
			privateSections := []build.Section{{ID: "table", Widget: "table", ConfigVersion: 1, Fields: []string{"bucket"}}, {ID: "notice", Widget: "text", ConfigVersion: 1, Text: "Private object selected"}}
			if kind == "record-set" {
				delete(privateDoc.Variables, "hasPrivate")
				privateDoc.Nodes["notice"] = platform.PageLayoutNode{Kind: "widget", Section: "notice"}
				privateSections[1] = build.Section{ID: "notice", Widget: "record-comparison", ConfigVersion: 1, Object: privateObject.Name, RecordSetVariable: "private", RecordComparison: &platform.PageRecordComparison{LabelField: "id"}, Fields: []string{"note"}}
			}
			submit(build.PageType, "private-page", "create", map[string]any{"name": "privateview", "title": "Private view", "object": object.Name, "sections": privateSections, "document": privateDoc})
			submit(build.PageType, "private-page", "publish", map[string]any{})
			reader, _ := tn.Member("reader")
			for _, d := range tn.Definitions(reader) {
				if d.Page != nil && d.Ref.Name == "privateview" {
					if len(d.Page.Document.Variables) != 0 || len(d.Page.Sections) != 1 {
						t.Fatal("private shared page record or dependency exposed")
					}
				}
				if d.Application != nil && d.Ref.Name == "private" {
					if len(d.Application.Variables) != 1 || d.Application.Variables["selected"].Source == nil {
						t.Fatal("private app record metadata exposed")
					}
					for _, ref := range d.Requires {
						if ref == privateObject {
							t.Fatal("private object dependency exposed")
						}
					}
				}
			}
		})
	}
}
