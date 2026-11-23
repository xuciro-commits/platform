package packages

import (
	"net/url"
	"strings"
	"testing"

	runtimeprotocol "platformserver/kernel/protocol"
)

func TestResolveCatalogCandidates(t *testing.T) {
	ghURL := (&url.URL{Scheme: "https", Host: "github.com", Path: "/acme/packages"}).String()
	gh, err := ResolveCatalogCandidates(ghURL)
	if err != nil || len(gh) != 2 || !strings.HasSuffix(gh[0], "/acme/packages/main/catalog.json") || !strings.HasSuffix(gh[1], "/acme/packages/master/catalog.json") {
		t.Fatalf("github repo mapping: %+v err=%v", gh, err)
	}
	gtURL := (&url.URL{Scheme: "https", Host: "gitee.com", Path: "/acme/packages/tree/v2/catalog"}).String()
	gt, err := ResolveCatalogCandidates(gtURL)
	if err != nil || len(gt) != 1 || !strings.HasSuffix(gt[0], "/acme/packages/raw/v2/catalog/catalog.json") {
		t.Fatalf("gitee tree mapping: %+v err=%v", gt, err)
	}
	loopURL := (&url.URL{Scheme: "http", Host: "127.0.0.1:18999", Path: "/catalog.json"}).String()
	if _, _, err := ValidatePackageURL(loopURL); err == nil {
		t.Fatal("loopback HTTP must be refused when allow-loopback is off")
	}
	SetAllowLoopback(true)
	t.Cleanup(func() { SetAllowLoopback(false) })
	if _, allowPrivate, err := ValidatePackageURL(loopURL); err != nil || !allowPrivate {
		t.Fatalf("loopback HTTP should be permitted when enabled: allowPrivate=%v err=%v", allowPrivate, err)
	}
}

func TestEffectiveLibrariesIncludesOfficialAndDedupes(t *testing.T) {
	third := (&url.URL{Scheme: "https", Host: "github.com", Path: "/acme/community-packages"}).String()
	libs := EffectiveLibraries([]runtimeprotocol.PackageLibrary{
		{ID: "dup", URL: OfficialLibraryURL() + "/", Title: "Duplicate"},
		{ID: "third", URL: third, Title: "Acme"},
	})
	if len(libs) != 2 || !libs[0].Builtin || libs[0].ID != OfficialLibraryID || libs[1].ID != "third" || libs[1].Builtin {
		t.Fatalf("unexpected effective libraries: %+v", libs)
	}
}
