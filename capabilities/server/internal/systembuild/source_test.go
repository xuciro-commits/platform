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
	work := filepath.Join(t.TempDir(), "retained")
	if err := CopySource(source.Root, work); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(work, "web/src/main.tsx"))
	if err != nil || string(body) != "running" {
		t.Fatalf("mutable checkout entered build: %q / %v", body, err)
	}
	if body, err := os.ReadFile(filepath.Join(work, "web/src/obsolete.ts")); err != nil || string(body) != "remove" {
		t.Fatalf("retained source was changed: %q / %v", body, err)
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
	body, err = os.ReadFile(filepath.Join(exported, "web/src/main.tsx"))
	if err != nil || string(body) != "running" {
		t.Fatalf("export needs its deleted build tree: %q / %v", body, err)
	}
	for _, file := range built.Runtime(7).Files {
		if file.Editable {
			t.Fatal("retained source became editable")
		}
	}
	writeSource(t, built.Root, "files/web/src/main.tsx", "tampered")
	if err := CopySource(built.Root, filepath.Join(t.TempDir(), "bad-export")); err == nil {
		t.Fatal("tampered source exported")
	}
}
