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
	Queries      []AssetBinding  `json:"queries,omitempty"`
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
	if err := page.CheckCollectionPorts(); err != nil {
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
		for _, group := range section.RecordLinks {
			requires = append(requires, group.Object)
		}
		requires = append(requires, section.ServiceDependencies()...)
		requires = append(requires, section.ExplorationReferences()...)
		requires = append(requires, section.Actions...)
		if section.InlineEdit != nil {
			requires = append(requires, section.InlineEdit.Action)
		}
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
		if ref.Kind == AssetLinkType {
			var l LinkType
			if json.Unmarshal(body, &l) != nil || asset.ContractVersion != l.Contract() {
				return fmt.Errorf("link asset has an incompatible cardinality contract")
			}
		}
		if ref.Kind == AssetObject {
			info, err := queryObjectDescriptor(body)
			if err != nil {
				return err
			}
			for _, f := range info.Fields {
				if f.Property == nil {
					continue
				}
				source, ok := lookup[f.Property.Ref]
				var p PropertyType
				if !ok || source.SourceVersion != f.Property.SourceVersion || json.Unmarshal(source.Body, &p) != nil || p.CheckField(f) != nil {
					return fmt.Errorf("frozen object field has an unavailable or incompatible property")
				}
			}
		}
		if ref.Kind == AssetApp {
			var application Application
			if err := json.Unmarshal(body, &application); err != nil {
				return err
			}
			if err := application.CheckVariables(); err != nil {
				return err
			}
			if err := checkFrozenQueries(application.QueryPage(), lookup); err != nil {
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
				bindings = append(bindings, flow.Queries...)
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
				if err := checkFrozenQueries(page, lookup); err != nil {
					return err
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
				if !ok || seen[binding.Ref] && ref.Kind == AssetFlow || binding.Ref.Kind != AssetFunction && binding.Ref.Kind != AssetCompute && !(ref.Kind == AssetFlow && binding.Ref.Kind == AssetQuery) || binding.SourceVersion == "" || dependency.SourceVersion != binding.SourceVersion {
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
	case AssetPropertyType:
		var p PropertyType
		if err := json.Unmarshal(body, &p); err != nil {
			return err
		}
		if err := p.Check(); err != nil {
			return err
		}
	case AssetObject:
		info, err := queryObjectDescriptor(body)
		if err != nil {
			return err
		}
		for _, f := range info.Fields {
			if f.Property != nil {
				if f.Property.Ref.Kind != AssetPropertyType || f.Property.SourceVersion == "" {
					return fmt.Errorf("invalid property binding")
				}
				required = append(required, f.Property.Ref)
			}
		}
	case AssetLinkType:
		var l LinkType
		if err := json.Unmarshal(body, &l); err != nil {
			return err
		}
		if err := l.Check(); err != nil {
			return err
		}
		if l.Child.App != ref.App {
			return fmt.Errorf("link type must be declared by its child owner")
		}
		required = append(required, l.Parent, l.Child)
	case AssetQuery:
		var q NamedQuery
		if err := json.Unmarshal(body, &q); err != nil {
			return err
		}
		if err := q.Check(); err != nil {
			return err
		}
		owner, _, ok := strings.Cut(q.Object, ".")
		if !ok {
			return fmt.Errorf("query object has no owner")
		}
		required = append(required, AssetRef{App: owner, Kind: AssetObject, Name: q.Object})
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
		for _, binding := range flow.Queries {
			if binding.Ref.Kind != AssetQuery || binding.SourceVersion == "" {
				return fmt.Errorf("release flow %s has an invalid query binding", ref)
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
		if err := page.CheckCollectionPorts(); err != nil {
			return err
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
			for _, group := range section.RecordLinks {
				required = append(required, group.Object)
			}
			required = append(required, section.ServiceDependencies()...)
			required = append(required, section.ExplorationReferences()...)
			required = append(required, section.Actions...)
			if section.InlineEdit != nil {
				required = append(required, section.InlineEdit.Action)
			}
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

func checkFrozenQueries(page Page, lookup map[AssetRef]ReleaseAsset) error {
	for _, s := range page.Sections {
		if err := page.CheckExplorationBinding(s); err != nil {
			return err
		}
		if err := checkFrozenExploration(page, s, lookup); err != nil {
			return err
		}
		if err := page.CheckContextViewBinding(s); err != nil {
			return err
		}
		if contextView(s.Widget) {
			refs := []AssetRef{}
			if s.Widget == "avatar-stack" || s.RecordVariable != "" {
				ref := s.Object
				if ref.Name == "" {
					ref = page.Object
				}
				refs = append(refs, ref)
			}
			if s.Avatar != nil && s.Avatar.ContextVariable != "" {
				refs = append(refs, page.RecordResourceObject(s.Avatar.ContextVariable))
			}
			for _, ref := range refs {
				asset, ok := lookup[ref]
				info, err := queryObjectDescriptor(asset.Body)
				shown := s.Object
				if shown.Name == "" {
					shown = page.Object
				}
				if !ok || err != nil || !frozenObjectOwnerMatches(asset, ref) || ref == shown && s.CheckContextViews(info) != nil {
					return fmt.Errorf("frozen context presentation object or fields are unavailable")
				}
			}
		}
		if err := page.CheckHistoryBinding(s); err != nil {
			return err
		}
		if s.HistoryLimit > 0 {
			ref := s.Object
			if ref.Name == "" {
				ref = page.Object
			}
			asset, ok := lookup[ref]
			var identity struct {
				Type   string      `json:"type"`
				App    string      `json:"app"`
				Entity *EntityInfo `json:"entity"`
			}
			err := json.Unmarshal(asset.Body, &identity)
			owner, typ := identity.App, identity.Type
			if identity.Entity != nil {
				owner, typ = identity.Entity.App, identity.Entity.Type
			} else if owner == "" {
				owner, _, _ = strings.Cut(typ, ".") // the native builder object descriptor
			}
			if !ok || err != nil || identity.Type != typ || typ != ref.Name || owner != ref.App {
				return fmt.Errorf("frozen history object does not match its actual entity owner")
			}
		}
		if err := page.CheckCollaborationBinding(s); err != nil {
			return err
		}
		if err := s.CheckServices(func(ref AssetRef) (EntityInfo, bool) {
			asset, ok := lookup[ref]
			var descriptor struct {
				Type   string      `json:"type"`
				Entity *EntityInfo `json:"entity"`
			}
			var info EntityInfo
			err := json.Unmarshal(asset.Body, &descriptor)
			if descriptor.Entity != nil {
				info = *descriptor.Entity // service names may differ from their actual app owner
			} else if err == nil {
				err = json.Unmarshal(asset.Body, &info)
			}
			return info, ok && err == nil && descriptor.Type == info.Type
		}, func(ref AssetRef) (Action, bool) {
			asset, ok := lookup[ref]
			var action Action
			err := json.Unmarshal(asset.Body, &action)
			return action, ok && err == nil
		}); err != nil {
			return err
		}
	}
	for _, s := range page.Sections {
		if s.Widget != "record-links" {
			continue
		}
		ref := s.Object
		if ref.Name == "" {
			ref = page.Object
		}
		asset, ok := lookup[ref]
		parent, err := queryObjectDescriptor(asset.Body)
		if !ok || err != nil || s.CheckRecordLinks(parent, func(typ string) (EntityInfo, bool) {
			for ref, asset := range lookup {
				if ref.Kind == AssetObject && ref.Name == typ {
					info, err := queryObjectDescriptor(asset.Body)
					return info, err == nil
				}
			}
			return EntityInfo{}, false
		}) != nil {
			return fmt.Errorf("frozen record link schema is unavailable")
		}
	}

	for _, s := range page.Sections {
		if s.Widget != "inline-action" {
			continue
		}
		ref := s.Object
		if ref.Name == "" {
			ref = page.Object
		}
		object, ok := lookup[ref]
		info, err := queryObjectDescriptor(object.Body)
		if !ok || err != nil || page.Document == nil || len(s.Actions) != 1 {
			return fmt.Errorf("frozen inline action object is unavailable")
		}
		asset, ok := lookup[s.Actions[0]]
		var action Action
		if !ok || json.Unmarshal(asset.Body, &action) != nil || s.CheckInlineAction(info, action) != nil {
			return fmt.Errorf("frozen inline action declaration is unavailable")
		}
	}
	for _, s := range page.Sections {
		if s.Widget != "record-comparison" && s.Widget != "record-card" && s.Widget != "sparkline-kpi" && s.Widget != "treemap" && s.Widget != "tag-counts" && s.Widget != "heatmap" && s.Widget != "record-scatter" && s.Widget != "histogram" && s.Widget != "term-counts" && s.Widget != "record-timeline" && s.Widget != "kanban" && s.Widget != "status-tracker" && s.Widget != "record-list" && s.Widget != "record-chart" && s.Widget != "record-events" && s.Widget != "record-picker" && s.Widget != "record-leaderboard" && s.Widget != "summary-stats" && s.Widget != "record-gantt" && s.Widget != "record-calendar" && s.MetricPresentation == nil {
			continue
		}
		ref := s.Object
		if ref.Name == "" {
			ref = page.Object
		}
		asset, ok := lookup[ref]
		info, err := queryObjectDescriptor(asset.Body)
		if !ok || err != nil || page.Document == nil || s.CheckTimeline(info) != nil || s.CheckKanban(info) != nil || s.CheckStatusTracker(info) != nil || s.CheckMetricPresentation(info) != nil || s.CheckRecordList(info) != nil || s.CheckHeatmap(info) != nil || s.CheckScatter(info) != nil || s.CheckRecordChart(info) != nil || s.CheckRecordEvents(info) != nil || s.CheckRecordPicker(info) != nil || s.CheckLeaderboard(info) != nil || s.CheckHistogram(info) != nil || s.CheckRecordCard(info) != nil || s.CheckRecordComparison(info) != nil || page.CheckRecordComparisonBinding(s) != nil || s.CheckSparkline(info) != nil || s.CheckTerms(info) != nil || s.CheckSummary(info) != nil || s.CheckRecordGantt(info) != nil || s.CheckRecordCalendar(info) != nil {
			return fmt.Errorf("frozen %s schema is unavailable", s.Widget)
		}
	}
	namedSources := map[AssetBinding]NamedQuery{}
	if page.Document != nil {
		if err := page.Document.CheckQuerySets(); err != nil {
			return err
		}
		for _, v := range page.Document.Variables {
			if v.Mode == "property" && v.Source != nil && v.Source.Object != nil {
				asset, ok := lookup[*v.Source.Object]
				object, err := queryObjectDescriptor(asset.Body)
				if !ok || err != nil || page.CheckPropertySchema(v, object) != nil {
					return fmt.Errorf("frozen property schema is unavailable")
				}
			}
			if v.Scope == "application" && v.Mode == "resource" && v.Type == "filter" && v.Source != nil && v.Source.Object != nil {
				asset, ok := lookup[*v.Source.Object]
				object, err := queryObjectDescriptor(asset.Body)
				if !ok || err != nil || CheckFilterSchema(*v.Source, object) != nil {
					return fmt.Errorf("frozen application filter object or fields are unavailable")
				}
			}
		}
		for id, plan := range page.Document.Queries {
			objectAsset, ok := lookup[plan.Object]
			object, err := queryObjectDescriptor(objectAsset.Body)
			if !ok || err != nil {
				return fmt.Errorf("page query %s object is unavailable", id)
			}
			var named *Definition
			if plan.Query != nil {
				asset, ok := lookup[plan.Query.Ref]
				if plan.Query.Ref.Kind == AssetLinkType {
					var l LinkType
					if json.Unmarshal(asset.Body, &l) != nil {
						return fmt.Errorf("invalid frozen link type")
					}
					parentAsset, pok := lookup[l.Parent]
					childAsset, cok := lookup[l.Child]
					parent, pe := queryObjectDescriptor(parentAsset.Body)
					child, ce := queryObjectDescriptor(childAsset.Body)
					parent.App = l.Parent.App
					child.App = l.Child.App
					if !pok || !cok || pe != nil || ce != nil || l.CheckSchema(parent, child) != nil {
						return fmt.Errorf("frozen link source schema is unavailable")
					}
					named = &Definition{Ref: asset.Ref, Version: asset.SourceVersion, LinkType: &l}
				} else {
					var query NamedQuery
					if !ok || json.Unmarshal(asset.Body, &query) != nil {
						return fmt.Errorf("page query %s named query is unavailable", id)
					}
					named = &Definition{Ref: asset.Ref, Version: asset.SourceVersion, Query: &query}
				}
			}
			for _, v := range page.Document.Variables {
				if v.Mode == "aggregate" && v.Source != nil && v.Source.Query == id {
					if err := page.Document.CheckAggregateScalar(v, object); err != nil {
						return err
					}
				}
			}
			if err := page.CheckRecordPickerQuery(id, named); err != nil {
				return err
			}
			if err := page.CheckResourceListQuery(id, named); err != nil {
				return err
			}
			if err := page.CheckAvatarQuery(id, named, object); err != nil {
				return err
			}
			if err := page.CheckLeaderboardQuery(id, named); err != nil {
				return err
			}
			if err := page.CheckQuerySchema(plan, object, named); err != nil {
				return fmt.Errorf("page query %s: %w", id, err)
			}
			if plan.Query != nil && named != nil && named.Query != nil {
				namedSources[*plan.Query] = *named.Query
			}
		}
	}
	return page.CheckQuerySetConditions(namedSources)
}

// Frozen objects retain their owner's complete definition. Construction fields
// store choices as source text, while native descriptors use an array. Project
// these representations before the shared query checker reads the schema.
func queryObjectDescriptor(body []byte) (EntityInfo, error) {
	var shape struct {
		Type      string                       `json:"type"`
		Fields    []map[string]json.RawMessage `json:"fields"`
		Lifecycle *LifecycleInfo               `json:"lifecycle"`
		States    []State                      `json:"states"`
		Actions   []struct {
			Name  string   `json:"name"`
			Title string   `json:"title"`
			From  []string `json:"from"`
			To    string   `json:"to"`
		} `json:"actions"`
	}
	if err := json.Unmarshal(body, &shape); err != nil {
		return EntityInfo{}, err
	}
	var fields []FieldInfo
	for _, raw := range shape.Fields {
		if choicesRaw, ok := raw["choices"]; ok {
			var source string
			if json.Unmarshal(choicesRaw, &source) == nil {
				options := []string{}
				for _, part := range strings.Split(source, ",") {
					if value := strings.TrimSpace(part); value != "" {
						options = append(options, value)
					}
				}
				raw["choices"], _ = json.Marshal(options)
			}
		}
		encoded, err := json.Marshal(raw)
		if err != nil {
			return EntityInfo{}, err
		}
		var field FieldInfo
		if err := json.Unmarshal(encoded, &field); err != nil {
			return EntityInfo{}, err
		}
		fields = append(fields, field)
	}
	owner, _, _ := strings.Cut(shape.Type, ".")
	if shape.Lifecycle == nil && len(shape.States) > 0 {
		l := LifecycleInfo{Field: "state", Initial: shape.States[0].Name, States: shape.States}
		choices := []string{}
		for _, s := range shape.States {
			choices = append(choices, s.Name)
		}
		fields = append(fields, FieldInfo{Name: "state", Title: "State", Type: "choice", ReadOnly: true, Choices: choices})
		for _, a := range shape.Actions {
			to := []string{}
			if a.To != "" {
				to = append(to, a.To)
			}
			l.Transitions = append(l.Transitions, TransitionInfo{Name: a.Name, Schema: shape.Type + "." + a.Name, Title: a.Title, From: a.From, To: to})
		}
		shape.Lifecycle = &l
	}
	return EntityInfo{App: owner, Type: shape.Type, Fields: fields, Lifecycle: shape.Lifecycle}, nil
}

// The narrow finite context profile keeps original complete object identity.
// Code assets may use a type prefix that differs from their actual app owner.
func frozenObjectOwnerMatches(asset ReleaseAsset, ref AssetRef) bool {
	var descriptor struct {
		Type   string      `json:"type"`
		App    string      `json:"app"`
		Entity *EntityInfo `json:"entity"`
	}
	if json.Unmarshal(asset.Body, &descriptor) != nil {
		return false
	}
	owner, typ := descriptor.App, descriptor.Type
	if descriptor.Entity != nil {
		owner, typ = descriptor.Entity.App, descriptor.Entity.Type
	} else if owner == "" {
		owner, _, _ = strings.Cut(typ, ".")
	}
	return ref.Kind == AssetObject && typ == descriptor.Type && typ == ref.Name && owner == ref.App
}
