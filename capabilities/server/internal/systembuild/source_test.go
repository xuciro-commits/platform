package systembuild

import (
	"os"
	"path/filepath"
	"testing"
)

func writeSource(t *testing.T, root, path, body string) {
	t.Helper()
	name := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFrozenSourceSurvivesRepositoryEditsAndWorktreeDeletion(t *testing.T) {
	root, store := t.TempDir(), t.TempDir()
	writeSource(t, root, "web/src/main.tsx", "running")
	writeSource(t, root, "web/src/obsolete.ts", "remove")
	writeSource(t, root, "contract/spec/K1.md", "kernel")
	writeSource(t, root, "deploy/local/.env", "do not export")
	writeSource(t, root, "deploy/idp.key", "do not export")
	writeSource(t, root, "web/node_modules/cache.js", "dependency cache")
	writeSource(t, root, ".build/old.log", "old output")
	source, err := CaptureSource(root, store)
	if err != nil {
		t.Fatal(err)
	}
	if len(source.Files) != 3 {
		t.Fatalf("source includes credentials or generated state: %+v", source.Files)
	}
	writeSource(t, root, "web/src/main.tsx", "mutable checkout")
	work, err := Materialize(Config{Root: root, Work: t.TempDir()}, source, map[string]string{"web/src/next.ts": "new"}, []string{"web/src/obsolete.ts"})
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(work, "web/src/main.tsx"))
	if err != nil || string(body) != "running" {
		t.Fatalf("mutable checkout entered build: %q / %v", body, err)
	}
	if _, err := os.Stat(filepath.Join(work, "web/src/obsolete.ts")); !os.IsNotExist(err) {
		t.Fatalf("approved delete was not applied: %v", err)
	}
	built, err := CaptureSource(work, store)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(work); err != nil {
		t.Fatal(err)
	}
	exported := filepath.Join(t.TempDir(), "export")
	if err := CopySource(built.Root, exported); err != nil {
		t.Fatal(err)
	}
	body, err = os.ReadFile(filepath.Join(exported, "web/src/next.ts"))
	if err != nil || string(body) != "new" {
		t.Fatalf("export needs its deleted build tree: %q / %v", body, err)
	}
	for _, file := range built.Runtime(7).Files {
		if file.Path == "contract/spec/K1.md" && file.Editable {
			t.Fatal("kernel source became editable")
		}
	}
	writeSource(t, built.Root, "files/web/src/main.tsx", "tampered")
	if err := CopySource(built.Root, filepath.Join(t.TempDir(), "bad-export")); err == nil {
		t.Fatal("tampered source exported")
	}
}

func TestMaterializationRefusesFrozenParentDeletes(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "contract/spec/K1.md", "kernel")
	source, err := CaptureSource(root, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Materialize(Config{Work: t.TempDir()}, source, nil, []string{"contract"}); err == nil {
		t.Fatal("parent deletion bypassed the kernel gate")
	}
}
