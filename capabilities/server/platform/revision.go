package platform

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"slices"
	"strconv"
	"strings"
)

// ReleaseAsset is a candidate input, not a public discovery Definition. Body
// must be the complete owner-produced descriptor (including permissions and
// other execution semantics), never a member-filtered API projection.
type ReleaseAsset struct {
	Ref             AssetRef        `json:"ref"`
	ContractVersion int             `json:"contractVersion"`
	SourceVersion   string          `json:"sourceVersion"`
	Requires        []AssetRef      `json:"requires"`
	Body            json.RawMessage `json:"body"`
}

// FlowReleaseDescriptor keeps the owner's complete definition and the
// bindings extracted from its compiled native flow. It is a release format,
// not an interpreter; the flow and definition owners still validate/run it.
type FlowReleaseDescriptor struct {
	Name       string          `json:"name"`
	Subject    AssetRef        `json:"subject"`
	Actions    []AssetRef      `json:"actions"`
	Definition json.RawMessage `json:"definition"`
}

// PageReleaseAsset is the single descriptor path for a code page and a page
// assembled by the builder. Its bindings become closure dependencies.
func PageReleaseAsset(app, sourceVersion string, page Page) (ReleaseAsset, error) {
	body, err := json.Marshal(page)
	if err != nil {
		return ReleaseAsset{}, err
	}
	requires := []AssetRef{page.Object}
	requires = append(requires, page.Actions...)
	for _, section := range page.Sections {
		if section.Object.Name != "" {
			requires = append(requires, section.Object)
		}
		if section.Query.Name != "" {
			requires = append(requires, section.Query)
		}
		requires = append(requires, section.Actions...)
	}
	slices.SortFunc(requires, compareRef)
	requires = slices.Compact(requires)
	return ReleaseAsset{Ref: AssetRef{App: app, Kind: AssetPage, Name: page.Name},
		ContractVersion: 1, SourceVersion: sourceVersion, Requires: requires, Body: body}, nil
}

// ApplicationReleaseAsset binds an application's ordered pages. A different
// navigation order changes its hash, while the closure includes every page.
func ApplicationReleaseAsset(app, sourceVersion string, application Application) (ReleaseAsset, error) {
	body, err := json.Marshal(application)
	if err != nil {
		return ReleaseAsset{}, err
	}
	requires := make([]AssetRef, 0, len(application.Pages))
	for _, name := range application.Pages {
		requires = append(requires, AssetRef{App: app, Kind: AssetPage, Name: name})
	}
	slices.SortFunc(requires, compareRef)
	requires = slices.Compact(requires)
	return ReleaseAsset{Ref: AssetRef{App: app, Kind: AssetApp, Name: application.Name},
		ContractVersion: 1, SourceVersion: sourceVersion, Requires: requires, Body: body}, nil
}

// ReleaseCandidate is a read-only closure of exact format-1 bytes. Publishing,
// storing and activating it require the separate accepted-result boundary.
type ReleaseCandidate struct {
	ID     string
	Bytes  []byte
	Assets []ReleaseAsset
}

type releaseBytes struct {
	Format int            `json:"format"`
	Assets []ReleaseAsset `json:"assets"`
}

// Candidate closes roots over pinned assets. Reciprocal object references are
// one semantic graph; structural cycles, missing or duplicate references,
// malformed descriptors and ambiguous JSON object keys are rejected.
// The same logical input has the same bytes regardless of map key, declaration
// or dependency order; semantic changes produce a new content ID.
func Candidate(roots []AssetRef, available []ReleaseAsset) (ReleaseCandidate, error) {
	var empty ReleaseCandidate
	if len(roots) == 0 {
		return empty, fmt.Errorf("a release needs at least one root")
	}
	lookup := make(map[AssetRef]ReleaseAsset, len(available))
	for _, asset := range available {
		if err := asset.Ref.Check(); err != nil {
			return empty, err
		}
		if _, found := lookup[asset.Ref]; found {
			return empty, fmt.Errorf("asset %s is declared twice", asset.Ref)
		}
		lookup[asset.Ref] = asset
	}
	visiting := map[AssetRef]bool{}
	visited := map[AssetRef]bool{}
	var closed []ReleaseAsset
	var visit func(AssetRef, AssetRef) error
	visit = func(ref, from AssetRef) error {
		if err := ref.Check(); err != nil {
			return err
		}
		if visiting[ref] {
			// Objects can reference one another (for example an order's SFCs
			// and an SFC's order). They form one closed semantic graph, not a
			// recursive publication step. Keep both edges in the exact bytes.
			// A self-dependency or a structural page/action cycle is invalid.
			if from.Kind == AssetObject && ref.Kind == AssetObject && from != ref {
				return nil
			}
			return fmt.Errorf("release dependency cycle at %s", ref)
		}
		if visited[ref] {
			return nil
		}
		asset, found := lookup[ref]
		if !found {
			return fmt.Errorf("release requires missing asset %s", ref)
		}
		if asset.SourceVersion == "" || asset.ContractVersion <= 0 {
			return fmt.Errorf("release asset %s has no pinned source or contract", ref)
		}
		body, err := canonicalBody(asset.Body)
		if err != nil {
			return fmt.Errorf("release asset %s: %w", ref, err)
		}
		if err := checkReleaseBindings(ref, body, asset.Requires); err != nil {
			return err
		}
		visiting[ref] = true
		deps := slices.Clone(asset.Requires)
		slices.SortFunc(deps, compareRef)
		for i, dep := range deps {
			if i > 0 && dep == deps[i-1] {
				return fmt.Errorf("release asset %s repeats dependency %s", ref, dep)
			}
			if err := visit(dep, ref); err != nil {
				return fmt.Errorf("%s: %w", ref, err)
			}
		}
		visiting[ref] = false
		visited[ref] = true
		asset.Requires = deps
		if asset.Requires == nil {
			asset.Requires = []AssetRef{}
		}
		asset.Body = body
		closed = append(closed, asset)
		return nil
	}
	for _, root := range roots {
		if err := visit(root, AssetRef{}); err != nil {
			return empty, err
		}
	}
	slices.SortFunc(closed, func(a, b ReleaseAsset) int { return compareRef(a.Ref, b.Ref) })
	raw, err := json.Marshal(releaseBytes{Format: 1, Assets: closed})
	if err != nil {
		return empty, fmt.Errorf("encode release candidate: %w", err)
	}
	hash := sha256.Sum256(raw)
	return ReleaseCandidate{ID: "sha256-v1:" + hex.EncodeToString(hash[:]), Bytes: raw, Assets: closed}, nil
}

// ReadCandidate verifies both the digest and the exact canonical bytes before
// a saved candidate can be used. It does not publish or activate that version.
func ReadCandidate(id string, raw []byte) (ReleaseCandidate, error) {
	var wire releaseBytes
	if len(raw) == 0 || len(raw) > 16<<20 {
		return ReleaseCandidate{}, fmt.Errorf("release candidate is empty or exceeds 16 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return ReleaseCandidate{}, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF || wire.Format != 1 {
		return ReleaseCandidate{}, fmt.Errorf("invalid release format or trailing data")
	}
	roots := make([]AssetRef, len(wire.Assets))
	for i, asset := range wire.Assets {
		roots[i] = asset.Ref
	}
	checked, err := Candidate(roots, wire.Assets)
	if err != nil {
		return ReleaseCandidate{}, err
	}
	if checked.ID != id || !bytes.Equal(checked.Bytes, raw) {
		return ReleaseCandidate{}, fmt.Errorf("release candidate identity or canonical bytes differ")
	}
	return checked, nil
}

// The declared graph must include dependencies that the canonical page and
// application descriptors actually name. Additional dependencies are allowed
// for owner-specific semantics (references, protocols, code contracts).
func checkReleaseBindings(ref AssetRef, body []byte, declared []AssetRef) error {
	var identity struct {
		Name   string `json:"name"`
		Type   string `json:"type"`
		Schema string `json:"schema"`
	}
	if err := json.Unmarshal(body, &identity); err != nil {
		return err
	}
	want := identity.Name
	switch ref.Kind {
	case AssetObject:
		want = identity.Type
	case AssetAction:
		want = identity.Schema
	}
	if want != ref.Name {
		return fmt.Errorf("release asset %s descriptor identity differs", ref)
	}
	var required []AssetRef
	switch ref.Kind {
	case AssetFlow:
		var flow FlowReleaseDescriptor
		if err := json.Unmarshal(body, &flow); err != nil {
			return fmt.Errorf("release flow %s: %w", ref, err)
		}
		if flow.Subject.Kind != AssetObject || !json.Valid(flow.Definition) || len(flow.Definition) == 0 || flow.Definition[0] != '{' {
			return fmt.Errorf("release flow %s needs a subject object and complete definition", ref)
		}
		required = append(required, flow.Subject)
		for _, action := range flow.Actions {
			if action.Kind != AssetAction {
				return fmt.Errorf("release flow %s binds a non-action", ref)
			}
			required = append(required, action)
		}
	case AssetPage:
		var page Page
		if err := json.Unmarshal(body, &page); err != nil {
			return fmt.Errorf("release page %s: %w", ref, err)
		}
		required = append(required, page.Object)
		required = append(required, page.Actions...)
		for _, section := range page.Sections {
			if section.Object.Name != "" {
				required = append(required, section.Object)
			}
			if section.Query.Name != "" {
				required = append(required, section.Query)
			}
			required = append(required, section.Actions...)
		}
	case AssetApp:
		var app Application
		if err := json.Unmarshal(body, &app); err != nil {
			return fmt.Errorf("release application %s: %w", ref, err)
		}
		for _, name := range app.Pages {
			required = append(required, AssetRef{App: ref.App, Kind: AssetPage, Name: name})
		}
	}
	for _, dep := range required {
		if err := dep.Check(); err != nil {
			return fmt.Errorf("release asset %s has invalid binding: %w", ref, err)
		}
		if !slices.Contains(declared, dep) {
			return fmt.Errorf("release asset %s omits bound dependency %s", ref, dep)
		}
	}
	return nil
}

func canonicalBody(raw []byte) ([]byte, error) {
	if len(raw) == 0 || len(raw) > 1<<20 {
		return nil, fmt.Errorf("descriptor is empty or exceeds 1 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	value, err := readJSONValue(decoder)
	if err != nil {
		return nil, fmt.Errorf("invalid descriptor JSON: %w", err)
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, fmt.Errorf("descriptor JSON has trailing content")
	}
	if _, ok := value.(map[string]any); !ok {
		return nil, fmt.Errorf("descriptor must be a JSON object")
	}
	return json.Marshal(value) // encoding/json sorts object keys recursively.
}

func readJSONValue(d *json.Decoder) (any, error) {
	token, err := d.Token()
	if err != nil {
		return nil, err
	}
	switch token {
	case json.Delim('{'):
		object := map[string]any{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return nil, err
			}
			name := key.(string)
			if _, exists := object[name]; exists {
				return nil, fmt.Errorf("duplicate JSON key %q", name)
			}
			object[name], err = readJSONValue(d)
			if err != nil {
				return nil, err
			}
		}
		_, err := d.Token()
		return object, err
	case json.Delim('['):
		array := []any{}
		for d.More() {
			item, err := readJSONValue(d)
			if err != nil {
				return nil, err
			}
			array = append(array, item)
		}
		_, err := d.Token()
		return array, err
	default:
		if number, ok := token.(json.Number); ok {
			return canonicalNumber(number)
		}
		return token, nil
	}
}

// JSON number spelling is not semantic: 1, 1.00 and 1e0 name one value.
// Retain arbitrary integer precision instead of passing through float64.
func canonicalNumber(number json.Number) (json.Number, error) {
	text := string(number)
	if len(text) > 4096 {
		return "", fmt.Errorf("descriptor number exceeds canonical range")
	}
	if i := strings.IndexAny(text, "eE"); i >= 0 {
		exponent, err := strconv.Atoi(text[i+1:])
		if err != nil || exponent < -1024 || exponent > 1024 {
			return "", fmt.Errorf("descriptor exponent exceeds canonical range")
		}
	}
	value, ok := new(big.Rat).SetString(text)
	if !ok {
		return "", fmt.Errorf("invalid descriptor number %q", text)
	}
	if value.IsInt() {
		return json.Number(value.Num().String()), nil
	}
	denominator := new(big.Int).Set(value.Denom())
	two, five := big.NewInt(2), big.NewInt(5)
	quotient, remainder := new(big.Int), new(big.Int)
	precision := 0
	for _, factor := range []*big.Int{two, five} {
		count := 0
		for {
			quotient.QuoRem(denominator, factor, remainder)
			if remainder.Sign() != 0 {
				break
			}
			denominator.Set(quotient)
			count++
		}
		if count > precision {
			precision = count
		}
	}
	if denominator.Cmp(big.NewInt(1)) != 0 || precision > 1024 {
		return "", fmt.Errorf("descriptor number exceeds canonical range")
	}
	decimal := strings.TrimRight(value.FloatString(precision), "0")
	return json.Number(strings.TrimSuffix(decimal, ".")), nil
}

func compareRef(a, b AssetRef) int {
	if a.App != b.App {
		return compareString(a.App, b.App)
	}
	if a.Kind != b.Kind {
		return compareString(string(a.Kind), string(b.Kind))
	}
	return compareString(a.Name, b.Name)
}

func compareString(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// CandidateDiff reports exact assets added, removed or changed. It verifies
// both inputs rather than trusting mutable in-memory copies of their assets.
func CandidateDiff(before, after ReleaseCandidate) (added, removed, changed []AssetRef, err error) {
	before, err = ReadCandidate(before.ID, before.Bytes)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("previous candidate: %w", err)
	}
	after, err = ReadCandidate(after.ID, after.Bytes)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("next candidate: %w", err)
	}
	prior := map[AssetRef][]byte{}
	for _, asset := range before.Assets {
		raw, _ := json.Marshal(asset)
		prior[asset.Ref] = raw
	}
	for _, asset := range after.Assets {
		raw, _ := json.Marshal(asset)
		old, found := prior[asset.Ref]
		if !found {
			added = append(added, asset.Ref)
		} else if !bytes.Equal(old, raw) {
			changed = append(changed, asset.Ref)
		}
		delete(prior, asset.Ref)
	}
	for ref := range prior {
		removed = append(removed, ref)
	}
	slices.SortFunc(added, compareRef)
	slices.SortFunc(removed, compareRef)
	slices.SortFunc(changed, compareRef)
	return added, removed, changed, nil
}
