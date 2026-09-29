package platformserver

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/build"
	"platformserver/platform"
)

func TestBuilderInventoryRestoresAssetsBeyondFirstReadPage(t *testing.T) {
	compose := func() *Tenant {
		tn, err := NewTenant("inventory", NewConsole("inventory", Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder}}}), build.New("inventory"))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := compose()
	c := platform.NewCaller(runtime{tn}, platform.Member{ID: "fixture", Tenant: tn.ID}, build.ID, true, true)
	put := func(typ, id string, value any) {
		t.Helper()
		change := &pb.ChangeRecord{ChangeId: typ + ":" + id, Revision: 1, Submission: &pb.Submission{Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: typ + ".create"}}}
		if err := tn.records.put(c, change, value); err != nil {
			t.Fatal(err)
		}
	}
	for index := 0; index <= 500; index++ {
		id := fmt.Sprintf("D%04d", index)
		object := build.Object{Record: platform.Record{ID: id}, Name: fmt.Sprintf("object%d", index), Title: "Source", Fields: []build.Field{{Name: "guest", Title: "Guest", Type: "text"}}, State: "draft"}
		page := build.Page{Record: platform.Record{ID: id}, Name: fmt.Sprintf("page%d", index), Title: "Sources", Object: "build.source", List: []string{"guest"}, Detail: []string{"guest"}, State: "draft"}
		app := build.Application{Record: platform.Record{ID: id}, Name: fmt.Sprintf("app%d", index), Title: "Source desk", Pages: []string{"sources"}, State: "draft"}
		if index == 500 {
			object.Name, object.State = "source", "published"
			page.Name, page.State = "sources", "published"
			app.Name, app.State = "sourcedesk", "published"
			raw, _ := json.Marshal(object)
			object.Published = string(raw)
			raw, _ = json.Marshal(page)
			page.Published = string(raw)
			raw, _ = json.Marshal(app)
			app.Published = string(raw)
		}
		put(build.ObjectType, id, object)
		put(build.PageType, id, page)
		put(build.AppType, id, app)
	}
	b := tn.app(build.ID).(*build.Build)
	if err := b.Reinstall(); err != nil {
		t.Fatal(err)
	}
	assets, err := b.ReleaseAssets()
	if err != nil {
		t.Fatal(err)
	}
	for _, wanted := range []platform.AssetRef{{App: build.ID, Kind: platform.AssetObject, Name: "build.source"}, {App: build.ID, Kind: platform.AssetPage, Name: "sources"}, {App: build.ID, Kind: platform.AssetApp, Name: "sourcedesk"}} {
		found := false
		for _, asset := range assets {
			if asset.Ref == wanted {
				found = true
			}
		}
		if !found {
			t.Fatalf("omitted asset after the first page: %s", wanted)
		}
	}
	saved, _, err := tn.Snapshot(func() int64 { return 0 })
	if err != nil {
		t.Fatal(err)
	}
	restored := compose()
	if err := restored.Restore(saved); err != nil {
		t.Fatal(err)
	}
	if snapshot(tn) != snapshot(restored) {
		t.Fatal("restoration lost definitions after the first page")
	}
	member, _ := restored.Member("builder")
	at := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	submit := func(schema, typ, id, key, payload string) *kernel.Error {
		_, err := restored.Submit(member, &pb.Submission{TenantId: restored.ID, PrincipalId: member.ID, Authority: build.ID, IdempotencyKey: key, Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: []byte(payload)}, at)
		return err
	}
	if err := submit("build.source.create", "build.source", "S", "source", `{"guest":"Ada"}`); err != nil {
		t.Fatalf("restored source was not installed: %v", err)
	}
	if err := submit(build.ObjectType+".create", build.ObjectType, "NEW", "duplicate", `{"name":"source","title":"Another source","fields":[{"name":"guest","title":"Guest","type":"text"}]}`); err == nil || !strings.Contains(err.Message, "already") {
		t.Fatalf("ignored duplicate object name after the first page: %v", err)
	}
	// The existing 1000-definition release limit must reject, rather than
	// produce a partial inventory that looks like a complete candidate.
	for index := 501; index <= 1000; index++ {
		id := fmt.Sprintf("D%04d", index)
		put(build.ObjectType, id, build.Object{Record: platform.Record{ID: id}, Name: fmt.Sprintf("object%d", index), Title: "Draft", State: "draft"})
	}
	if _, err := b.ReleaseAssets(); err == nil {
		t.Fatal("truncated an oversized release inventory")
	}
}
