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

// A plant builder publishes a page over real shop orders, freezes and
// activates that release; the operator opens it, and a later draft neither
// moves the active release nor changes what the operator is offered.
func TestPlantReleaseActivatedForOperator(t *testing.T) {
	seats := []platformserver.Seat{
		{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder",
			Roles: map[string]string{build.ID: build.Builder, mes.ID: string(mes.Supervisor)}}},
		{Subjects: []string{"operator"}, Member: platform.Member{ID: "operator",
			Roles: map[string]string{build.ID: build.User, mes.ID: string(mes.Supervisor)}}},
	}
	tenant, err := NewTenant("release-plant-active", erp.New("release-plant-active"), seats...)
	if err != nil {
		t.Fatal(err)
	}
	builder, _ := tenant.Member("builder")
	operator, _ := tenant.Member("operator")
	at := time.Date(2026, 9, 29, 9, 0, 0, 0, time.UTC)
	submit := func(key, schema string, payload any) {
		t.Helper()
		raw, _ := json.Marshal(payload)
		if _, err := tenant.Submit(builder, &pb.Submission{TenantId: tenant.ID, PrincipalId: builder.ID,
			Authority: build.ID, IdempotencyKey: key, Target: &pb.EntityRef{Type: build.PageType, Id: "P1"},
			Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, at); err != nil {
			t.Fatalf("%s: %s %s", schema, err.Code, err.Message)
		}
	}
	submit("create", build.PageType+".create", map[string]any{
		"name": "productiondesk", "title": "Production desk", "object": mes.OrderType,
		"list": []string{"product", "quantity", "status"}, "detail": []string{"product", "quantity", "status"},
		"actions": []string{mes.SchemaRelease}})
	submit("publish", build.SchemaRelease, map[string]any{})
	preview, err := tenant.PreviewRelease(builder, platform.AssetPage, "P1")
	if err != nil || preview.Diagnostic != "" || preview.CandidateID == "" {
		t.Fatalf("preview: %+v, %v", preview, err)
	}
	if _, err := tenant.SaveReleaseCandidate(builder, platform.AssetPage, "P1", preview.CandidateID, "s1", at); err != nil {
		t.Fatal(err)
	}
	if _, err := tenant.ActivateRelease(operator, preview.CandidateID, "a0", at); err == nil {
		t.Fatal("an operator moved the release pointer")
	}
	if id, err := tenant.ActivateRelease(builder, preview.CandidateID, "a1", at); err != nil || id != preview.CandidateID {
		t.Fatalf("activate: %s, %v", id, err)
	}
	offered := func() *platform.Page {
		for _, d := range tenant.Definitions(operator) {
			if d.Ref == (platform.AssetRef{App: build.ID, Kind: platform.AssetPage, Name: "productiondesk"}) {
				return d.Page
			}
		}
		return nil
	}
	if p := offered(); p == nil || p.Title != "Production desk" {
		t.Fatalf("operator is not offered the active page: %+v", p)
	}
	submit("edit", build.PageType+".edit", map[string]any{"title": "Draft desk"})
	if tenant.ActiveRelease() != preview.CandidateID || offered().Title != "Production desk" {
		t.Fatal("a saved draft changed the active release or the operator's page")
	}
}
