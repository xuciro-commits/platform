package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"platformserver/host"
	"platformserver/kernel/protocol"
	"platformserver/platform"
	packagedesc "platformserver/services/devops/descriptor"
	"platformserver/services/ontology"
	"platformserver/services/resources/publication"
)

const (
	manifestFile = "package.yaml"
	lockFile     = "package.lock.json"
	setupFile    = "setup.yaml"
	seedFile     = "seed.yaml"
	dataDir      = "data"
	dataChunk    = 24 << 10
	guideFile    = "docs/guide.md"
	lockFormat   = 2
)

var kindDirectories = map[string]string{
	"object": "objects", "action": "actions", "query": "queries", "page": "pages", "app": "applications",
	"flow": "flows", "function": "functions", "compute": "functions/compute", "agent": "agents",
	"connection": "integrations/connections", "source": "integrations/sources", "listener": "integrations/listeners",
	"writeback": "integrations/writebacks", "dataset": "integrations/datasets", "mediaset": "integrations/media-sets",
	"schedule": "integrations/schedules", "pipeline": "integrations/pipelines", "matching": "integrations/matching",
	"observation": "integrations/observations", "sample-set": "tests/sample-sets", "test-plan": "tests/test-plans",
	"evaluation-set": "tests/evaluations", "link-type": "objects/link-types", "property-type": "objects/property-types",
	"value-type": "objects/value-types", "interface": "objects/interfaces", "object-type-group": "objects/groups",
	"protocol": "protocols", "product": "products", "contract": "contracts", "implementation": "implementations",
}

var defaultContract = map[string]int{"link-type": 4}

var sourceFamily = map[string]string{
	"agent": "agent", "connection": "connection", "dataset": "dataset", "evaluation-set": "evaluation", "function": "function",
	"link-type": "link", "matching": "matching", "observation": "observation", "query": "query", "sample-set": "samples",
	"source": "source", "test-plan": "tests", "writeback": "writeback", "listener": "listener", "mediaset": "mediaset",
	"schedule": "schedule", "pipeline": "pipeline", "contract": "contract", "implementation": "implementation",
}

func firstSourceVersion(kind string) string {
	if family, ok := sourceFamily[kind]; ok {
		return "1." + family + "-1"
	}
	return "1"
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

type lockedAsset struct {
	Digest          string                   `json:"digest"`
	ContractVersion int                      `json:"contractVersion"`
	SourceVersion   string                   `json:"sourceVersion"`
	SchemaVersions  []platform.SchemaVersion `json:"schemaVersions,omitempty"`
}

type lock struct {
	Format   int                    `json:"format"`
	Package  string                 `json:"package"`
	Version  string                 `json:"version"`
	Requires []string               `json:"requires"`
	Artifact string                 `json:"artifact"`
	Assets   map[string]lockedAsset `json:"assets"`
}

func (l lock) text() string {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(l); err != nil {
		panic(err)
	}
	return out.String()
}

type built struct {
	id, version  string
	title        string
	description  string
	artifact     string
	artifactName string
	lock         lock
	assets       int
	listing      *protocol.PackageListing
	descriptor   platform.PackageDescriptor
}

type resource struct {
	ref  platform.AssetRef
	path string
	body any
}

func digest(text string) string {
	sum := sha256.Sum256([]byte(text))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func readObject(path string) (*object, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	value, err := parseJSON(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if out := asObject(value); out != nil {
		return out, nil
	}
	return nil, fmt.Errorf("%s: expected an object", path)
}

func asObject(value any) *object {
	switch v := value.(type) {
	case *object:
		return v
	}
	return nil
}

func text(o *object, key string) string {
	value, _ := o.get(key)
	s, _ := value.(string)
	return s
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func toValue(v any) any {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	value, err := parseJSON(raw)
	if err != nil {
		panic(err)
	}
	return value
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
		if dir := filepath.Join(root, entry.Name()); entry.IsDir() && exists(filepath.Join(dir, manifestFile)) {
			out = append(out, dir)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("no package sources found")
	}
	return out, nil
}

func fileName(name string) string {
	return unsafeName.ReplaceAllString(strings.TrimPrefix(name, "build."), "-")
}

func classify(rel string) (string, bool) {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	dir := strings.Join(parts[:len(parts)-1], "/")
	for kind, directory := range kindDirectories {
		if directory == dir {
			return kind, true
		}
	}
	switch {
	case len(parts) == 3 && parts[0] == "assets":
		return parts[1], true
	case len(parts) == 3 && parts[0] == "objects" && parts[2] == "object.yaml":
		return "object", true
	case len(parts) == 3 && parts[0] == "objects" && parts[2] == "page.yaml":
		return "page", true
	case len(parts) == 4 && parts[0] == "objects" && parts[2] == "actions":
		return "action", true
	}
	return "", false
}

func refName(kind string, body *object) string {
	switch kind {
	case "object":
		if namespace := text(body, "namespace"); namespace != "" {
			return namespace + "." + text(body, "name")
		}
		return "build." + text(body, "name")
	case "action":
		return text(body, "schema")
	case "protocol":
		var descriptor platform.ProtocolReleaseDescriptor
		if json.Unmarshal([]byte(stringify(body)), &descriptor) == nil {
			return protocol.ContractAssetName(descriptor.Name, descriptor.Version)
		}
		return ""
	case "contract":
		var contract protocol.ProtocolContract
		if json.Unmarshal([]byte(stringify(body)), &contract) == nil {
			return contract.AssetName()
		}
		return ""
	}
	return text(body, "name")
}

func layout(assets []resource) (map[platform.AssetRef]string, error) {
	objects := map[string]string{}
	for _, asset := range assets {
		if asset.ref.Kind == "object" {
			dir := fileName(text(asObject(asset.body), "name"))
			objects[asset.ref.Name], objects[text(asObject(asset.body), "name")] = dir, dir
		}
	}
	out := map[platform.AssetRef]string{}
	used := map[string]platform.AssetRef{}
	for _, asset := range assets {
		ref, body := asset.ref, asObject(asset.body)
		directory, ok := kindDirectories[string(ref.Kind)]
		if !ok {
			directory = "assets/" + string(ref.Kind)
		}
		path := directory + "/" + fileName(ref.Name) + ".yaml"
		if ref.Kind == protocol.AssetContract || ref.Kind == platform.AssetProtocol {
			if base, major, versioned := strings.Cut(ref.Name, "@"); versioned {
				path = directory + "/" + fileName(base) + "@" + fileName(major) + ".yaml"
			}
		}
		switch ref.Kind {
		case "object":
			path = "objects/" + objects[ref.Name] + "/object.yaml"
		case "page":
			if dir, ok := objects[ref.Name]; ok {
				path = "objects/" + dir + "/page.yaml"
			}
		case "action":
			target := text(body, "target")
			if dir, ok := objects[target]; ok && strings.HasPrefix(ref.Name, target+".") {
				path = "objects/" + dir + "/actions/" + fileName(strings.TrimPrefix(ref.Name, target+".")) + ".yaml"
			}
		}
		if kind, ok := classify(path); !ok || kind != string(ref.Kind) {
			return nil, fmt.Errorf("%s would be read back as %q from %s", ref, kind, path)
		}
		if prior, taken := used[path]; taken {
			return nil, fmt.Errorf("%s and %s both map to %s", prior, ref, path)
		}
		used[path], out[ref] = ref, path
	}
	return out, nil
}

func split(artifactPath, dir string) error {
	raw, err := os.ReadFile(artifactPath)
	if err != nil {
		return err
	}
	value, err := parseJSON(raw)
	if err != nil {
		return err
	}
	descriptor := asObject(value)
	blueprintValue, _ := descriptor.get("blueprint")
	blueprint := asObject(blueprintValue)
	if blueprint == nil {
		return fmt.Errorf("%s: not a package with a blueprint", artifactPath)
	}
	if exists(dir) {
		return fmt.Errorf("%s already exists", dir)
	}
	manifest := &object{}
	splitLocales := asObject(descriptor.values["languages"])
	for _, key := range descriptor.order() {
		if key != "blueprint" && key != "languages" {
			manifest.set(key, descriptor.values[key])
		}
	}
	if raw, present := manifest.get("contracts"); present {
		if contracts := asObject(raw); contracts != nil {
			contracts.remove("provides")
		}
	}
	roots, _ := blueprint.get("roots")
	manifest.set("roots", roots)
	manifest.set("artifact", filepath.Base(artifactPath))

	prior := lock{Assets: map[string]lockedAsset{}}
	var assets []resource
	assetValues, _ := blueprint.get("assets")
	list, _ := assetValues.([]any)
	for _, item := range list {
		asset := asObject(item)
		refValue, _ := asset.get("ref")
		ref := asObject(refValue)
		body, _ := asset.get("body")
		contract, _ := asset.get("contractVersion")
		number, _ := contract.(float64)
		entry := lockedAsset{ContractVersion: int(number), SourceVersion: text(asset, "sourceVersion")}
		key := text(ref, "kind") + "/" + text(ref, "name")
		if bodyObject := asObject(body); text(ref, "kind") == "object" && bodyObject != nil {
			if history, ok := bodyObject.get("schemaVersions"); ok {
				encoded := []byte(stringify(history))
				if err := json.Unmarshal(encoded, &entry.SchemaVersions); err != nil {
					return fmt.Errorf("%s: %w", key, err)
				}
				bodyObject.remove("schemaVersions")
			}
		}
		if bodyObject := asObject(body); splitLocales != nil && bodyObject != nil && (text(ref, "kind") == "app" || text(ref, "kind") == "contract") {
			bodyObject.remove("languages")
		}
		prior.Assets[key] = entry
		assets = append(assets, resource{ref: platform.AssetRef{App: text(ref, "app"), Kind: platform.AssetKind(text(ref, "kind")), Name: text(ref, "name")}, body: body})
	}
	paths, err := layout(assets)
	if err != nil {
		return err
	}
	for _, asset := range assets {
		if err := writeYAML(filepath.Join(dir, paths[asset.ref]), asset.body); err != nil {
			return err
		}
	}
	if guide := text(blueprint, "guide"); guide != "" {
		if err := writeFile(filepath.Join(dir, guideFile), guide); err != nil {
			return err
		}
	}
	if setup, ok := blueprint.get("setup"); ok {
		if err := writeYAML(filepath.Join(dir, setupFile), setup); err != nil {
			return err
		}
	}
	if seed, ok := blueprint.get("seed"); ok {
		items, _ := seed.([]any)
		demo, data := []any{}, map[string][]any{}
		var types []string
		for _, item := range items {
			entry := asObject(item)
			if entry == nil || text(entry, "kind") != "data" {
				demo = append(demo, item)
				continue
			}
			typ := text(entry, "type")
			if _, seen := data[typ]; !seen {
				types = append(types, typ)
			}
			rows, _ := entry.get("rows")
			list, _ := rows.([]any)
			data[typ] = append(data[typ], list...)
		}
		if len(demo) > 0 {
			doc := &object{}
			doc.set("records", demo)
			if err := writeYAML(filepath.Join(dir, seedFile), doc); err != nil {
				return err
			}
		}
		for _, typ := range types {
			doc := &object{}
			doc.set("type", typ)
			doc.set("records", data[typ])
			if err := writeYAML(filepath.Join(dir, dataDir, fileName(typ[strings.LastIndex(typ, ".")+1:])+".yaml"), doc); err != nil {
				return err
			}
		}
	}
	if splitLocales != nil {
		for _, lang := range splitLocales.order() {
			if err := writeYAML(filepath.Join(dir, localesDir, lang+".yaml"), splitLocales.values[lang]); err != nil {
				return err
			}
		}
	}
	if err := writeYAML(filepath.Join(dir, manifestFile), manifest); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(dir, lockFile), prior.text()); err != nil {
		return err
	}
	_, err = lockPackage(dir)
	return err
}

func readLock(dir string) (lock, error) {
	out := lock{Assets: map[string]lockedAsset{}}
	raw, err := os.ReadFile(filepath.Join(dir, lockFile))
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("%s: %w", lockFile, err)
	}
	if out.Assets == nil {
		out.Assets = map[string]lockedAsset{}
	}
	return out, nil
}

func readData(dir string) ([]any, error) {
	paths, err := filepath.Glob(filepath.Join(dir, dataDir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	slices.Sort(paths)
	var out []any
	for _, path := range paths {
		value, err := readYAML(path)
		if err != nil {
			return nil, err
		}
		doc := asObject(value)
		if doc == nil {
			return nil, fmt.Errorf("%s: needs a type and a list of records", path)
		}
		list, _ := doc.get("records")
		rows, ok := list.([]any)
		typ := text(doc, "type")
		if typ == "" || !ok || len(rows) == 0 {
			return nil, fmt.Errorf("%s: needs a type and a list of records", path)
		}
		chunk, size := []any{}, 0
		flush := func() {
			entry := &object{}
			entry.set("kind", "data")
			entry.set("type", typ)
			entry.set("rows", chunk)
			out = append(out, entry)
			chunk, size = []any{}, 0
		}
		for _, row := range rows {
			if asObject(row) == nil || text(asObject(row), "id") == "" {
				return nil, fmt.Errorf("%s: every record is an object with its own id", path)
			}
			n := len(stringify(row)) + 1
			if size+n > dataChunk && len(chunk) > 0 {
				flush()
			}
			chunk, size = append(chunk, row), size+n
		}
		flush()
	}
	return out, nil
}

func requiredTypes(dir string, requires []string) (map[string]map[string]exprField, error) {
	out := map[string]map[string]exprField{}
	for _, need := range requires {
		source := filepath.Join(filepath.Dir(dir), requiredID(need))
		if !exists(filepath.Join(source, manifestFile)) {
			return nil, fmt.Errorf("%s: requires %s, but there is no package source at %s", manifestFile, need, source)
		}
		resources, err := readResources(source)
		if err != nil {
			return nil, err
		}
		for _, r := range resources {
			if r.ref.Kind == "object" {
				out[r.ref.Name] = objectFields(asObject(r.body))
			}
		}
	}
	return out, nil
}

func readResources(dir string) ([]resource, error) {
	var out []resource
	seen := map[platform.AssetRef]string{}
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Ext(path) != ".yaml" {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		if rel == manifestFile || rel == setupFile || rel == seedFile || rel == tryFile || strings.HasPrefix(filepath.ToSlash(rel), localesDir+"/") || strings.HasPrefix(filepath.ToSlash(rel), dataDir+"/") {
			return nil
		}
		kind, ok := classify(rel)
		if !ok {
			return fmt.Errorf("%s: not a resource location; see the package format", rel)
		}
		value, err := readYAML(path)
		if err != nil {
			return err
		}
		body := asObject(value)
		if body == nil {
			return fmt.Errorf("%s: a resource is a mapping", rel)
		}
		ref := platform.AssetRef{App: "build", Kind: platform.AssetKind(kind), Name: refName(kind, body)}
		if kind == "action" && !body.has("schema") && strings.HasPrefix(filepath.ToSlash(rel), "objects/") {
			ref.Name = "@" + filepath.ToSlash(rel)
		}
		if strings.HasSuffix(ref.Name, ".") || ref.Name == "" || ref.Name == "build." {
			return fmt.Errorf("%s: the resource needs a name", rel)
		}
		if prior, repeated := seen[ref]; repeated {
			return fmt.Errorf("%s and %s both define %s", prior, rel, ref)
		}
		seen[ref] = rel
		out = append(out, resource{ref: ref, path: rel, body: body})
		return nil
	})
	slices.SortFunc(out, func(a, b resource) int { return publication.CompareAssetRef(a.ref, b.ref) })
	return out, err
}

func previousObject(previous *platform.PackageDescriptor, ref platform.AssetRef) *ontology.Object {
	if previous == nil || previous.Blueprint == nil {
		return nil
	}
	for _, asset := range previous.Blueprint.Assets {
		if asset.Ref == ref {
			var object ontology.Object
			if json.Unmarshal(asset.Body, &object) == nil {
				return &object
			}
		}
	}
	return nil
}

func schemaHistory(body *object, history []platform.SchemaVersion, previous *ontology.Object) ([]platform.SchemaVersion, error) {
	var current ontology.Object
	if err := json.Unmarshal([]byte(stringify(body)), &current); err != nil {
		return nil, err
	}
	schema, err := ontology.ObjectSchemaBody(current)
	if err != nil {
		return nil, err
	}
	version, err := publication.SchemaContentVersion(schema)
	if err != nil {
		return nil, err
	}
	var released []platform.SchemaVersion
	if previous != nil {
		released = previous.SchemaVersions
	}
	if len(history) < len(released) || !slices.Equal(history[:len(released)], released) {
		return nil, errors.New("the schema history in the lock does not continue the released one; download the released artifact and run pkg lock again")
	}
	if len(history) > 0 && history[len(history)-1].Version == version {
		return history, nil
	}
	if len(released) > 0 && released[len(released)-1].Version == version {
		return slices.Clone(released), nil
	}
	family := uint32(1)
	if len(released) > 0 {
		family = released[len(released)-1].Family
	}
	if previous != nil && !ontology.CompatibleObjectSchema(*previous, current) {
		family++
	}
	return append(slices.Clone(released), platform.SchemaVersion{Version: version, Family: family}), nil
}

func build(dir string, previous *platform.PackageDescriptor) (built, error) {
	manifestValue, err := readYAML(filepath.Join(dir, manifestFile))
	if err != nil {
		return built{}, err
	}
	manifest := asObject(manifestValue)
	if manifest == nil || text(manifest, "id") == "" || text(manifest, "version") == "" {
		return built{}, fmt.Errorf("%s: id and version are required", manifestFile)
	}
	id, version := text(manifest, "id"), text(manifest, "version")
	roots, _ := manifest.get("roots")
	if _, ok := roots.([]any); !ok {
		return built{}, fmt.Errorf("%s: roots is a list", manifestFile)
	}
	artifactName := text(manifest, "artifact")
	if artifactName == "" {
		artifactName = id + "-" + version + ".json"
	}
	manifest.remove("roots")
	manifest.remove("artifact")
	listing, err := readListing(manifest)
	if err != nil {
		return built{}, fmt.Errorf("%s: %w", manifestFile, err)
	}
	manifest.remove("listing")

	prior, err := readLock(dir)
	if err != nil {
		return built{}, err
	}
	resources, err := readResources(dir)
	if err != nil {
		return built{}, err
	}
	next := lock{Format: lockFormat, Package: id, Version: version, Requires: []string{}, Assets: map[string]lockedAsset{}}
	if requires, ok := manifest.get("requires"); ok {
		if err := json.Unmarshal([]byte(stringify(requires)), &next.Requires); err != nil {
			return built{}, fmt.Errorf("%s: requires: %w", manifestFile, err)
		}
	}
	external, err := requiredTypes(dir, next.Requires)
	if err != nil {
		return built{}, err
	}
	if resources, err = expand(dir, manifest, resources, previous != nil && previous.Version == version, external); err != nil {
		return built{}, err
	}
	resolver := host.PublicationResolver()
	assets := []any{}
	for _, r := range resources {
		key := string(r.ref.Kind) + "/" + r.ref.Name
		entry, locked := prior.Assets[key]
		if !locked {
			entry = lockedAsset{ContractVersion: max(1, defaultContract[string(r.ref.Kind)]), SourceVersion: firstSourceVersion(string(r.ref.Kind))}
		}
		body := asObject(r.body)
		if r.ref.Kind == platform.AssetFlow {
			if err := flowBindings(body, entry.SourceVersion); err != nil {
				return built{}, fmt.Errorf("%s: %w", r.path, err)
			}
		}
		if r.ref.Kind == platform.AssetObject {
			if err := checkObjectActions(body); err != nil {
				return built{}, fmt.Errorf("%s: %w", r.path, err)
			}
			if _, generated := body.get("schemaVersions"); generated {
				return built{}, fmt.Errorf("%s: schemaVersions is generated into %s; remove it", r.path, lockFile)
			}
			if !locked || len(entry.SchemaVersions) > 0 {
				if entry.SchemaVersions, err = schemaHistory(body, entry.SchemaVersions, previousObject(previous, r.ref)); err != nil {
					return built{}, fmt.Errorf("%s: %w", r.path, err)
				}
				body.set("schemaVersions", toValue(entry.SchemaVersions))
			}
		}
		complete, err := resolver.WithDependencies(platform.ReleaseAsset{Ref: r.ref, ContractVersion: entry.ContractVersion, SourceVersion: entry.SourceVersion, Body: json.RawMessage(stringify(body))})
		if err != nil {
			return built{}, fmt.Errorf("%s: %w", r.path, err)
		}
		asset := &object{}
		asset.set("ref", toValue(r.ref))
		asset.set("contractVersion", float64(entry.ContractVersion))
		asset.set("sourceVersion", entry.SourceVersion)
		asset.set("requires", toValue(append([]platform.AssetRef{}, complete.Requires...)))
		if len(complete.Provides) > 0 {
			asset.set("provides", toValue(complete.Provides))
		}
		asset.set("body", body)
		entry.Digest = digest(stringify(asset))
		next.Assets[key] = entry
		assets = append(assets, asset)
	}
	blueprint := &object{}
	blueprint.set("roots", roots)
	blueprint.set("assets", assets)
	if guide, err := os.ReadFile(filepath.Join(dir, guideFile)); err == nil {
		blueprint.set("guide", string(guide))
	}
	if exists(filepath.Join(dir, setupFile)) {
		setup, err := readYAML(filepath.Join(dir, setupFile))
		if err != nil {
			return built{}, err
		}
		blueprint.set("setup", setup)
	}
	if exists(filepath.Join(dir, seedFile)) {
		seedValue, err := readYAML(filepath.Join(dir, seedFile))
		if err != nil {
			return built{}, err
		}
		seedObj := asObject(seedValue)
		if seedObj == nil {
			return built{}, fmt.Errorf("%s: records is a list", seedFile)
		}
		records, ok := seedObj.get("records")
		if !ok {
			return built{}, fmt.Errorf("%s: records is a list", seedFile)
		}
		if _, ok := records.([]any); !ok {
			return built{}, fmt.Errorf("%s: records is a list", seedFile)
		}
		blueprint.set("seed", records)
	}
	data, err := readData(dir)
	if err != nil {
		return built{}, err
	}
	if len(data) > 0 {
		list, _ := blueprint.get("seed")
		items, _ := list.([]any)
		blueprint.set("seed", append(items, data...))
	}
	manifest.set("blueprint", blueprint)
	if raw, present := manifest.get("contracts"); present {
		contracts := asObject(raw)
		if contracts == nil {
			return built{}, fmt.Errorf("%s: contracts must be an object", manifestFile)
		}
		if _, manual := contracts.get("provides"); manual {
			return built{}, fmt.Errorf("%s: contracts.provides is generated from implementations", manifestFile)
		}
		var candidate platform.PackageDescriptor
		if err := json.Unmarshal([]byte(stringify(manifest)), &candidate); err != nil {
			return built{}, err
		}
		provided, err := packagedesc.ProvidedContracts(candidate)
		if err != nil {
			return built{}, err
		}
		if len(provided) > 0 {
			contracts.set("provides", toValue(provided))
		}
	}
	artifact := stringify(manifest)
	next.Artifact = digest(artifact)
	var descriptor platform.PackageDescriptor
	if err := json.Unmarshal([]byte(artifact), &descriptor); err != nil {
		return built{}, err
	}
	if err := packagedesc.Validate(descriptor, resolver.WithDependencies); err != nil {
		return built{}, err
	}
	return built{id: id, version: version, title: descriptor.Title, description: descriptor.Description, artifact: artifact, artifactName: artifactName, lock: next, assets: len(assets), listing: listing, descriptor: descriptor}, nil
}

func snapshot(dir string) (*platform.PackageDescriptor, []byte, error) {
	manifestValue, err := readYAML(filepath.Join(dir, manifestFile))
	if err != nil {
		return nil, nil, err
	}
	name := text(asObject(manifestValue), "artifact")
	if name == "" {
		return nil, nil, nil
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(dir), name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var descriptor platform.PackageDescriptor
	if err := json.Unmarshal(raw, &descriptor); err != nil {
		return nil, nil, fmt.Errorf("%s: %w", name, err)
	}
	return &descriptor, raw, nil
}

func lockPackage(dir string) (built, error) {
	previous, _, err := snapshot(dir)
	if err != nil {
		return built{}, err
	}
	b, err := build(dir, previous)
	if err != nil {
		return b, err
	}
	return b, writeFile(filepath.Join(dir, lockFile), b.lock.text())
}

func check(dir string) (built, error) {
	previous, raw, err := snapshot(dir)
	if err != nil {
		return built{}, err
	}
	b, err := build(dir, previous)
	if err != nil {
		return b, err
	}
	if current, err := os.ReadFile(filepath.Join(dir, lockFile)); err != nil || string(current) != b.lock.text() {
		return b, fmt.Errorf("%s: %s is stale; run make packages", dir, lockFile)
	}
	if previous != nil && previous.Version == b.version {
		if err := equivalent(raw, []byte(b.artifact)); err != nil {
			return b, fmt.Errorf("%s differs from the released %s@%s: %w", dir, b.id, b.version, err)
		}
	}
	return b, nil
}

func equivalent(released, rebuilt []byte) error {
	var a, b platform.PackageDescriptor
	if err := json.Unmarshal(released, &a); err != nil {
		return err
	}
	if err := json.Unmarshal(rebuilt, &b); err != nil {
		return err
	}
	if a.Blueprint == nil || b.Blueprint == nil {
		return errors.New("a blueprint is missing")
	}
	ab, bb := *a.Blueprint, *b.Blueprint
	a.Blueprint, b.Blueprint = nil, nil
	if !sameJSON(a, b) {
		return errors.New("package fields differ")
	}
	slices.SortFunc(ab.Roots, publication.CompareAssetRef)
	slices.SortFunc(bb.Roots, publication.CompareAssetRef)
	if !sameJSON(ab.Roots, bb.Roots) || ab.Guide != bb.Guide || !sameJSON(ab.Setup, bb.Setup) || !sameJSON(ab.Seed, bb.Seed) {
		return errors.New("roots, guide or setup differ")
	}
	index := map[platform.AssetRef]platform.ReleaseAsset{}
	for _, asset := range bb.Assets {
		index[asset.Ref] = asset
	}
	if len(ab.Assets) != len(bb.Assets) {
		return fmt.Errorf("%d resources released, %d rebuilt", len(ab.Assets), len(bb.Assets))
	}
	for _, want := range ab.Assets {
		got, ok := index[want.Ref]
		if !ok {
			return fmt.Errorf("%s is missing", want.Ref)
		}
		if got.ContractVersion != want.ContractVersion || got.SourceVersion != want.SourceVersion {
			return fmt.Errorf("%s changed its contract or source version", want.Ref)
		}
		x, err := publication.CanonicalAssetBody(want.Body)
		if err != nil {
			return err
		}
		y, err := publication.CanonicalAssetBody(got.Body)
		if err != nil {
			return err
		}
		if !bytes.Equal(x, y) {
			return fmt.Errorf("%s has a different body", want.Ref)
		}
	}
	return nil
}

func sameJSON(a, b any) bool {
	x, errA := json.Marshal(a)
	y, errB := json.Marshal(b)
	return errA == nil && errB == nil && bytes.Equal(x, y)
}

func readListing(manifest *object) (*protocol.PackageListing, error) {
	value, ok := manifest.get("listing")
	if !ok {
		return nil, nil
	}
	var listing protocol.PackageListing
	if err := platform.DecodeJSON([]byte(stringify(value)), &listing, "listing"); err != nil {
		return nil, err
	}
	listing.Domains, listing.Capabilities = packagedesc.Domains(listing), nil
	if err := packagedesc.CheckListing(listing); err != nil {
		return nil, err
	}
	return &listing, nil
}

type catalogDocument struct {
	Version  int                            `json:"version"`
	Title    string                         `json:"title,omitempty"`
	Packages []protocol.PackageCatalogEntry `json:"packages"`
}

func descriptorLanguages(d platform.PackageDescriptor) platform.Languages {
	out := platform.Languages{}
	merge := func(src platform.Languages) {
		for lang, dict := range src {
			if out[lang] == nil {
				out[lang] = map[string]string{}
			}
			for k, v := range dict {
				if v != "" && out[lang][k] == "" {
					out[lang][k] = v
				}
			}
		}
	}
	merge(d.Languages)
	if d.Blueprint != nil {
		for _, asset := range d.Blueprint.Assets {
			if asset.Ref.Kind == "app" || asset.Ref.Kind == "contract" {
				var holder struct {
					Languages platform.Languages `json:"languages"`
				}
				if json.Unmarshal(asset.Body, &holder) == nil {
					merge(holder.Languages)
				}
			}
		}
	}
	return out
}

func lookupLocaleText(dict map[string]string, src string) string {
	if src == "" || len(dict) == 0 {
		return ""
	}
	if tr := dict[src]; tr != "" {
		return tr
	}
	return dict[strings.ToUpper(src[:1])+src[1:]]
}

func catalogEntry(b built, url string) protocol.PackageCatalogEntry {
	var locales map[string]protocol.CatalogLocale
	for lang, dict := range descriptorLanguages(b.descriptor) {
		trTitle := lookupLocaleText(dict, b.title)
		trDesc := lookupLocaleText(dict, b.description)
		if trTitle != "" || trDesc != "" {
			if locales == nil {
				locales = map[string]protocol.CatalogLocale{}
			}
			locales[lang] = protocol.CatalogLocale{Title: trTitle, Description: trDesc}
		}
	}
	return protocol.PackageCatalogEntry{ID: b.id, Version: b.version, Title: b.title, Description: b.description, URL: url, Sha256: b.lock.Artifact, Listing: b.listing, Locales: locales}
}
