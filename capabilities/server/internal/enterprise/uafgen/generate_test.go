package uafgen

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"platformserver/internal/enterprise/uaf"
)

func TestGeneratedReleasesPreserveTheCompleteOriginalProfiles(t *testing.T) {
	source := os.DirFS(filepath.Join("..", "uaf", "spec"))
	first, err := Generate(source)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Generate(source)
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("the same original files generated different data: %v", err)
	}
	releases, err := uaf.Releases()
	if err != nil || len(releases) != len(first) {
		t.Fatalf("generated releases were omitted from runtime: %v", err)
	}
	for name, raw := range first {
		checkedIn, err := os.ReadFile(filepath.Join("..", "uaf", "gen", name))
		if err != nil || !bytes.Equal(checkedIn, raw) {
			t.Fatalf("%s is stale; run make generate-uaf: %v", name, err)
		}
		var generated uaf.GeneratedRelease
		if err := json.Unmarshal(raw, &generated); err != nil {
			t.Fatal(err)
		}
		if generated.FormatVersion != uaf.DataFormat || generated.StandardVersion != generated.Metamodel.Version || len(generated.Sources) != 2 {
			t.Fatalf("%s lost its data format or original sources", name)
		}
		for _, original := range generated.Sources {
			file, err := fs.ReadFile(source, original.File)
			if err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(file)
			if original.SHA256 != hex.EncodeToString(digest[:]) {
				t.Fatalf("%s no longer identifies its complete original bytes", original.File)
			}
		}
		profile, err := fs.ReadFile(source, generated.Sources[0].File)
		if err != nil {
			t.Fatal(err)
		}
		original, err := Parse(profile)
		if err != nil {
			t.Fatal(err)
		}
		original.Version = generated.StandardVersion
		if !reflect.DeepEqual(*original, generated.Metamodel) {
			t.Fatalf("%s omitted or changed original profile metadata", name)
		}
		runtime, ok := uaf.Release(original.Version)
		if !ok || !reflect.DeepEqual(*runtime, *original) {
			t.Fatalf("%s changed the publicly discoverable metamodel", name)
		}
	}
}
