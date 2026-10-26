package platformserver

import "platformserver/internal/construction"

import (
	"os"
	"path/filepath"
	"testing"

	"platformserver/apps/build"
	"platformserver/apps/core"
	"platformserver/platform"
)

func TestPackageBlueprintExtendsInstalledCodeWithoutNativeDrafts(t *testing.T) {
	base := platform.AssetRef{App: core.ID, Kind: platform.AssetApp, Name: core.ID}
	page, err := platform.PageReleaseAsset(construction.ID, "1", platform.Page{Name: "materialdesk", Title: "Materials", Object: platform.AssetRef{App: core.ID, Kind: platform.AssetObject, Name: core.MaterialType}, Sections: []platform.Section{{ID: "table", Widget: "table", Fields: []string{"name"}}}})
	if err != nil {
		t.Fatal(err)
	}
	application, err := platform.ApplicationReleaseAsset(construction.ID, "1", platform.Application{Name: "materialworkspace", Title: "Material workspace", Pages: []string{"materialdesk"}, Extends: &base, Entry: core.ID,
		Replacements: []platform.ApplicationPageReplacement{{View: "materials", Page: "materialdesk"}}})
	if err != nil {
		t.Fatal(err)
	}
	descriptor := PackageDescriptor{ID: "example.material-workspace", Version: "1.0.0", Namespace: "example.material-workspace", Blueprint: &PackageBlueprint{Roots: []platform.AssetRef{application.Ref}, Assets: []platform.ReleaseAsset{page, application}}}
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "materials.json"), platform.Raw(descriptor), 0600); err != nil {
		t.Fatal(err)
	}
	index, err := LoadPackageIndex(directory)
	if err != nil {
		t.Fatal(err)
	}
	tn := composeTenant(t, "native-blueprint", []Seat{seatOf("admin", "platform:admin"), seatOf("builder", "build:builder", "core:steward")}, build.New("native-blueprint"), core.New("native-blueprint"))
	testConsoleOf(tn).BindPackageIndex(index)
	decide(t, tn, "admin", PlatformApp, SchemaPackageInstall, PackageType, descriptor.ID, map[string]any{"id": descriptor.ID, "version": descriptor.Version}, txnAt)
	original, err := tn.app(core.ID).(*core.Core).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	answer, err := tn.CreatePackageDrafts(memberOf(t, tn, "builder"), descriptor.ID, descriptor.Version, "native-drafts", txnAt)
	if err != nil || len(answer.Drafts) != 2 || answer.Application != "materialworkspace" {
		t.Fatalf("native dependency prevented ordinary Build drafts: %+v %v", answer, err)
	}
	for _, draft := range answer.Drafts {
		if draft.Ref.App != construction.ID || draft.Type != construction.AppType && draft.Type != construction.PageType {
			t.Fatalf("native prerequisite became a draft: %+v", draft)
		}
	}
	current, err := tn.app(core.ID).(*core.Core).Snapshot()
	if err != nil || string(original) != string(current) || tn.ActiveRelease() != "" {
		t.Fatal("draft creation changed the native owner or activated the application")
	}
	preview, err := tn.PreviewReleaseDrafts(memberOf(t, tn, "builder"), []construction.JointDraftRef{{Kind: platform.AssetPage, ID: "materialdesk"}, {Kind: platform.AssetApp, ID: "materialworkspace"}})
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("native extension drafts do not form an ordinary candidate: %+v %v", preview, err)
	}
}
