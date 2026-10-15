package platformserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"
	"time"

	"platformserver/platform"
)

func TestProtocolPackageInstallRetainsVerifiedBinary(t *testing.T) {
	directory := t.TempDir()
	if err := os.Mkdir(filepath.Join(directory, "artifacts"), 0700); err != nil {
		t.Fatal(err)
	}
	write := func(version string) string {
		t.Helper()
		binary := []byte("trusted binary " + version)
		sum := sha256.Sum256(binary)
		digest := hex.EncodeToString(sum[:])
		if err := os.WriteFile(filepath.Join(directory, "artifacts", digest), binary, 0700); err != nil {
			t.Fatal(err)
		}
		descriptor := PackageDescriptor{ID: "test.protocol", Version: version, Namespace: "test.protocol", Runtime: &platform.DriverRuntime{ABI: platform.DriverABI, Targets: []platform.DriverTarget{{OS: goruntime.GOOS, Arch: goruntime.GOARCH, Artifact: digest}}, Profiles: []platform.DriverProfile{{ID: "read", Title: "Read points", Operations: []string{"read", "sample"}}}}}
		raw, _ := json.Marshal(descriptor)
		if err := os.WriteFile(filepath.Join(directory, version+".json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
		return digest
	}
	first := write("1.0.0")
	tn := composeTenant(t, "protocol-package", []Seat{seatOf("admin", PlatformApp+":"+Admin)})
	console := testConsoleOf(tn)
	var err error
	console.index, err = LoadPackageIndex(directory)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	if got := refuse(t, tn, "admin", PlatformApp, SchemaPackageInstall, PackageType, "test.protocol", map[string]any{"id": "test.protocol"}, at); got == "ok" {
		t.Fatal("protocol installed without exact version")
	}
	decide(t, tn, "admin", PlatformApp, SchemaPackageInstall, PackageType, "test.protocol", map[string]any{"id": "test.protocol", "version": "1.0.0"}, at)
	if _, err = console.ResolveDriver("test.protocol", "1.0.0", first, "read"); err != nil {
		t.Fatal(err)
	}
	// Publishing a new trusted artifact is visible to this same Console. No
	// BindPackageIndex call or tenant/host restart replaces its original index.
	bound := console.index
	second := write("2.0.0")
	views := console.PackageViews()
	if len(views) != 2 || views[1].Descriptor.Version != "2.0.0" || !views[1].Precheck.OK {
		t.Fatalf("new directory version is unavailable: %+v", views)
	}
	if console.index != bound || len(bound.Packages) != 1 {
		t.Fatal("listing changed the shared deployment index")
	}
	decide(t, tn, "admin", PlatformApp, SchemaPackageUpgrade, PackageType, "test.protocol", map[string]any{"id": "test.protocol", "version": "2.0.0"}, at)
	if _, err = console.ResolveDriver("test.protocol", "1.0.0", first, "read"); err != nil {
		t.Fatal("retained binary unavailable", err)
	}
	if catalog := console.ProtocolPackages(); len(catalog) != 2 || catalog[0].Version != "1.0.0" || catalog[0].State != "retained" || catalog[1].Version != "2.0.0" || catalog[1].Digest != second {
		t.Fatalf("installed catalog: %+v", catalog)
	}
	if _, err = console.ResolveDriver("test.protocol", "2.0.0", first, "read"); err == nil {
		t.Fatal("wrong content digest resolved")
	}
	if err = tn.files().Put(context.Background(), packageBinaryKey(tn.ID, "test.protocol", "1.0.0", first), []byte("modified"), "application/octet-stream"); err != nil {
		t.Fatal(err)
	}
	if _, err = console.ResolveDriver("test.protocol", "1.0.0", first, "read"); err == nil {
		t.Fatal("changed retained binary resolved")
	}
	if err = os.WriteFile(filepath.Join(directory, "artifacts", second), []byte("modified"), 0700); err != nil {
		t.Fatal(err)
	}
	current, loadErr := LoadPackageIndex(directory)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if _, err = current.ResolveDriver("test.protocol", "2.0.0", second, "read"); err == nil {
		t.Fatal("changed trusted-directory binary resolved")
	}
}
