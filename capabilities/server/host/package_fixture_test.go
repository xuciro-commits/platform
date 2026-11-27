package host

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func fixturePath(t testing.TB, name string) string {
	t.Helper()
	root := os.Getenv("PLATFORM_TEST_PACKAGES_DIR")
	if root == "" {
		t.Fatal("run package tests via make test or make test-pg")
	}
	raw, err := os.ReadFile(filepath.Join(root, "inputs.json"))
	if err != nil {
		t.Fatal(err)
	}
	var inputs struct {
		Packages map[string]struct{ SHA256 string }
	}
	if err := json.Unmarshal(raw, &inputs); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, name+".json")
	content, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if want := inputs.Packages[name].SHA256; want != fmt.Sprintf("sha256:%x", sha256.Sum256(content)) {
		t.Fatalf("package input digest mismatch: %s", name)
	}
	return file
}
