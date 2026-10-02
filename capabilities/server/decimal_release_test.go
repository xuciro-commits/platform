package platformserver

import (
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/platform"
	"testing"
	"time"
)

func TestDecimalApplicationFreezeProjectionAndRecovery(t *testing.T) {
	const tenant = "decimal-application-state"
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

	submit(build.ObjectType, "object", "create", map[string]any{"name": "note", "title": "Note", "access": []build.Access{{Role: build.User, Read: "all"}}, "fields": []build.Field{{Name: "count", Title: "Count", Type: "integer"}}})
	submit(build.ObjectType, "object", "publish", map[string]any{})
	object := platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: "build.note"}
	decimal := platform.Raw(platform.DecimalValue{Kind: "decimal", Value: "1.9999999999999999"})
	doc := &platform.PageDocument{FormatVersion: 2, UIProfile: platform.PageUIProfile(), Root: "root", Nodes: map[string]platform.PageLayoutNode{"root": {Kind: "rows", Children: []string{"input"}}, "input": {Kind: "widget", Section: "input", ValueVariable: "threshold"}}, Variables: map[string]platform.PageVariable{"threshold": {Scope: "application", Type: "decimal", Mode: "shared", Writable: true, Source: &platform.PageResourceSource{Kind: "application", Variable: "threshold"}}}, Queries: map[string]platform.PageQuery{"read": {Object: object, Limit: 10, Conditions: []platform.PageQueryCondition{{Field: "count", Op: ">", Value: platform.PageValue{Variable: "threshold"}}}}}}
	submit(build.PageType, "page", "create", map[string]any{"name": "notes", "title": "Notes", "object": object.Name, "sections": []build.Section{{ID: "input", Widget: "input", ConfigVersion: 1}}, "document": doc})
	submit(build.PageType, "page", "publish", map[string]any{})
	submit(build.AppType, "app", "create", map[string]any{"name": "desk", "title": "Desk", "pages": []string{"notes"}, "uiProfile": platform.PageUIProfile(), "variables": map[string]platform.PageVariable{"threshold": {Scope: "application", Type: "decimal", Mode: "state", Initial: decimal}}})
	preview, err := tn.PreviewRelease(member, platform.AssetApp, "app")
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("preview %+v %v", preview, err)
	}
	if _, err = tn.SaveReleaseCandidate(member, platform.AssetApp, "app", preview.CandidateID, "freeze", at); err != nil {
		t.Fatal(err)
	}
	submit(build.AppType, "app", "edit", map[string]any{"variables": map[string]platform.PageVariable{"threshold": {Scope: "application", Type: "decimal", Mode: "state", Initial: platform.Raw(platform.DecimalValue{Kind: "decimal", Value: "2"})}}})
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
			if d.Application != nil && d.Ref.Name == "desk" {
				value, ok := platform.DecimalLiteral(d.Application.Variables["threshold"].Initial)
				if !ok || value.Value != "1.9999999999999999" {
					t.Fatal("frozen decimal lost precision or drifted")
				}
			}
		}
	}
}
