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
	Name         string          `json:"name"`
	Subject      AssetRef        `json:"subject"`
	Actions      []AssetRef      `json:"actions"`
	Definition   json.RawMessage `json:"definition"`
	Functions    []AssetBinding  `json:"functions,omitempty"`
	Operations   []AssetBinding  `json:"operations,omitempty"`
	Dependencies []AssetRef      `json:"dependencies,omitempty"`
}

// AssetBinding fixes the owner's exact source version at a dependency edge.
type AssetBinding struct {
	Ref           AssetRef `json:"ref"`
	SourceVersion string   `json:"sourceVersion"`
}

// PageReleaseAsset is the single descriptor path for a code page and a page
// assembled by the builder. Its bindings become closure dependencies.
func PageReleaseAsset(app, sourceVersion string, page Page) (ReleaseAsset, error) {
	if err := page.Document.Check(page.Sections); err != nil {
		return ReleaseAsset{}, err
	}
	if err := page.CheckRecordPorts(); err != nil {
		return ReleaseAsset{}, err
	}
	body, err := json.Marshal(page)
	if err != nil {
		return ReleaseAsset{}, err
	}
	requires := append([]AssetRef{page.Object}, page.NavigationTargets()...)
	requires = append(requires, page.Actions...)
	requires = append(requires, page.QueryReferences()...)
	for _, selection := range page.Selections {
		requires = append(requires, selection.Object)
	}
	for _, section := range page.Sections {
		if section.Function != nil {
			requires = append(requires, section.Function.Ref)
		}
		if section.Operation != nil {
			requires = append(requires, section.Operation.Ref)
		}
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

// ApplicationReleaseAsset binds ordered navigation and explicit resources.
// Frozen closure dependencies retain their original owner and exact bytes.
func ApplicationReleaseAsset(app, sourceVersion string, application Application) (ReleaseAsset, error) {
	if err := application.CheckVariables(); err != nil {
		return ReleaseAsset{}, err
	}
	if err := application.CheckResources(); err != nil {
		return ReleaseAsset{}, err
	}
	body, err := json.Marshal(application)
	if err != nil {
		return ReleaseAsset{}, err
	}
	requires := application.Dependencies(app)
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
	var path []AssetRef
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
			if pageNavigationEdge(lookup[from], ref) {
				start := slices.Index(path, ref)
				navigation := start >= 0
				for i := start; i >= 0 && i+1 < len(path); i++ {
					navigation = navigation && pageNavigationEdge(lookup[path[i]], path[i+1])
				}
				if navigation {
					return nil
				}
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
		if ref.Kind == AssetApp {
			var application Application
			if err := json.Unmarshal(body, &application); err != nil {
				return err
			}
			if err := application.CheckVariables(); err != nil {
				return err
			}
			for _, name := range application.Pages {
				pageRef := AssetRef{App: ref.App, Kind: AssetPage, Name: name}
				target, ok := lookup[pageRef]
				var page Page
				if !ok || json.Unmarshal(target.Body, &page) != nil {
					return fmt.Errorf("application page %s is unavailable", pageRef)
				}
				if err := application.CheckPageVariables(page); err != nil {
					return err
				}
			}
		}
		if ref.Kind == AssetFlow || ref.Kind == AssetPage {
			var bindings []AssetBinding
			if ref.Kind == AssetFlow {
				var flow FlowReleaseDescriptor
				if err := json.Unmarshal(body, &flow); err != nil {
					return err
				}
				bindings = append(slices.Clone(flow.Functions), flow.Operations...)
			} else {
				var page Page
				if err := json.Unmarshal(body, &page); err != nil {
					return err
				}
				if page.Document != nil {
					for _, event := range page.Document.Events {
						if event.Navigate != nil {
							target, ok := lookup[event.Navigate.Page]
							var targetPage Page
							if !ok || json.Unmarshal(target.Body, &targetPage) != nil {
								return fmt.Errorf("page navigation target %s is unavailable", event.Navigate.Page)
							}
							if err := CheckPageNavigation(page, targetPage, *event.Navigate); err != nil {
								return err
							}
						}
					}
				}
				if page.Document != nil {
					for id, plan := range page.Document.Queries {
						objectAsset, ok := lookup[plan.Object]
						var object EntityInfo
						if !ok || json.Unmarshal(objectAsset.Body, &object) != nil {
							return fmt.Errorf("page query %s object is unavailable", id)
						}
						var named *Definition
						if plan.Query != nil {
							asset, ok := lookup[plan.Query.Ref]
							var query NamedQuery
							if !ok || json.Unmarshal(asset.Body, &query) != nil {
								return fmt.Errorf("page query %s named query is unavailable", id)
							}
							named = &Definition{Ref: asset.Ref, Version: asset.SourceVersion, Query: &query}
						}
						if err := page.CheckQuerySchema(plan, object, named); err != nil {
							return fmt.Errorf("page query %s: %w", id, err)
						}
					}
				}

				for _, section := range page.Sections {
					if section.Function != nil {
						bindings = append(bindings, *section.Function)
					}
					if section.Operation != nil {
						bindings = append(bindings, *section.Operation)
					}
				}
			}
			seen := map[AssetRef]bool{}
			for _, binding := range bindings {
				dependency, ok := lookup[binding.Ref]
				if !ok || seen[binding.Ref] && ref.Kind == AssetFlow || binding.Ref.Kind != AssetFunction && binding.Ref.Kind != AssetCompute || binding.SourceVersion == "" || dependency.SourceVersion != binding.SourceVersion {
					return fmt.Errorf("%s needs exact function dependency %s at %s", ref, binding.Ref, binding.SourceVersion)
				}
				seen[binding.Ref] = true
			}
		}
		visiting[ref] = true
		path = append(path, ref)
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
		path = path[:len(path)-1]
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
	case AssetFunction:
		var function AIFunction
		if err := json.Unmarshal(body, &function); err != nil {
			return fmt.Errorf("release function %s: %w", ref, err)
		}
		if err := function.Check(); err != nil {
			return err
		}
		required = append(required, AssetRef{App: ref.App, Kind: AssetObject, Name: function.Object})
	case AssetCompute:
		var operation Operation
		if err := json.Unmarshal(body, &operation); err != nil {
			return err
		}
		if operation.Name != ref.Name {
			return fmt.Errorf("compute descriptor identity differs")
		}
		if err := operation.Check(); err != nil {
			return err
		}
	case AssetFlow:
		var flow FlowReleaseDescriptor
		if err := json.Unmarshal(body, &flow); err != nil {
			return fmt.Errorf("release flow %s: %w", ref, err)
		}
		if flow.Subject.Name != "" && flow.Subject.Kind != AssetObject || !json.Valid(flow.Definition) || len(flow.Definition) == 0 || flow.Definition[0] != '{' {
			return fmt.Errorf("release flow %s needs a valid optional subject and complete definition", ref)
		}
		if flow.Subject.Name != "" {
			required = append(required, flow.Subject)
		}
		for _, binding := range flow.Functions {
			required = append(required, binding.Ref)
		}
		for _, binding := range flow.Operations {
			if binding.Ref.Kind != AssetCompute || binding.SourceVersion == "" {
				return fmt.Errorf("release flow %s has an invalid compute binding", ref)
			}
			required = append(required, binding.Ref)
		}
		required = append(required, flow.Dependencies...)
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
		if err := page.Document.Check(page.Sections); err != nil {
			return fmt.Errorf("release page %s: %w", ref, err)
		}
		if err := page.CheckRecordPorts(); err != nil {
			return err
		}
		required = append(required, page.Object)
		required = append(required, page.NavigationTargets()...)
		required = append(required, page.Actions...)
		required = append(required, page.QueryReferences()...)
		for _, selection := range page.Selections {
			required = append(required, selection.Object)
		}
		for _, section := range page.Sections {
			if section.Function != nil {
				required = append(required, section.Function.Ref)
			}
			if section.Operation != nil {
				required = append(required, section.Operation.Ref)
			}
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
		if err := app.CheckResources(); err != nil {
			return err
		}
		required = app.Dependencies(ref.App)
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
