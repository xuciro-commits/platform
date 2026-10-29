package systembuild

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"platformserver/internal/systemscope"
	"platformserver/platform"
)

type Source struct {
	Root   string                               `json:"-"`
	Digest string                               `json:"digest"`
	Files  map[string]platform.SystemSourceFile `json:"files"`
}

var sourceSkip = map[string]bool{".git": true, ".build": true, ".codex": true, "node_modules": true, "dist": true, ".next": true, ".turbo": true, "coverage": true, ".DS_Store": true}

func sourceManifest(root string, copyTo string) (map[string]platform.SystemSourceFile, error) {
	files := map[string]platform.SystemSourceFile{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if path != root && (sourceSkip[info.Name()] || systemscope.Sensitive(rel)) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		mode := uint32(0o644)
		if info.Mode()&0o111 != 0 {
			mode = 0o755
		}
		files[rel] = platform.SystemSourceFile{Path: rel, Digest: systemscope.Digest(body), Size: len(body), Mode: mode, Editable: systemscope.Verdict(rel) == ""}
		if copyTo != "" {
			target := filepath.Join(copyTo, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			return os.WriteFile(target, body, os.FileMode(mode))
		}
		return nil
	})
	return files, err
}

func sourceDigest(files map[string]platform.SystemSourceFile) string {
	manifest := map[string]string{}
	for path, file := range files {
		manifest[path] = fmt.Sprintf("%s %o", file.Digest, file.Mode)
	}
	return systemscope.Manifest(manifest)
}

func CaptureSource(root, store string) (Source, error) {
	if err := os.MkdirAll(store, 0o755); err != nil {
		return Source{}, err
	}
	temp, err := os.MkdirTemp(store, ".source-")
	if err != nil {
		return Source{}, err
	}
	defer os.RemoveAll(temp)
	files, err := sourceManifest(root, filepath.Join(temp, "files"))
	if err != nil {
		return Source{}, err
	}
	source := Source{Digest: sourceDigest(files), Files: files}
	source.Root = filepath.Join(store, source.Digest)
	if _, err := os.Stat(source.Root); err == nil {
		return OpenSource(source.Root)
	}
	body, err := json.Marshal(source)
	if err != nil {
		return Source{}, err
	}
	if err := os.WriteFile(filepath.Join(temp, "manifest.json"), body, 0o644); err != nil {
		return Source{}, err
	}
	if err := os.Rename(temp, source.Root); err != nil {
		return Source{}, err
	}
	return source, nil
}

func OpenSource(root string) (Source, error) {
	body, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		return Source{}, err
	}
	var source Source
	if err := json.Unmarshal(body, &source); err != nil {
		return Source{}, err
	}
	source.Root = root
	if err := VerifySource(root, source.Digest); err != nil {
		return Source{}, err
	}
	return source, nil
}

func VerifySource(root, expected string) error {
	body, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		return err
	}
	var source Source
	if err := json.Unmarshal(body, &source); err != nil {
		return err
	}
	if expected == "" || source.Digest != expected || sourceDigest(source.Files) != expected {
		return fmt.Errorf("source manifest differs from %s", expected)
	}
	actual, err := sourceManifest(filepath.Join(root, "files"), "")
	if err != nil {
		return err
	}
	if sourceDigest(actual) != expected {
		return fmt.Errorf("source bytes differ from %s", expected)
	}
	return nil
}

func CopySource(root, out string) error {
	source, err := OpenSource(root)
	if err != nil {
		return err
	}
	for path, file := range source.Files {
		body, err := os.ReadFile(filepath.Join(root, "files", filepath.FromSlash(path)))
		if err != nil {
			return err
		}
		target := filepath.Join(out, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, body, os.FileMode(file.Mode)); err != nil {
			return err
		}
	}
	return nil
}

func (source Source) Runtime(version int) *platform.SystemSource {
	files := make([]platform.SystemSourceFile, 0, len(source.Files))
	for _, file := range source.Files {
		file.Editable = systemscope.Verdict(file.Path) == ""
		files = append(files, file)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return &platform.SystemSource{Version: version, Digest: source.Digest, Files: files, Read: func(path string) (string, error) {
		file, ok := source.Files[path]
		if !ok {
			return "", fmt.Errorf("source file %s is unavailable", path)
		}
		body, err := os.ReadFile(filepath.Join(source.Root, "files", filepath.FromSlash(path)))
		if err != nil {
			return "", err
		}
		if systemscope.Digest(body) != file.Digest {
			return "", fmt.Errorf("source file %s changed", path)
		}
		return string(body), nil
	}}
}

func DigestFile(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return ""
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func DigestTree(root string) (string, error) {
	if _, err := os.Stat(root); err != nil {
		return "", err
	}
	digest := digestFile(root)
	if digest == "" {
		return "", fmt.Errorf("cannot digest %s", root)
	}
	return digest, nil
}

func digestFile(p string) string {
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	h := sha256.New()
	err := filepath.Walk(p, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(p, path)
		fmt.Fprintf(h, "%x  %s\n", sha256.Sum256(body), filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}
