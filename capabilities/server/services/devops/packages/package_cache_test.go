package packages

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"platformserver/kernel/protocol"
	"platformserver/platform"
	"platformserver/services/resources/publication"
)

func TestDirectoryIndexCacheInvalidatesOnChange(t *testing.T) {
	dir := t.TempDir()
	rawA := `{"id":"alpha","version":"0.1.0","title":"Alpha","namespace":"alpha"}`
	path := filepath.Join(dir, "alpha.json")
	if err := os.WriteFile(path, []byte(rawA), 0o644); err != nil {
		t.Fatal(err)
	}
	resolver := publication.Resolver{}

	first, stamp1, err := loadCachedPackageIndex(resolver, dir)
	if err != nil {
		t.Fatal(err)
	}
	second, stamp2, err := loadCachedPackageIndex(resolver, dir)
	if err != nil {
		t.Fatal(err)
	}
	if stamp1 != stamp2 || first != second {
		t.Fatalf("expected identical cached index pointer when directory is unchanged")
	}

	rawB := `{"id":"alpha","version":"0.2.0","title":"Alpha 2","namespace":"alpha"}`
	if err := os.WriteFile(path, []byte(rawB), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	_ = os.Chtimes(path, future, future)

	third, stamp3, err := loadCachedPackageIndex(resolver, dir)
	if err != nil {
		t.Fatal(err)
	}
	if stamp3 == stamp1 || third == first {
		t.Fatalf("expected cache invalidation after file update")
	}
	if len(third.Packages) != 1 || third.Packages[0].Version != "0.2.0" {
		t.Fatalf("expected updated version 0.2.0, got %+v", third.Packages)
	}
}

func TestPackageViewsCacheConcurrentAndInvalidates(t *testing.T) {
	owner := New("test", publication.Resolver{})
	owner.BindPackageIndex(&protocol.PackageIndex{
		Packages: []platform.PackageDescriptor{
			{ID: "alpha", Version: "0.1.0", Title: "Alpha", Namespace: "alpha"},
		},
	})

	first := owner.PackageViews()
	if owner.viewsCacheKey == "" {
		t.Fatalf("expected viewsCacheKey to be populated after PackageViews")
	}
	key1 := owner.viewsCacheKey

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got := owner.PackageViews()
			if len(got) != len(first) {
				t.Errorf("concurrent PackageViews length mismatch: got %d want %d", len(got), len(first))
			}
		}()
	}
	wg.Wait()
	if owner.viewsCacheKey != key1 {
		t.Fatalf("expected viewsCacheKey to remain stable across concurrent reads")
	}

	owner.BindPackageIndex(&protocol.PackageIndex{
		Packages: []platform.PackageDescriptor{
			{ID: "alpha", Version: "0.2.0", Title: "Alpha", Namespace: "alpha"},
		},
	})
	if owner.viewsCacheKey != "" || owner.viewsCache != nil {
		t.Fatalf("expected BindPackageIndex to clear views cache")
	}
	_ = owner.PackageViews()
	if owner.viewsCacheKey == "" || owner.viewsCacheKey == key1 {
		t.Fatalf("expected new viewsCacheKey after index change")
	}
}

func TestPreparationBlueprintDirectLookup(t *testing.T) {
	owner := New("test", publication.Resolver{})
	owner.BindPackageIndex(&protocol.PackageIndex{
		Packages: []platform.PackageDescriptor{
			{
				ID:        "alpha",
				Version:   "0.1.0",
				Title:     "Alpha",
				Namespace: "alpha",
				Blueprint: &platform.PackageBlueprint{
					Roots: []platform.AssetRef{{App: "build", Kind: "app", Name: "alpha"}},
				},
			},
		},
	})

	bp, held, err := owner.PreparationBlueprint("alpha", "0.1.0")
	if err != nil {
		t.Fatalf("PreparationBlueprint failed: %v", err)
	}
	if held != nil {
		t.Fatalf("expected nil InstalledPackage for uninstalled package, got %+v", held)
	}
	if len(bp.Roots) != 1 || bp.Roots[0].Name != "alpha" {
		t.Fatalf("unexpected blueprint roots: %+v", bp.Roots)
	}
	if _, _, err := owner.PreparationBlueprint("alpha", "9.9.9"); err == nil {
		t.Fatalf("expected error for missing version")
	}
}
