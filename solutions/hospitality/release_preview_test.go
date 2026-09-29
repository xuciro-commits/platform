package hospitality

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"platformserver"
	"platformserver/apps/build"
	"platformserver/platform"
)

func TestHotelBuilderPreviewIsReadOnlyAndRestricted(t *testing.T) {
	w := newWorld(t, hotelProvider)
	w.expect(w.submit("manager", build.ID, build.ObjectType+".create", build.ObjectType, "O-PREVIEW", "preview-create",
		map[string]any{"name": "visit", "title": "Visit", "fields": []map[string]any{{"name": "guest", "title": "Guest", "type": "text"}}}), "ok")

	preview, err := w.tenant.PreviewRelease(w.members["manager"], platform.AssetObject, "O-PREVIEW")
	if err != nil || preview.Diagnostic != "" || preview.CurrentID != "" || preview.CandidateID == "" || len(preview.Added) < 3 {
		t.Fatalf("new draft candidate: %+v, %v", preview, err)
	}
	if _, err := w.tenant.PreviewRelease(w.members["desk"], platform.AssetObject, "O-PREVIEW"); err == nil {
		t.Fatal("a non-builder obtained an unfiltered release candidate")
	}
	// A read-only preview cannot install a draft or change a member's catalog.
	installedPage := platform.AssetRef{App: build.ID, Kind: platform.AssetPage, Name: "visit"}
	if _, err := w.tenant.ReleaseCandidate([]platform.AssetRef{installedPage}); err == nil {
		t.Fatal("draft became an installed definition")
	}
	w.expect(w.submit("manager", build.ID, build.SchemaPublish, build.ObjectType, "O-PREVIEW", "preview-publish", map[string]any{}), "ok")
	published, err := w.tenant.PreviewRelease(w.members["manager"], platform.AssetObject, "O-PREVIEW")
	if err != nil || published.Diagnostic != "" || preview.CandidateID != published.CurrentID || published.CurrentID != published.CandidateID {
		t.Fatalf("preview differs from the accepted publication: %v, %+v / %+v", err, preview, published)
	}
	installed, err := w.tenant.ReleaseCandidate([]platform.AssetRef{installedPage})
	if err != nil {
		t.Fatal(err)
	}

	w.expect(w.submit("manager", build.ID, build.ObjectType+".edit", build.ObjectType, "O-PREVIEW", "preview-edit",
		map[string]any{"title": "Guest visit"}), "ok")
	preview, err = w.tenant.PreviewRelease(w.members["manager"], platform.AssetObject, "O-PREVIEW")
	if err != nil || preview.Diagnostic != "" || preview.CurrentID != published.CurrentID || preview.CandidateID == published.CurrentID ||
		len(preview.Changed) == 0 {
		t.Fatalf("edited draft comparison: %+v, %v", preview, err)
	}
	stillPublished, err := w.tenant.ReleaseCandidate([]platform.AssetRef{installedPage})
	if err != nil || stillPublished.ID != installed.ID {
		t.Fatalf("preview overwrote the installed release: %v, %s / %s", err, stillPublished.ID, installed.ID)
	}

	handler := platformserver.NewHost(platformserver.Tokens(map[string]string{"manager-token": "manager", "desk-token": "desk"}), w.tenant).Handler()
	request := func(token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/v1/releases/preview", strings.NewReader(`{"kind":"object","id":"O-PREVIEW"}`))
		r.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		return rec
	}
	if rec := request("desk-token"); rec.Code != http.StatusForbidden || rec.Body.Len() != 0 {
		t.Fatalf("unprivileged preview disclosed %d: %s", rec.Code, rec.Body.String())
	}
	rec := request("manager-token")
	var response platformserver.ReleasePreview
	if err := json.Unmarshal(rec.Body.Bytes(), &response); rec.Code != http.StatusOK || err != nil || response.CandidateID != preview.CandidateID {
		t.Fatalf("builder HTTP preview: %d %s, %v", rec.Code, rec.Body.String(), err)
	}
	w.expect(w.submit("manager", build.ID, build.AppType+".create", build.AppType, "A-PREVIEW", "preview-app",
		map[string]any{"name": "hotelapp", "title": "Hotel app", "pages": []string{"visit"}}), "ok")
	w.expect(w.submit("manager", build.ID, build.SchemaHandOver, build.AppType, "A-PREVIEW", "preview-app-publish", map[string]any{}), "ok")
	w.expect(w.submit("manager", build.ID, build.ObjectType+".edit", build.ObjectType, "O-PREVIEW", "preview-rename",
		map[string]any{"name": "renamed"}), "ok")
	renamed, err := w.tenant.PreviewRelease(w.members["manager"], platform.AssetObject, "O-PREVIEW")
	if err != nil || !strings.Contains(renamed.Diagnostic, "another type is already this Go type") || renamed.CandidateID != "" {
		t.Fatalf("incompatible rename was not rejected before affecting an installed application: %+v, %v", renamed, err)
	}
}

func TestHotelObjectPreviewRejectsAnInstalledPageWithRemovedField(t *testing.T) {
	w := newWorld(t, hotelProvider)
	w.expect(w.submit("manager", build.ID, build.ObjectType+".create", build.ObjectType, "O-CLOSURE", "closure-object",
		map[string]any{"name": "delivery", "title": "Delivery", "fields": []map[string]any{
			{"name": "guest", "title": "Guest", "type": "text"}, {"name": "room", "title": "Room", "type": "text"},
		}}), "ok")
	w.expect(w.submit("manager", build.ID, build.SchemaPublish, build.ObjectType, "O-CLOSURE", "closure-publish", map[string]any{}), "ok")
	w.expect(w.submit("manager", build.ID, build.PageType+".create", build.PageType, "P-CLOSURE", "closure-page",
		map[string]any{"name": "deliverydesk", "title": "Delivery desk", "object": "build.delivery",
			"list": []string{"guest"}, "detail": []string{"room"}}), "ok")
	w.expect(w.submit("manager", build.ID, build.SchemaRelease, build.PageType, "P-CLOSURE", "closure-page-publish", map[string]any{}), "ok")
	w.expect(w.submit("manager", build.ID, build.ObjectType+".edit", build.ObjectType, "O-CLOSURE", "closure-remove-room",
		map[string]any{"fields": []map[string]any{{"name": "guest", "title": "Guest", "type": "text"}}}), "ok")
	preview, err := w.tenant.PreviewRelease(w.members["manager"], platform.AssetObject, "O-CLOSURE")
	if err != nil || preview.CandidateID != "" || preview.CurrentID == "" ||
		!strings.Contains(preview.Diagnostic, "page deliverydesk: build.delivery has no field room") {
		t.Fatalf("a dependent page lost its bound field without diagnosis: %+v, %v", preview, err)
	}
	ref := platform.AssetRef{App: build.ID, Kind: platform.AssetPage, Name: "deliverydesk"}
	before, err := w.tenant.ReleaseCandidate([]platform.AssetRef{ref})
	if err != nil {
		t.Fatal(err)
	}
	if outcome := w.submit("manager", build.ID, build.SchemaPublish, build.ObjectType, "O-CLOSURE",
		"closure-bad-publish", map[string]any{}); outcome != "ERROR_CODE_INVALID_ARGUMENT" {
		t.Fatalf("actual publication bypassed the preview rejection: %s", outcome)
	}
	if live, err := w.tenant.ReleaseCandidate([]platform.AssetRef{ref}); err != nil || live.ID != before.ID {
		t.Fatalf("rejected publication changed the installed page: %+v, %v", live, err)
	}
}

func TestHotelObjectPreviewChecksExplicitPageOverGeneratedName(t *testing.T) {
	w := newWorld(t, hotelProvider)
	w.expect(w.submit("manager", build.ID, build.ObjectType+".create", build.ObjectType, "O-SAME", "same-object",
		map[string]any{"name": "pickup", "title": "Pickup", "fields": []map[string]any{
			{"name": "guest", "title": "Guest", "type": "text"}, {"name": "room", "title": "Room", "type": "text"},
		}}), "ok")
	w.expect(w.submit("manager", build.ID, build.SchemaPublish, build.ObjectType, "O-SAME", "same-object-publish", map[string]any{}), "ok")
	w.expect(w.submit("manager", build.ID, build.PageType+".create", build.PageType, "P-SAME", "same-page",
		map[string]any{"name": "pickup", "title": "Pickup desk", "object": "build.pickup",
			"list": []string{"guest"}, "detail": []string{"room"}}), "ok")
	w.expect(w.submit("manager", build.ID, build.SchemaRelease, build.PageType, "P-SAME", "same-page-publish", map[string]any{}), "ok")
	w.expect(w.submit("manager", build.ID, build.ObjectType+".edit", build.ObjectType, "O-SAME", "same-remove-room",
		map[string]any{"fields": []map[string]any{{"name": "guest", "title": "Guest", "type": "text"}}}), "ok")
	preview, err := w.tenant.PreviewRelease(w.members["manager"], platform.AssetObject, "O-SAME")
	if err != nil || preview.CandidateID != "" ||
		!strings.Contains(preview.Diagnostic, "page pickup: build.pickup has no field room") {
		t.Fatalf("explicit page over generated name escaped validation: %+v, %v", preview, err)
	}
	if outcome := w.submit("manager", build.ID, build.SchemaPublish, build.ObjectType, "O-SAME",
		"same-bad-publish", map[string]any{}); outcome != "ERROR_CODE_INVALID_ARGUMENT" {
		t.Fatalf("explicit page over generated name escaped publication validation: %s", outcome)
	}
}

func TestHotelAutomaticPageFollowsCompatibleObjectChange(t *testing.T) {
	w := newWorld(t, hotelProvider)
	w.expect(w.submit("manager", build.ID, build.ObjectType+".create", build.ObjectType, "O-AUTO", "auto-create",
		map[string]any{"name": "handoff", "title": "Handoff", "fields": []map[string]any{
			{"name": "guest", "title": "Guest", "type": "text"}, {"name": "optional", "title": "Optional", "type": "text"},
		}}), "ok")
	w.expect(w.submit("manager", build.ID, build.SchemaPublish, build.ObjectType, "O-AUTO", "auto-publish", map[string]any{}), "ok")
	w.expect(w.submit("manager", build.ID, build.ObjectType+".edit", build.ObjectType, "O-AUTO", "auto-edit",
		map[string]any{"fields": []map[string]any{{"name": "guest", "title": "Guest", "type": "text"}}}), "ok")
	preview, err := w.tenant.PreviewRelease(w.members["manager"], platform.AssetObject, "O-AUTO")
	if err != nil || preview.Diagnostic != "" || preview.CandidateID == "" {
		t.Fatalf("automatic page could not follow object: %+v, %v", preview, err)
	}
	w.expect(w.submit("manager", build.ID, build.SchemaPublish, build.ObjectType, "O-AUTO", "auto-republish", map[string]any{}), "ok")
	installed, err := w.tenant.ReleaseCandidate([]platform.AssetRef{{App: build.ID, Kind: platform.AssetPage, Name: "handoff"}})
	if err != nil || installed.ID == "" {
		t.Fatalf("updated automatic page is not installed: %+v, %v", installed, err)
	}
}

func TestHotelExplicitPageSurvivesObjectRepublish(t *testing.T) {
	w := newWorld(t, hotelProvider)
	w.expect(w.submit("manager", build.ID, build.ObjectType+".create", build.ObjectType, "O-EXPLICIT", "explicit-create",
		map[string]any{"name": "pickup", "title": "Pickup", "fields": []map[string]any{
			{"name": "guest", "title": "Guest", "type": "text"}, {"name": "room", "title": "Room", "type": "text"},
		}}), "ok")
	w.expect(w.submit("manager", build.ID, build.SchemaPublish, build.ObjectType, "O-EXPLICIT", "explicit-publish", map[string]any{}), "ok")
	w.expect(w.submit("manager", build.ID, build.PageType+".create", build.PageType, "P-EXPLICIT", "explicit-page",
		map[string]any{"name": "pickup", "title": "Pickup desk", "object": "build.pickup",
			"list": []string{"guest"}, "detail": []string{"room"}}), "ok")
	w.expect(w.submit("manager", build.ID, build.SchemaRelease, build.PageType, "P-EXPLICIT", "explicit-page-publish", map[string]any{}), "ok")
	w.expect(w.submit("manager", build.ID, build.ObjectType+".edit", build.ObjectType, "O-EXPLICIT", "explicit-edit",
		map[string]any{"title": "Pickup revised"}), "ok")
	w.expect(w.submit("manager", build.ID, build.SchemaPublish, build.ObjectType, "O-EXPLICIT", "explicit-republish", map[string]any{}), "ok")

	handler := platformserver.NewHost(platformserver.Tokens(map[string]string{"desk-token": "desk"}), w.tenant).Handler()
	request := httptest.NewRequest(http.MethodGet, "/v1/definitions", nil)
	request.Header.Set("Authorization", "Bearer desk-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, request)
	var definitions []platform.Definition
	if err := json.Unmarshal(rec.Body.Bytes(), &definitions); rec.Code != http.StatusOK || err != nil {
		t.Fatalf("operator could not read installed definitions: %d %s (%v)", rec.Code, rec.Body.String(), err)
	}
	for _, definition := range definitions {
		if definition.Ref == (platform.AssetRef{App: build.ID, Kind: platform.AssetPage, Name: "pickup"}) {
			if definition.Page == nil || definition.Page.Title != "Pickup desk" ||
				len(definition.Page.DetailFields) != 1 || definition.Page.DetailFields[0] != "room" {
				t.Fatalf("object republish overwrote the explicit operator page: %+v", definition)
			}
			return
		}
	}
	t.Fatal("operator lost the explicit page after object republish")
}
