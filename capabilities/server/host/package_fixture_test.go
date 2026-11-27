package host

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

var fixtureDirectory = sync.OnceValues(func() (string, error) {
	raw, err := exec.Command("node", "../../../scripts/package-fixtures.mjs").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, raw)
	}
	return strings.TrimSpace(string(raw)), nil
})

func fixturePath(t testing.TB, path string) string {
	t.Helper()
	root, err := fixtureDirectory()
	if err != nil {
		t.Fatalf("external package fixtures: %v (set PLATFORM_FIXTURES_DIR to the pinned package fixture root)", err)
	}
	return filepath.Join(root, path)
}
