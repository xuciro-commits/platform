package manufacturing

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"erp"
	"mes"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver"
	"platformserver/apps/build"
	"platformserver/platform"
)

func TestPlantCodePageReleaseCandidate(t *testing.T) {
	tenant, err := NewTenant("release-plant", erp.New("release-plant"))
	if err != nil {
		t.Fatal(err)
	}
	root := platform.AssetRef{App: mes.ID, Kind: platform.AssetPage, Name: "shop-orders"}
	candidate, err := tenant.ReleaseCandidate([]platform.AssetRef{root})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := platform.ReadCandidate(candidate.ID, candidate.Bytes); err != nil {
		t.Fatal(err)
	}
	for _, required := range []platform.AssetRef{
		{App: mes.ID, Kind: platform.AssetObject, Name: mes.OrderType},
		{App: mes.ID, Kind: platform.AssetAction, Name: mes.SchemaRelease},
	} {
		if !slices.ContainsFunc(candidate.Assets, func(a platform.ReleaseAsset) bool { return a.Ref == required }) {
			t.Errorf("plant release omits %s", required)
		}
	}
}

func TestPlantBuilderPreviewsPageOverRealShopOrders(t *testing.T) {
	seat := platformserver.Seat{Subjects: []string{"builder"}, Member: platform.Member{
		ID: "builder", Roles: map[string]string{build.ID: build.Builder, mes.ID: string(mes.Supervisor)}}}
	tenant, err := NewTenant("release-plant-builder", erp.New("release-plant-builder"), seat)
	if err != nil {
		t.Fatal(err)
	}
	member, _ := tenant.Member("builder")
	payload, _ := json.Marshal(map[string]any{
		"name": "productiondesk", "title": "Production desk", "object": mes.OrderType,
		"list": []string{"product", "quantity", "status"}, "detail": []string{"product", "quantity", "status"},
		"actions": []string{mes.SchemaRelease},
	})
	if _, err := tenant.Submit(member, &pb.Submission{TenantId: tenant.ID, PrincipalId: member.ID,
		Authority: build.ID, IdempotencyKey: "plant-preview-page",
		Target: &pb.EntityRef{Type: build.PageType, Id: "P1"},
		Schema: &pb.SchemaRef{Name: build.PageType + ".create", Version: 1}, Payload: payload},
		time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	preview, err := tenant.PreviewRelease(member, platform.AssetPage, "P1")
	if err != nil || preview.Diagnostic != "" || preview.CandidateID == "" || preview.CurrentID != "" {
		t.Fatalf("plant page preview: %+v, %v", preview, err)
	}
	for _, required := range []platform.AssetRef{
		{App: build.ID, Kind: platform.AssetPage, Name: "productiondesk"},
		{App: mes.ID, Kind: platform.AssetObject, Name: mes.OrderType},
		{App: mes.ID, Kind: platform.AssetAction, Name: mes.SchemaRelease},
	} {
		if !slices.Contains(preview.Added, required) {
			t.Errorf("plant builder candidate omits %s", required)
		}
	}
	if _, err := tenant.ReleaseCandidate([]platform.AssetRef{{App: build.ID, Kind: platform.AssetPage, Name: "productiondesk"}}); err == nil {
		t.Fatal("a manufacturing draft was installed by read-only preview")
	}
}
