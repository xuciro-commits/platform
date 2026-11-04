package host

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"platformserver/platform"
	"platformserver/services/devops/packages"
	"platformserver/services/resources/authoring"
)

func invariantPublishData(t *testing.T, tenant *invariantTenant, member platform.Member, drafts []build.JointDraftRef, key string) string {
	t.Helper()
	preview, err := tenant.PreviewReleaseDrafts(member, drafts, "", invariantClock)
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("preview %s: %+v: %v", key, preview, err)
	}
	id, err := tenant.SaveReleaseCandidates(member, drafts, preview.CandidateID, key, invariantClock)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tenant.ActivateRelease(member, id, key, invariantClock); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestInvariantK1K2K4MediaProductDeliveryRetainsVersionsAndBytes(t *testing.T) {
	source := newInvariantTenant(t, "media-publisher")
	raw := []byte("immutable media bytes\n")
	hash := fmt.Sprintf("%x", sha256.Sum256(raw))
	if err := source.files().Put(context.Background(), source.ID+"/"+hash, raw, "text/plain"); err != nil {
		t.Fatal(err)
	}
	source.accept(t, platform.MediaSetType+".create", platform.MediaSetType, "PUBLISHER-MEDIA", map[string]any{"name": "documents", "title": "Documents"})
	source.accept(t, platform.SchemaMediaSetAdd, platform.MediaSetType, "PUBLISHER-MEDIA", map[string]any{"hash": hash, "name": "retained.txt", "size": len(raw), "contentType": "text/plain"})
	source.accept(t, platform.SchemaMediaSetRemove, platform.MediaSetType, "PUBLISHER-MEDIA", map[string]any{"hash": hash})
	source.accept(t, "build.app.create", build.AppType, "MEDIA-APP", map[string]any{"name": "mediaworkspace", "title": "Media workspace", "resources": []platform.AssetRef{{App: build.ID, Kind: platform.AssetMediaSet, Name: "documents"}}})
	id := invariantPublishData(t, source, source.member(t), []build.JointDraftRef{{Kind: platform.AssetMediaSet, ID: "PUBLISHER-MEDIA"}, {Kind: platform.AssetApp, ID: "MEDIA-APP"}}, "media-release")
	candidate, err := platform.ReadCandidate(id, source.releases.Raw(id))
	if err != nil {
		t.Fatal(err)
	}
	descriptor := platform.PackageDescriptor{ID: "media", Version: "1.0.0", Namespace: "media", Title: "Media", Blueprint: &platform.PackageBlueprint{Roots: []platform.AssetRef{{App: build.ID, Kind: platform.AssetApp, Name: "mediaworkspace"}}, Assets: candidate.Assets}}
	seat := platform.Seat{Subjects: []string{"admin"}, Member: platform.Member{ID: "admin", Roles: map[string]string{build.ID: build.Builder, "platform": "admin"}}}
	target := newInvariantTenant(t, "media-customer", seat)
	admin, _ := target.Member("admin")
	inspected, err := target.inspectPackage(context.Background(), platform.PackageInspectRequest{Content: platform.Raw(descriptor)})
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []struct {
		schema  string
		payload any
	}{
		{packages.SchemaPackageImport, map[string]any{"id": "media", "digest": inspected.Digest}},
		{packages.SchemaPackageInstall, map[string]any{"id": "media", "version": "1.0.0"}},
	} {
		submission := target.submission(action.schema, platform.PackageType, "media", action.payload)
		submission.Authority = "platform"
		submission.PrincipalId = admin.ID
		if _, refusal := target.Submit(admin, submission, invariantClock); refusal != nil {
			t.Fatalf("%s: %v", action.schema, refusal)
		}
	}
	sets, _, refusal := platform.Find[platform.MediaSet](target.automation(build.ID, false), platform.Query{})
	if refusal != nil || len(sets) != 1 {
		t.Fatalf("package did not install its media set: %v", refusal)
	}
	mediaID := sets[0].ID
	check := func(tenant *invariantTenant) {
		t.Helper()
		version, ok := platform.Get[platform.MediaSetVersion](tenant.automation(build.ID, false), platform.MediaSetVersionID(mediaID, 1))
		if !ok || version.MediaSet != mediaID || len(version.Items) != 1 || version.Items[0].Hash != hash {
			t.Fatalf("installed historical version absent: %+v", version)
		}
		for _, suffix := range []string{"", "?version=1"} {
			request := httptest.NewRequest(http.MethodGet, "/v1/media-sets/"+mediaID+"/items/"+hash+suffix, nil)
			request.SetPathValue("id", mediaID)
			request.SetPathValue("hash", hash)
			response := httptest.NewRecorder()
			tenant.downloadMediaItem(response, request, admin, invariantClock)
			if response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), raw) {
				t.Fatalf("installed history download %s: %d %s", suffix, response.Code, response.Body.String())
			}
		}
		request := httptest.NewRequest(http.MethodGet, "/v1/media-sets/"+mediaID+"/items/"+hash, nil)
		request.SetPathValue("id", mediaID)
		request.SetPathValue("hash", hash)
		denied := httptest.NewRecorder()
		tenant.downloadMediaItem(denied, request, platform.Member{ID: "outsider", Roles: map[string]string{}}, invariantClock)
		if denied.Code == http.StatusOK {
			t.Fatal("unprivileged media download was permitted")
		}
	}
	check(target)
	want := target.recordImage(t)
	for replay := 0; replay < 2; replay++ {
		if err := target.files().Delete(context.Background(), target.ID+"/"+hash); err != nil {
			t.Fatal(err)
		}
		restored := newInvariantTenantReplayWithApps(t, target.ID, nil, seat)
		restored.Files = target.files()
		if err := restored.recoverEntries(target.entries); err != nil {
			t.Fatal(err)
		}
		check(restored)
		if differing := differingTypes(want, restored.recordImage(t)); len(differing) != 0 {
			t.Fatalf("media replay differs in %v", differing)
		}
	}
	reuse := newInvariantTenant(t, "media-reuse", seat)
	reuse.accept(t, platform.MediaSetType+".create", platform.MediaSetType, "LOCAL", map[string]any{"name": "documents", "title": "Local documents"})
	reuseAdmin, _ := reuse.Member("admin")
	offer, err := reuse.inspectPackage(context.Background(), platform.PackageInspectRequest{Content: platform.Raw(descriptor)})
	if err != nil {
		t.Fatal(err)
	}
	importRequest := reuse.submission(packages.SchemaPackageImport, platform.PackageType, "media", map[string]any{"id": "media", "digest": offer.Digest})
	importRequest.Authority, importRequest.PrincipalId = "platform", reuseAdmin.ID
	if _, refusal := reuse.Submit(reuseAdmin, importRequest, invariantClock); refusal != nil {
		t.Fatal(refusal)
	}
	installRequest := reuse.submission(packages.SchemaPackageInstall, platform.PackageType, "media", map[string]any{"id": "media", "version": "1.0.0"})
	installRequest.Authority, installRequest.PrincipalId = "platform", reuseAdmin.ID
	if _, refusal := reuse.Submit(reuseAdmin, installRequest, invariantClock); refusal == nil {
		t.Fatal("package installation overwrote a customer resource")
	}
	local, _ := platform.Get[platform.MediaSet](reuse.automation(build.ID, false), "LOCAL")
	if local.Title != "Local documents" || local.Version != 0 {
		t.Fatal("package draft creation rewrote an existing media set")
	}
	edit := target.submission(platform.MediaSetType+".edit", platform.MediaSetType, mediaID, map[string]any{"title": "Customer override"})
	edit.PrincipalId = admin.ID
	if _, refusal := target.Submit(admin, edit, invariantClock); refusal == nil {
		t.Fatal("customer edited a managed media set")
	}
	target.accept(t, platform.MediaSetType+".create", platform.MediaSetType, "CUSTOMER", map[string]any{"name": "customer", "title": "Customer extension"})
	source.accept(t, platform.SchemaMediaSetAdd, platform.MediaSetType, "PUBLISHER-MEDIA", map[string]any{"hash": hash, "name": "publisher.txt", "size": len(raw), "contentType": "text/plain"})
	next := invariantPublishData(t, source, source.member(t), []build.JointDraftRef{{Kind: platform.AssetMediaSet, ID: "PUBLISHER-MEDIA"}, {Kind: platform.AssetApp, ID: "MEDIA-APP"}}, "media-release-next")
	nextCandidate, err := platform.ReadCandidate(next, source.releases.Raw(next))
	if err != nil {
		t.Fatal(err)
	}
	descriptor.Version, descriptor.Blueprint.Assets = "1.1.0", nextCandidate.Assets
	nextOffer, err := target.inspectPackage(context.Background(), platform.PackageInspectRequest{Content: platform.Raw(descriptor)})
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []struct {
		schema  string
		payload any
	}{
		{packages.SchemaPackageImport, map[string]any{"id": "media", "digest": nextOffer.Digest}},
		{packages.SchemaPackageUpgrade, map[string]any{"id": "media", "version": "1.1.0"}},
		{packages.SchemaPackageRetire, map[string]any{"id": "media"}},
	} {
		submission := target.submission(action.schema, platform.PackageType, "media", action.payload)
		submission.Authority, submission.PrincipalId = "platform", admin.ID
		if _, refusal := target.Submit(admin, submission, invariantClock); refusal != nil {
			t.Fatalf("%s: %v", action.schema, refusal)
		}
	}
	check(target)
	customer, exists := platform.Get[platform.MediaSet](target.automation(build.ID, false), "CUSTOMER")
	if !exists || customer.Archived || customer.Title != "Customer extension" {
		t.Fatal("package lifecycle changed a customer extension")
	}
}
