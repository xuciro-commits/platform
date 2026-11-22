package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

type packed struct {
	id, version  string
	assets       int
	artifact     string
	artifactPath string
	lock         string
	lockPath     string
}

func digest(text string) string {
	sum := sha256.Sum256([]byte(text))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func readJSON(path string) (any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	value, err := parseJSON(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return value, nil
}

func readObject(path string) (*object, error) {
	value, err := readJSON(path)
	if err != nil {
		return nil, err
	}
	out := asObject(value)
	if out == nil {
		return nil, fmt.Errorf("%s: expected an object", path)
	}
	return out, nil
}

func asObject(value any) *object {
	switch v := value.(type) {
	case *object:
		return v
	}
	return nil
}

func inside(base, value string) (string, error) {
	target := filepath.Join(base, value)
	if filepath.IsAbs(value) {
		target = filepath.Clean(value)
	}
	rel, err := filepath.Rel(base, target)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", fmt.Errorf("%s escapes %s", value, base)
	}
	return target, nil
}

func text(o *object, key string) string {
	value, _ := o.get(key)
	s, _ := value.(string)
	return s
}

func sources(root string, args []string) ([]string, error) {
	if len(args) > 0 {
		return args, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, entry := range entries {
		dir := filepath.Join(root, entry.Name())
		if !entry.IsDir() || !exists(filepath.Join(dir, "package.json")) || !exists(filepath.Join(dir, "blueprint.json")) {
			continue
		}
		out = append(out, dir)
	}
	if len(out) == 0 {
		return nil, errors.New("no package sources found")
	}
	return out, nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func pack(dir string) (packed, error) {
	descriptorFile, blueprintFile := filepath.Join(dir, "package.json"), filepath.Join(dir, "blueprint.json")
	if !exists(descriptorFile) || !exists(blueprintFile) {
		return packed{}, fmt.Errorf("%s: package.json and blueprint.json are required", dir)
	}
	fields, err := readObject(descriptorFile)
	if err != nil {
		return packed{}, err
	}
	_, generated := fields.get("blueprint")
	if _, digested := fields.get("artifactDigest"); generated || digested {
		return packed{}, fmt.Errorf("%s: blueprint and artifactDigest are generated fields", descriptorFile)
	}
	id := text(fields, "id")
	if id == "" {
		return packed{}, fmt.Errorf("%s: id is required", descriptorFile)
	}
	name := strings.ReplaceAll(id, ".", "-") + ".json"
	if value, ok := fields.get("artifact"); ok && value != nil {
		if name, ok = value.(string); !ok {
			return packed{}, fmt.Errorf("%s: artifact must be a file name", descriptorFile)
		}
	}
	if !strings.HasSuffix(name, ".json") {
		return packed{}, fmt.Errorf("%s: artifact must end in .json", descriptorFile)
	}
	if strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
		return packed{}, fmt.Errorf("%s: artifact must be a JSON file name", dir)
	}
	fields.remove("artifact")

	source, err := readObject(blueprintFile)
	if err != nil {
		return packed{}, err
	}
	rootsValue, _ := source.get("roots")
	entriesValue, _ := source.get("assets")
	_, rootsOK := rootsValue.([]any)
	entries, assetsOK := entriesValue.([]any)
	if !rootsOK || !assetsOK {
		return packed{}, fmt.Errorf("%s: roots and assets must be arrays", blueprintFile)
	}

	assets := []any{}
	digests := map[string]string{}
	for _, value := range entries {
		entry := asObject(value)
		if entry == nil {
			return packed{}, fmt.Errorf("%s: each asset needs ref and path, not body", blueprintFile)
		}
		refValue, _ := entry.get("ref")
		ref := asObject(refValue)
		path := text(entry, "path")
		if _, hasBody := entry.get("body"); ref == nil || path == "" || hasBody {
			return packed{}, fmt.Errorf("%s: each asset needs ref and path, not body", blueprintFile)
		}
		key := text(ref, "kind") + "/" + text(ref, "name")
		if _, repeated := digests[key]; repeated {
			return packed{}, fmt.Errorf("%s: duplicate asset %s", blueprintFile, key)
		}
		asset := &object{}
		asset.set("ref", ref)
		for _, field := range []string{"contractVersion", "sourceVersion", "requires", "provides"} {
			if v, ok := entry.get(field); ok {
				asset.set(field, v)
			}
		}
		file, err := inside(dir, path)
		if err != nil {
			return packed{}, err
		}
		body, err := readJSON(file)
		if err != nil {
			return packed{}, err
		}
		asset.set("body", body)
		assets = append(assets, asset)
		digests[key] = digest(stringify(asset))
	}

	blueprint := &object{}
	blueprint.set("roots", rootsValue)
	blueprint.set("assets", assets)
	if value, ok := source.get("guide"); ok {
		guide, _ := value.(string)
		file, err := inside(dir, guide)
		if err != nil {
			return packed{}, err
		}
		data, err := os.ReadFile(file)
		if err != nil {
			return packed{}, err
		}
		blueprint.set("guide", string(data))
	}
	if value, ok := source.get("setup"); ok {
		setup, err := readSetup(dir, value)
		if err != nil {
			return packed{}, fmt.Errorf("%s: %w", blueprintFile, err)
		}
		blueprint.set("setup", setup)
	}

	fields.set("blueprint", blueprint)
	artifact := stringify(fields)
	lock := &object{}
	lock.set("format", 1.0)
	lock.set("package", id)
	lock.set("version", text(fields, "version"))
	requires, ok := fields.get("requires")
	if !ok || requires == nil {
		requires = []any{}
	}
	lock.set("requires", requires)
	lock.set("artifact", digest(artifact))
	keys := make([]string, 0, len(digests))
	for key := range digests {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	locked := &object{}
	for _, key := range keys {
		locked.set(key, digests[key])
	}
	lock.set("assets", locked)
	return packed{
		id: id, version: text(fields, "version"), assets: len(assets),
		artifact: artifact, artifactPath: filepath.Join(filepath.Dir(dir), name),
		lock: stringify(lock) + "\n", lockPath: filepath.Join(dir, "package.lock.json"),
	}, nil
}

func readSetup(dir string, value any) (*object, error) {
	setup := asObject(value)
	if setup == nil || text(setup, "steps") == "" {
		return nil, errors.New("setup needs a steps path")
	}
	file, err := inside(dir, text(setup, "steps"))
	if err != nil {
		return nil, err
	}
	stepsFile, err := readObject(file)
	if err != nil {
		return nil, err
	}
	steps, _ := stepsFile.get("steps")
	if _, ok := steps.([]any); !ok {
		return nil, fmt.Errorf("%s: expected { steps: [] }", text(setup, "steps"))
	}
	var roles any = []any{}
	if rolesValue, ok := setup.get("roles"); ok {
		path, _ := rolesValue.(string)
		file, err := inside(dir, path)
		if err != nil {
			return nil, err
		}
		if roles, err = readJSON(file); err != nil {
			return nil, err
		}
		if _, ok := roles.([]any); !ok {
			return nil, fmt.Errorf("%s: expected an array", path)
		}
	}
	out := &object{}
	out.set("roles", roles)
	out.set("steps", steps)
	return out, nil
}

func build(dir string) (packed, error) {
	p, err := pack(dir)
	if err != nil {
		return p, err
	}
	if err := os.WriteFile(p.artifactPath, []byte(p.artifact), 0o644); err != nil {
		return p, err
	}
	return p, os.WriteFile(p.lockPath, []byte(p.lock), 0o644)
}

func check(dir string) (packed, error) {
	p, err := pack(dir)
	if err != nil {
		return p, err
	}
	artifact, err := os.ReadFile(p.artifactPath)
	if err != nil {
		return p, fmt.Errorf("%s: missing generated artifact", p.artifactPath)
	}
	if string(artifact) != p.artifact {
		return p, fmt.Errorf("%s: generated artifact is stale; run make packages", p.artifactPath)
	}
	if lock, err := os.ReadFile(p.lockPath); err != nil || string(lock) != p.lock {
		return p, fmt.Errorf("%s: generated lock is stale; run make packages", p.lockPath)
	}
	return p, nil
}
