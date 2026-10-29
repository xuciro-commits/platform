package platformserver

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	controlowner "platformserver/kernel/control"
	"platformserver/platform"
	"platformserver/studio/packages"
	"strings"
	"testing"
	"time"
)

func TestPackageSourcesReviewIsInertAndInstallationUsesReviewedBytes(t *testing.T) {
	tn := composeTenant(t, "package-sources", []platform.Seat{seatOf("admin", "platform:admin")})
	console := testConsoleOf(tn)
	before, err := console.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"id":"test.extension","version":"1","namespace":"test.extension","title":"Test extension"}`)
	downloads := 0
	tn.Outbound = func(request *http.Request, private bool) (*http.Response, error) {
		downloads++
		if private || request.URL.Scheme != "https" || request.Header.Get("Authorization") != "" {
			t.Fatal("package download gained unrelated access")
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(raw))}, nil
	}
	preview, err := tn.inspectPackage(context.Background(), platform.PackageInspectRequest{URL: "https://packages.example.test/extension.json?temporary=token"})
	if err != nil {
		t.Fatal(err)
	}
	after, err := console.Snapshot()
	if err != nil || !bytes.Equal(before, after) || len(console.PackageViews()) != 0 {
		t.Fatal("review changed authoritative package state", err)
	}
	if preview.Origin != "https://packages.example.test/extension.json" {
		t.Fatal("temporary URL credentials entered source metadata")
	}
	raw = []byte(`{"id":"test.extension","version":"2","namespace":"test.extension"}`)
	var entries []Entry
	tn.Record = func(entry Entry) { entries = append(entries, entry) }
	at := time.Now().UTC()
	decide(t, tn, "admin", controlowner.PlatformApp, packages.SchemaPackageImport, platform.PackageType, "test.extension", map[string]any{"id": "test.extension", "digest": preview.Digest}, at)
	views := console.PackageViews()
	if len(views) != 1 || views[0].Descriptor.Version != "1" || views[0].Installed != nil || !views[0].Precheck.OK {
		t.Fatalf("reviewed source: %+v", views)
	}
	decide(t, tn, "admin", controlowner.PlatformApp, packages.SchemaPackageInstall, platform.PackageType, "test.extension", map[string]any{"id": "test.extension", "version": "1"}, at)
	if downloads != 1 {
		t.Fatal("confirm or install fetched new content")
	}
	if refusal := refuse(t, tn, "admin", controlowner.PlatformApp, packages.SchemaPackageRemoveSource, platform.PackageType, "test.extension", map[string]any{"id": "test.extension", "digest": preview.Digest}, at); refusal == "ok" {
		t.Fatal("installed source was removed")
	}
	recovered := composeTenant(t, tn.ID, []platform.Seat{seatOf("admin", "platform:admin")})
	recovered.Files = tn.files()
	if err := recovered.Replay(entries); err != nil {
		t.Fatal(err)
	}
	if got := testConsoleOf(recovered).PackageViews(); len(got) != 1 || got[0].SourceDigest != preview.Digest || got[0].Installed == nil {
		t.Fatalf("source did not survive result recovery: %+v", got)
	}
	decide(t, tn, "admin", controlowner.PlatformApp, packages.SchemaPackageDrain, platform.PackageType, "test.extension", map[string]any{"id": "test.extension"}, at)
	decide(t, tn, "admin", controlowner.PlatformApp, packages.SchemaPackageRetire, platform.PackageType, "test.extension", map[string]any{"id": "test.extension"}, at)
	decide(t, tn, "admin", controlowner.PlatformApp, packages.SchemaPackageRemoveSource, platform.PackageType, "test.extension", map[string]any{"id": "test.extension", "digest": preview.Digest}, at)
	if views = console.PackageViews(); len(views) != 1 || views[0].Source != "installed" || views[0].Installed.State != "retired" {
		t.Fatalf("removing source destroyed retained installation: %+v", views)
	}
	retained, err := (hostView{t: tn}).GetPackageArtifact(packages.ArtifactKey(tn.ID, "test.extension", "1"))
	if err != nil {
		t.Fatal("removing source deleted historical artifact", err)
	}
	sum := sha256.Sum256(retained)
	if "sha256:"+hex.EncodeToString(sum[:]) != views[0].Installed.Artifact {
		t.Fatal("removing source changed historical artifact")
	}

}

func TestLocalPackageSourceRejectsChangedVersionAndInvalidContent(t *testing.T) {
	tn := composeTenant(t, "local-package", []platform.Seat{seatOf("admin", "platform:admin")})
	for _, content := range []string{"null", `{}`, `{"id":"test","version":"1","namespace":"test","installScript":"run"}`, `{"id":"test","version":"1","namespace":"test"} {}`} {
		if _, err := tn.inspectPackage(context.Background(), platform.PackageInspectRequest{Content: json.RawMessage(content)}); err == nil {
			t.Fatalf("invalid package was accepted: %s", content)
		}
	}
	if _, err := tn.inspectPackage(context.Background(), platform.PackageInspectRequest{Content: make(json.RawMessage, packages.MaxPackageImportBytes+1)}); err == nil {
		t.Fatal("oversized local package passed review")
	}
	tn.Outbound = func(*http.Request, bool) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found"))}, nil
	}
	if _, err := tn.inspectPackage(context.Background(), platform.PackageInspectRequest{URL: "https://packages.example.test/missing.json"}); err == nil || len(testConsoleOf(tn).PackageViews()) != 0 {
		t.Fatal("failed download created a package source")
	}
	preview, err := tn.inspectPackage(context.Background(), platform.PackageInspectRequest{Content: json.RawMessage(`{"id":"test","version":"1","namespace":"test"}`)})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	decide(t, tn, "admin", controlowner.PlatformApp, packages.SchemaPackageImport, platform.PackageType, "test", map[string]any{"id": "test", "digest": preview.Digest}, at)
	changed, err := tn.inspectPackage(context.Background(), platform.PackageInspectRequest{Content: json.RawMessage(`{"id":"test","version":"1","namespace":"test","title":"changed"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if refusal := refuse(t, tn, "admin", controlowner.PlatformApp, packages.SchemaPackageImport, platform.PackageType, "test", map[string]any{"id": "test", "digest": changed.Digest}, at); refusal == "ok" {
		t.Fatal("same version imported different contents")
	}
	if len(testConsoleOf(tn).PackageViews()) != 1 {
		t.Fatal("refused import changed the library")
	}
}

func TestPackageReviewRejectsForeignFormatsAndTamperedOffers(t *testing.T) {
	tenant := composeTenant(t, "archive-review", []platform.Seat{seatOf("admin", "platform:admin")})
	descriptor := platform.PackageDescriptor{ID: "test.review", Version: "1", Namespace: "test.review"}
	preview, err := tenant.inspectPackage(context.Background(), platform.PackageInspectRequest{Content: platform.Raw(descriptor)})
	if err != nil || preview.Descriptor.ID != descriptor.ID {
		t.Fatal(preview, err)
	}
	foreign := map[string]any{"format": "platform.package.v1", "descriptor": descriptor}
	if _, err := tenant.inspectPackage(context.Background(), platform.PackageInspectRequest{Content: platform.Raw(foreign)}); err == nil {
		t.Fatal("a foreign archive format passed review")
	}
	if len(testConsoleOf(tenant).PackageViews()) != 0 {
		t.Fatal("review installed state")
	}
	offerKey := packages.OfferKey(tenant.ID, preview.Digest)
	if err := tenant.files().Put(context.Background(), offerKey, []byte(`{"changed":true}`), "application/json"); err != nil {
		t.Fatal(err)
	}
	if refusal := refuse(t, tenant, "admin", controlowner.PlatformApp, packages.SchemaPackageImport, platform.PackageType, descriptor.ID, map[string]any{"id": descriptor.ID, "digest": preview.Digest}, time.Now()); refusal == "ok" {
		t.Fatal("tampered offer was imported")
	}
}
