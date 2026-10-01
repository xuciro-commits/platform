package platformserver

import (
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/platform"
)

func TestLoopRecordBindingUsesQueryObjectType(t *testing.T) {
	const tenant = "typed-loop"
	tn, err := NewTenant(tenant, NewConsole(tenant, Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder}}}), build.New(tenant))
	if err != nil {
		t.Fatal(err)
	}
	member, _ := tn.Member("builder")
	for _, name := range []string{"one", "two"} {
		for _, verb := range []string{"create", "publish"} {
			payload := map[string]any{}
			if verb == "create" {
				payload = map[string]any{"name": name, "title": name, "fields": []build.Field{{Name: "note", Title: "Note", Type: "text"}}}
			}
			_, problem := tn.Submit(member, &pb.Submission{TenantId: tenant, PrincipalId: member.ID, Authority: build.ID, IdempotencyKey: name + verb, Target: &pb.EntityRef{Type: build.ObjectType, Id: name}, Schema: &pb.SchemaRef{Name: build.ObjectType + "." + verb, Version: 1}, Payload: platform.Raw(payload)}, time.Now().UTC())
			if problem != nil {
				t.Fatal(problem.Message)
			}
		}
	}
	object := func(name string) platform.AssetRef {
		return platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: "build." + name}
	}
	page := platform.Page{Name: "loop", Object: object("one"), Layout: "composed", Sections: []platform.Section{{ID: "source", Widget: "table", ConfigVersion: 1, Fields: []string{"note"}}, {ID: "body", Widget: "detail", ConfigVersion: 1, Object: object("two"), RecordVariable: "item", Fields: []string{"note"}}}, Document: &platform.PageDocument{FormatVersion: 2, UIProfile: platform.PageUIProfile(), Root: "root", Nodes: map[string]platform.PageLayoutNode{"root": {Kind: "rows", Children: []string{"source", "loop"}}, "source": {Kind: "widget", Section: "source"}, "loop": {Kind: "loop", Children: []string{"body"}, Loop: &platform.PageLoop{Collection: "window", ItemVariable: "item", Limit: 20}}, "body": {Kind: "widget", Section: "body"}}, Variables: map[string]platform.PageVariable{"window": {Scope: "page", Type: "object-set", Mode: "resource", Source: &platform.PageResourceSource{Kind: "query", Section: "source"}}, "item": {Scope: "loop-item", Owner: "loop", Type: "record", Mode: "resource", Source: &platform.PageResourceSource{Kind: "item", Node: "loop"}}}}}
	if err := tn.InstallPage(tn.app(build.ID), page); err == nil || !strings.Contains(err.Error(), "record type") {
		t.Fatal(err)
	}
	page.Sections[1].Object = object("one")
	if err := tn.InstallPage(tn.app(build.ID), page); err != nil {
		t.Fatal(err)
	}
}
