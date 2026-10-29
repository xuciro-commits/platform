package platform

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"math"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

type AssetEnvironmentRequirement struct {
	Ref        AssetRef `json:"ref"`
	Profile    string   `json:"profile,omitempty"`
	Credential bool     `json:"credential,omitempty"`
}

type AssetReleaseContext struct {
	DraftsOnly bool
	DraftIDs   map[AssetRef]string
	Member     Member
	At         time.Time
	Upgrade    bool
}

type AssetApplyContext struct {
	Private  bool
	Validate bool
	Upgrade  bool
}

type AssetInstallation struct {
	Ref    AssetRef        `json:"ref"`
	Schema string          `json:"schema"`
	Record string          `json:"record"`
	Image  json.RawMessage `json:"image"`
}

type AssetDraft struct {
	Ref    AssetRef        `json:"ref"`
	Type   string          `json:"type"`
	Name   string          `json:"name"`
	Schema string          `json:"schema"`
	Image  json.RawMessage `json:"image"`
}

type AssetLifecycleOwner interface {
	App
	AssetKinds() []AssetKind
	ReleaseAssets() ([]ReleaseAsset, error)
	ResolvePinned(ref AssetRef, sourceVersion string, contentVersion ...string) (ReleaseAsset, error)
	AssetEnvironmentRequirements(ReleaseAsset) ([]AssetEnvironmentRequirement, error)
	BeginAssetBootstrap([]ReleaseAsset) (func(), error)
	ReleaseDrafts([]ReleaseAsset, AssetReleaseContext) ([]AssetDraft, error)
	PrepareReleaseAssets([]ReleaseAsset, AssetReleaseContext) ([]AssetInstallation, error)
	DecodeAssetInstallation(schema string, image json.RawMessage) (AssetInstallation, error)
	ValidateAssetInstallation(AssetInstallation) error
	ApplyAssetInstallation(AssetInstallation, AssetApplyContext) error
}

type assetOwnerKey struct {
	app  string
	kind AssetKind
}

type AssetRegistry struct {
	owners map[string]AssetLifecycleOwner
	kinds  map[assetOwnerKey]AssetLifecycleOwner
}

func NewAssetRegistry(owners ...AssetLifecycleOwner) (*AssetRegistry, error) {
	registry := &AssetRegistry{owners: map[string]AssetLifecycleOwner{}, kinds: map[assetOwnerKey]AssetLifecycleOwner{}}
	for _, owner := range owners {
		if err := registry.Register(owner); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func (r *AssetRegistry) Register(owner AssetLifecycleOwner) error {
	if owner == nil || reflect.ValueOf(owner).Kind() == reflect.Pointer && reflect.ValueOf(owner).IsNil() {
		return fmt.Errorf("asset lifecycle owner is nil")
	}
	app := owner.Manifest().ID
	ledger := owner.AcceptedLedger()
	if app == "" || strings.Contains(app, "/") || ledger == nil || ledger.authority != app {
		return fmt.Errorf("asset lifecycle owner needs its app identity and ledger")
	}
	if r.owners == nil {
		r.owners = map[string]AssetLifecycleOwner{}
		r.kinds = map[assetOwnerKey]AssetLifecycleOwner{}
	}
	if r.owners[app] != nil {
		return fmt.Errorf("asset lifecycle owner %s is registered twice", app)
	}
	kinds := slices.Clone(owner.AssetKinds())
	if len(kinds) == 0 {
		return fmt.Errorf("asset lifecycle owner %s declares no supported kinds", app)
	}
	seen := map[AssetKind]bool{}
	for _, kind := range kinds {
		if err := (AssetRef{App: app, Kind: kind, Name: "registered"}).Check(); err != nil {
			return err
		}
		if seen[kind] || r.kinds[assetOwnerKey{app, kind}] != nil {
			return fmt.Errorf("asset lifecycle %s/%s is registered twice", app, kind)
		}
		seen[kind] = true
	}
	r.owners[app] = owner
	for _, kind := range kinds {
		r.kinds[assetOwnerKey{app, kind}] = owner
	}
	return nil
}

func (r *AssetRegistry) Owner(ref AssetRef) (AssetLifecycleOwner, error) {
	if err := ref.Check(); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, fmt.Errorf("asset %s has no lifecycle registry", ref)
	}
	owner := r.kinds[assetOwnerKey{ref.App, ref.Kind}]
	if owner == nil {
		return nil, fmt.Errorf("asset %s has no registered lifecycle owner", ref)
	}
	return owner, nil
}
func (r *AssetRegistry) Owners() []AssetLifecycleOwner {
	if r == nil {
		return nil
	}
	var owners []AssetLifecycleOwner
	for _, app := range slices.Sorted(maps.Keys(r.owners)) {
		owners = append(owners, r.owners[app])
	}
	return owners
}

func (r *AssetRegistry) freezeAsset(asset ReleaseAsset) (ReleaseAsset, error) {
	if _, err := r.Owner(asset.Ref); err != nil {
		return ReleaseAsset{}, err
	}
	if asset.ContractVersion < 1 || asset.SourceVersion == "" {
		return ReleaseAsset{}, fmt.Errorf("asset %s has no pinned source or contract", asset.Ref)
	}
	body, err := canonicalBody(asset.Body)
	if err != nil {
		return ReleaseAsset{}, fmt.Errorf("asset %s: %w", asset.Ref, err)
	}
	asset.Body = body
	asset.Requires = slices.Clone(asset.Requires)
	for _, ref := range asset.Requires {
		if err := ref.Check(); err != nil {
			return ReleaseAsset{}, err
		}
	}
	slices.SortFunc(asset.Requires, compareRef)
	asset.Requires = slices.Compact(asset.Requires)
	if err := checkAssetDescription(asset); err != nil {
		return ReleaseAsset{}, err
	}
	return asset, nil
}

func (r *AssetRegistry) ReleaseAssets() ([]ReleaseAsset, error) {
	var assets []ReleaseAsset
	seen := map[AssetRef]bool{}
	for _, owner := range r.Owners() {
		owned, err := owner.ReleaseAssets()
		if err != nil {
			return nil, err
		}
		for _, asset := range owned {
			if asset.Ref.App != owner.Manifest().ID || seen[asset.Ref] {
				return nil, fmt.Errorf("owner returned a foreign or duplicate asset %s", asset.Ref)
			}
			asset, err = r.freezeAsset(asset)
			if err != nil {
				return nil, err
			}
			seen[asset.Ref] = true
			assets = append(assets, asset)
		}
	}
	slices.SortFunc(assets, func(a, b ReleaseAsset) int { return compareRef(a.Ref, b.Ref) })
	return assets, nil
}

func (r *AssetRegistry) ResolvePinned(ref AssetRef, version string, contentVersion ...string) (ReleaseAsset, error) {
	owner, err := r.Owner(ref)
	if err != nil {
		return ReleaseAsset{}, err
	}
	if version == "" {
		return ReleaseAsset{}, fmt.Errorf("asset %s requires an exact source version", ref)
	}
	if len(contentVersion) > 1 || len(contentVersion) == 1 && (ref.Kind != AssetPage || contentVersion[0] == "") {
		return ReleaseAsset{}, fmt.Errorf("asset %s requires one nonempty page content pin", ref)
	}
	if len(contentVersion) == 1 {
		if err := CheckPageContentVersion(contentVersion[0]); err != nil {
			return ReleaseAsset{}, err
		}
	}
	asset, err := owner.ResolvePinned(ref, version, contentVersion...)
	if err != nil {
		return ReleaseAsset{}, err
	}
	if asset.Ref != ref || asset.SourceVersion != version {
		return ReleaseAsset{}, fmt.Errorf("asset %s returned a different retained identity or version", ref)
	}
	frozen, err := r.freezeAsset(asset)
	if err != nil {
		return ReleaseAsset{}, err
	}
	if len(contentVersion) == 1 {
		var page Page
		if err := json.Unmarshal(frozen.Body, &page); err != nil {
			return ReleaseAsset{}, err
		}
		actual, err := PageContentVersion(page)
		if err != nil || actual != contentVersion[0] {
			return ReleaseAsset{}, fmt.Errorf("asset %s returned different retained page content", ref)
		}
	}
	return frozen, nil
}

func (r *AssetRegistry) groupedAssets(assets []ReleaseAsset) (map[string][]ReleaseAsset, error) {
	grouped := map[string][]ReleaseAsset{}
	seen := map[AssetRef]bool{}
	for _, asset := range assets {
		if seen[asset.Ref] {
			return nil, fmt.Errorf("release repeats asset %s", asset.Ref)
		}
		frozen, err := r.freezeAsset(asset)
		if err != nil {
			return nil, err
		}
		seen[asset.Ref] = true
		grouped[asset.Ref.App] = append(grouped[asset.Ref.App], frozen)
	}
	for app := range grouped {
		slices.SortFunc(grouped[app], func(a, b ReleaseAsset) int { return compareRef(a.Ref, b.Ref) })
	}
	return grouped, nil
}

func (r *AssetRegistry) EnvironmentRequirements(assets []ReleaseAsset) ([]AssetEnvironmentRequirement, error) {
	grouped, err := r.groupedAssets(assets)
	if err != nil {
		return nil, err
	}
	requirements := map[AssetRef]AssetEnvironmentRequirement{}
	for _, app := range slices.Sorted(maps.Keys(grouped)) {
		for _, asset := range grouped[app] {
			owned, err := r.owners[app].AssetEnvironmentRequirements(asset)
			if err != nil {
				return nil, err
			}
			for _, requirement := range owned {
				if _, err := r.Owner(requirement.Ref); err != nil {
					return nil, err
				}
				if requirement.Ref.Kind != AssetConnection {
					return nil, fmt.Errorf("environment requirement %s is not a connection", requirement.Ref)
				}
				if previous, ok := requirements[requirement.Ref]; ok {
					if previous.Profile != "" && requirement.Profile != "" && previous.Profile != requirement.Profile {
						return nil, fmt.Errorf("asset %s needs conflicting environment profiles", requirement.Ref)
					}
					if requirement.Profile == "" {
						requirement.Profile = previous.Profile
					}
					requirement.Credential = requirement.Credential || previous.Credential
				}
				requirements[requirement.Ref] = requirement
			}
		}
	}
	var out []AssetEnvironmentRequirement
	for _, ref := range slices.SortedFunc(maps.Keys(requirements), compareRef) {
		out = append(out, requirements[ref])
	}
	return out, nil
}

func (r *AssetRegistry) PrepareReleaseAssets(assets []ReleaseAsset, context AssetReleaseContext) ([]AssetInstallation, error) {
	grouped, err := r.groupedAssets(assets)
	if err != nil {
		return nil, err
	}
	if _, err := r.EnvironmentRequirements(assets); err != nil {
		return nil, err
	}
	var out []AssetInstallation
	seen := map[AssetRef]bool{}
	records := map[string]bool{}
	for _, app := range slices.Sorted(maps.Keys(grouped)) {
		plans, err := r.owners[app].PrepareReleaseAssets(grouped[app], context)
		if err != nil {
			return nil, err
		}
		for _, plan := range plans {
			if plan.Ref.App != app || !slices.ContainsFunc(grouped[app], func(a ReleaseAsset) bool { return a.Ref == plan.Ref }) || seen[plan.Ref] || records[plan.Record] {
				return nil, fmt.Errorf("owner returned a foreign or duplicate installation %s", plan.Ref)
			}
			if err := r.checkInstallationImage(plan); err != nil {
				return nil, err
			}
			plan.Image = bytes.Clone(plan.Image)
			seen[plan.Ref], records[plan.Record] = true, true
			out = append(out, plan)
		}
	}
	return out, nil
}

func (r *AssetRegistry) ReleaseDrafts(assets []ReleaseAsset, context AssetReleaseContext) ([]AssetDraft, error) {
	grouped, err := r.groupedAssets(assets)
	if err != nil {
		return nil, err
	}
	if _, err := r.EnvironmentRequirements(assets); err != nil {
		return nil, err
	}
	var out []AssetDraft
	seen := map[string]bool{}
	refs := map[AssetRef]bool{}
	for _, app := range slices.Sorted(maps.Keys(grouped)) {
		drafts, err := r.owners[app].ReleaseDrafts(grouped[app], context)
		if err != nil {
			return nil, err
		}
		for _, draft := range drafts {
			key := draft.Type + "/" + draft.Name
			if draft.Ref.App != app || !slices.ContainsFunc(grouped[app], func(a ReleaseAsset) bool { return a.Ref == draft.Ref }) || refs[draft.Ref] || draft.Type == "" || draft.Name == "" || draft.Schema == "" || !json.Valid(draft.Image) || seen[key] {
				return nil, fmt.Errorf("owner returned an invalid or duplicate draft %s", key)
			}
			action, declared := r.owners[app].Manifest().Actions.Action(draft.Schema)
			if !declared || !action.New || action.Target != draft.Type {
				return nil, fmt.Errorf("owner %s returned a draft outside its create declarations", app)
			}
			draft.Image = bytes.Clone(draft.Image)
			seen[key] = true
			refs[draft.Ref] = true
			out = append(out, draft)
		}
	}
	return out, nil
}

func (r *AssetRegistry) BeginBootstrap(assets []ReleaseAsset) (func(), error) {
	grouped, err := r.groupedAssets(assets)
	if err != nil {
		return nil, err
	}
	var releases []func()
	release := func() {
		for i := len(releases) - 1; i >= 0; i-- {
			releases[i]()
		}
		releases = nil
	}
	for _, app := range slices.Sorted(maps.Keys(grouped)) {
		end, err := r.owners[app].BeginAssetBootstrap(grouped[app])
		if end != nil {
			releases = append(releases, end)
		}
		if err != nil {
			release()
			return nil, err
		}
		if end == nil {
			release()
			return nil, fmt.Errorf("asset owner %s provided no bootstrap release", app)
		}
	}
	return release, nil
}

func (r *AssetRegistry) DecodeAssetInstallation(app, schema string, image json.RawMessage) (AssetInstallation, error) {
	if r == nil || r.owners[app] == nil {
		return AssetInstallation{}, fmt.Errorf("installation has no lifecycle owner %s", app)
	}
	plan, err := r.owners[app].DecodeAssetInstallation(schema, bytes.Clone(image))
	if err != nil {
		return AssetInstallation{}, err
	}
	if plan.Ref.App != app || plan.Schema != schema || !bytes.Equal(plan.Image, image) {
		return AssetInstallation{}, fmt.Errorf("owner changed historical installation identity or bytes")
	}
	if err := r.checkInstallationShape(plan); err != nil {
		return AssetInstallation{}, err
	}
	return plan, nil
}
func (r *AssetRegistry) checkInstallationShape(plan AssetInstallation) error {
	if _, err := r.Owner(plan.Ref); err != nil {
		return err
	}
	typ, id, found := strings.Cut(plan.Record, "/")
	if !found || typ == "" || id == "" || plan.Schema == "" || !json.Valid(plan.Image) {
		return fmt.Errorf("asset %s has an incomplete installation image", plan.Ref)
	}
	return nil
}

func (r *AssetRegistry) checkInstallationImage(plan AssetInstallation) error {
	decoded, err := r.DecodeAssetInstallation(plan.Ref.App, plan.Schema, plan.Image)
	if err != nil {
		return err
	}
	if decoded.Ref != plan.Ref || decoded.Record != plan.Record {
		return fmt.Errorf("asset %s installation identity differs from its saved image", plan.Ref)
	}
	return nil
}

func (r *AssetRegistry) ValidateInstallation(plan AssetInstallation) error {
	if err := r.checkInstallationImage(plan); err != nil {
		return err
	}
	owner, _ := r.Owner(plan.Ref)
	plan.Image = bytes.Clone(plan.Image)
	return owner.ValidateAssetInstallation(plan)
}
func (r *AssetRegistry) ApplyInstallation(plan AssetInstallation, context AssetApplyContext) error {
	if err := r.checkInstallationImage(plan); err != nil {
		return err
	}
	owner, _ := r.Owner(plan.Ref)
	if context.Validate {
		if err := owner.ValidateAssetInstallation(AssetInstallation{Ref: plan.Ref, Schema: plan.Schema, Record: plan.Record, Image: bytes.Clone(plan.Image)}); err != nil {
			return err
		}
	}
	plan.Image = bytes.Clone(plan.Image)
	return owner.ApplyAssetInstallation(plan, context)
}

type BlueprintCatalog interface {
	InstalledBlueprint(id, version string) ([]AssetRef, []ReleaseAsset, error)
}

type LinkPublicationReader interface {
	LinkPublication([]byte) (LinkType, error)
}

type FlowReleaseOwner interface {
	FlowReleaseAsset(string, int) (ReleaseAsset, error)
}

type ReleaseExecutionOwner interface {
	ForkReleaseExecution([]ReleaseAsset) (App, error)
}

type OperationDefinitions interface {
	OperationDefinition(string, int) (Operation, int, bool)
}

type FlowDefinitionPresence interface {
	HasDefinition(string, string, int) bool
}
type OperationCompletion interface {
	OperationEnded(Caller, string, time.Time) *kernel.Error
}

type NativeFlowAssets interface {
	NativeFlowAsset(Manifest, string, int, []AssetRef) (ReleaseAsset, error)
}

type AssetDefinitionReader interface {
	DefinitionMetadataKind(AssetKind) bool
	DefinitionDescriptor(ReleaseAsset) (Definition, error)
}

type EvaluationPolicy struct {
	CompareFields    []string         `json:"compareFields,omitempty"`
	MinQuality       float64          `json:"minQuality"`
	MaxCostUSD       float64          `json:"maxCostUsd"`
	MaxLatencyMillis int64            `json:"maxLatencyMillis"`
	Cases            []EvaluationCase `json:"cases"`
}
type EvaluationCase struct {
	Name     string          `json:"name"`
	Input    json.RawMessage `json:"input"`
	Expected json.RawMessage `json:"expected"`
}

func (p EvaluationPolicy) Check() error {
	if math.IsNaN(p.MinQuality) || p.MinQuality <= 0 || p.MinQuality > 1 || math.IsNaN(p.MaxCostUSD) || p.MaxCostUSD <= 0 || p.MaxCostUSD > 1000 || p.MaxLatencyMillis < 1 || p.MaxLatencyMillis > 300000 || len(p.Cases) < 1 || len(p.Cases) > 5 {
		return fmt.Errorf("function evaluation needs bounded quality, cost, latency and cases")
	}
	if len(p.CompareFields) > 16 {
		return fmt.Errorf("evaluation compares at most 16 output fields")
	}
	for i, name := range p.CompareFields {
		if !functionFieldName.MatchString(name) || slices.Contains(p.CompareFields[:i], name) {
			return fmt.Errorf("evaluation comparison fields need unique output names")
		}
	}
	seen := map[string]bool{}
	for _, test := range p.Cases {
		var input, expected map[string]json.RawMessage
		if !functionName.MatchString(test.Name) || seen[test.Name] || len(test.Input) > 32<<10 || len(test.Expected) > 32<<10 || json.Unmarshal(test.Input, &input) != nil || input == nil || json.Unmarshal(test.Expected, &expected) != nil || expected == nil {
			return fmt.Errorf("evaluation cases need unique names and bounded synthetic input/answer objects")
		}
		seen[test.Name] = true
	}
	return nil
}

func (f AIFunction) CheckEvaluationPolicy(p EvaluationPolicy) error {
	if err := p.Check(); err != nil {
		return err
	}
	for _, name := range p.CompareFields {
		if !slices.ContainsFunc(f.Output, func(field Field) bool { return field.Name == name }) {
			return fmt.Errorf("evaluation comparison names an undeclared output field %s", name)
		}
	}
	return nil
}

func (p EvaluationPolicy) Matches(actual, expected []byte) bool {
	var left, right any
	if json.Unmarshal(actual, &left) != nil || json.Unmarshal(expected, &right) != nil {
		return false
	}
	if len(p.CompareFields) == 0 {
		return reflect.DeepEqual(left, right)
	}
	a, aok := left.(map[string]any)
	b, bok := right.(map[string]any)
	if !aok || !bok {
		return false
	}
	for _, name := range p.CompareFields {
		av, aok := a[name]
		bv, bok := b[name]
		if !aok || !bok || !reflect.DeepEqual(av, bv) {
			return false
		}
	}
	return true
}

type EvaluationSetDescriptor struct {
	Name     string           `json:"name"`
	Title    string           `json:"title"`
	Function AssetBinding     `json:"function"`
	Model    string           `json:"model,omitempty"`
	Policy   EvaluationPolicy `json:"policy"`
}

func (e EvaluationSetDescriptor) Check() error {
	if !functionName.MatchString(e.Name) || e.Title == "" || len(e.Title) > 256 || e.Function.Ref.Check() != nil || e.Function.Ref.Kind != AssetFunction || e.Function.SourceVersion == "" || e.Model != strings.TrimSpace(e.Model) || len(e.Model) > 256 {
		return fmt.Errorf("evaluation set needs a name, title and pinned function")
	}
	return e.Policy.Check()
}
func ReadEvaluationSet(raw []byte) (EvaluationSetDescriptor, error) {
	var e EvaluationSetDescriptor
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&e); err != nil {
		return e, err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return e, fmt.Errorf("evaluation set has trailing data")
	}
	return e, e.Check()
}

func (f AIFunction) EvaluationInput(raw []byte) bool {
	if len(raw) > f.MaxInputBytes || !utf8.Valid(raw) {
		return false
	}
	var input map[string]json.RawMessage
	if json.Unmarshal(raw, &input) != nil || input == nil {
		return false
	}
	if f.Conversation {
		var record map[string]json.RawMessage
		var question string
		var history []map[string]json.RawMessage
		if len(input) != 3 || json.Unmarshal(input["record"], &record) != nil || record == nil || json.Unmarshal(input["question"], &question) != nil || strings.TrimSpace(question) == "" || len(question) > 4096 || json.Unmarshal(input["history"], &history) != nil || history == nil || len(history) > 8 {
			return false
		}
		for _, turn := range history {
			var prior string
			if len(turn) != 2 || json.Unmarshal(turn["question"], &prior) != nil || strings.TrimSpace(prior) == "" || len(prior) > 4096 || f.ValidateOutput(turn["answer"]) != nil {
				return false
			}
		}
		input = record
	}
	if len(input) != len(f.Fields) {
		return false
	}
	for _, name := range f.Fields {
		if value, present := input[name]; !present || !scalarEvaluationInput(value) {
			return false
		}
	}
	return true
}

func scalarEvaluationInput(raw json.RawMessage) bool {
	var value any
	if json.Unmarshal(raw, &value) != nil || value == nil {
		return false
	}
	switch value.(type) {
	case string, bool, float64:
		return true
	default:
		return false
	}
}

type LinkTraversal struct {
	Binding   AssetBinding `json:"binding"`
	Direction string       `json:"direction"`
	ID        string       `json:"id"`
}

type LinkType struct {
	Roles        []string `json:"roles,omitempty"`
	Name         string   `json:"name"`
	Title        string   `json:"title"`
	Description  string   `json:"description"`
	Parent       AssetRef `json:"parent"`
	Child        AssetRef `json:"child"`
	Via          string   `json:"via"`
	Forward      string   `json:"forward"`
	Reverse      string   `json:"reverse"`
	Storage      string   `json:"storage"`
	Cardinality  string   `json:"cardinality"`
	Required     bool     `json:"required"`
	DeletePolicy string   `json:"deletePolicy"`
}

func (l LinkType) Check() error {
	if l.Storage == "edge" {
		if !pageNodeID.MatchString(l.Name) || l.Title == "" || l.Description == "" || len(l.Title) > 1024 || len(l.Description) > 16384 || l.Parent.Check() != nil || l.Child.Check() != nil || l.Parent.Kind != AssetObject || l.Child.Kind != AssetObject || l.Parent.App != l.Child.App || l.Via != "" || !pageNodeID.MatchString(l.Forward) || !pageNodeID.MatchString(l.Reverse) || !slices.Contains([]string{"one-to-many", "one-to-one", "many-to-many"}, l.Cardinality) || !slices.Contains([]string{"owner", "restrict-active"}, l.DeletePolicy) || len(l.Roles) == 0 || len(l.Roles) > 16 {
			return fmt.Errorf("owned edge needs same-owner objects, explicit roles and supported cardinality")
		}
		seen := map[string]bool{}
		for _, role := range l.Roles {
			if role == "" || len(role) > 64 || strings.TrimSpace(role) != role || seen[role] {
				return fmt.Errorf("owned edge roles must be unique nonempty names")
			}
			seen[role] = true
		}
		return nil
	}
	if !pageNodeID.MatchString(l.Name) || l.Title == "" || l.Description == "" || len(l.Title) > 1024 || len(l.Description) > 16384 || !pageNodeID.MatchString(l.Via) || !pageNodeID.MatchString(l.Forward) || !pageNodeID.MatchString(l.Reverse) || l.Parent.Check() != nil || l.Child.Check() != nil || l.Parent.Kind != AssetObject || l.Child.Kind != AssetObject || l.Storage != "reference" || !slices.Contains([]string{"one-to-many", "one-to-one"}, l.Cardinality) || !slices.Contains([]string{"owner", "restrict-active"}, l.DeletePolicy) || l.DeletePolicy == "restrict-active" && l.Parent.App != l.Child.App {
		return fmt.Errorf("link type needs typed objects, two names and a supported reference cardinality profile")
	}
	return nil
}
func (l LinkType) Contract() int {
	if l.Storage == "edge" {
		return 4
	}
	if l.DeletePolicy == "restrict-active" {
		return 3
	}
	if l.Cardinality == "one-to-one" {
		return 2
	}
	return 1
}

func (l LinkType) CheckSchema(parent, child EntityInfo) error {
	if err := l.Check(); err != nil {
		return err
	}
	if l.Storage == "edge" {
		if l.Parent.Name != parent.Type || l.Child.Name != child.Type || l.Parent.App != parent.App || l.Child.App != child.App {
			return fmt.Errorf("owned edge endpoint types differ")
		}
		return nil
	}
	f, ok := child.Field(l.Via)
	if l.Parent.Name != parent.Type || l.Parent.App != parent.App || l.Child.Name != child.Type || l.Child.App != child.App || !ok || f.Type != "reference" || f.Ref != parent.Type || f.Required != l.Required {
		return fmt.Errorf("link type requires its original typed reference and field requirement")
	}
	return nil
}
func (d Definition) LinkVersion(version string) *Definition {
	if d.Version == version && d.LinkType != nil {
		return &d
	}
	l, ok := d.LinkVersions[version]
	if !ok {
		return nil
	}
	d.Version = version
	d.LinkType = &l
	d.ContractVersion = l.Contract()
	d.LinkVersions = nil
	return &d
}

func (d Definition) QuerySourceVersion(version string) *Definition {
	if d.Ref.Kind == AssetLinkType {
		return d.LinkVersion(version)
	}
	return d.QueryVersion(version)
}

func (q NamedQuery) Check() error {
	if q.Name == "" || q.Title == "" || (q.Object == "") == (q.Interface == "") || len(q.Name) > 80 || len(q.Title) > 1024 || len(q.Description) > 4096 || q.Limit < 0 || q.Limit > 200 || len(q.Sort) > 4 || len(q.Domain) > 16384 {
		return fmt.Errorf("query has an invalid identity or budget")
	}
	if q.Interface != "" {
		owner, _, _ := strings.Cut(q.Interface, ".")
		if q.By != "" || q.InterfaceShape == nil || q.InterfaceShape.Name != q.Interface || q.InterfaceShape.Check(owner) != nil || len(q.Implementations) > 256 {
			return fmt.Errorf("interface query needs its frozen shape and implementation list, without an untyped parent input")
		}
		for i, typ := range q.Implementations {
			if typ == "" || slices.Contains(q.Implementations[:i], typ) {
				return fmt.Errorf("interface query implementations must be distinct object types")
			}
		}
	} else if q.InterfaceShape != nil || len(q.Implementations) != 0 {
		return fmt.Errorf("object query cannot carry an interface binding")
	}
	var terms []json.RawMessage
	if len(q.Domain) > 0 && (json.Unmarshal(q.Domain, &terms) != nil || len(terms) > 32) {
		return fmt.Errorf("query domain needs a bounded list of conditions")
	}
	return nil
}

func (q NamedQuery) Dependencies() []AssetRef {
	types := q.Implementations
	if q.Object != "" {
		types = []string{q.Object}
	}
	refs := make([]AssetRef, 0, len(types))
	for _, typ := range types {
		owner, _, _ := strings.Cut(typ, ".")
		refs = append(refs, AssetRef{App: owner, Kind: AssetObject, Name: typ})
	}
	slices.SortFunc(refs, func(a, b AssetRef) int { return strings.Compare(a.String(), b.String()) })
	return refs
}

func (d Definition) QueryVersion(version string) *Definition {
	if version == d.Version && d.Query != nil {
		return &d
	}
	q, ok := d.QueryVersions[version]
	if !ok {
		return nil
	}
	body, err := json.Marshal(q)
	if err != nil {
		return nil
	}
	description, err := DescribeAsset(ReleaseAsset{Ref: d.Ref, SourceVersion: version, Body: body})
	if err != nil {
		return nil
	}
	d.Query, d.Version, d.QueryVersions = &q, version, nil
	d.Requires, d.Provides = description.Requires, description.Provides
	return &d
}

type PropertyType struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Type        string `json:"type"`
}

func (p PropertyType) Check() error {
	if !pageNodeID.MatchString(p.Name) || p.Title == "" || len(p.Title) > 1024 || strings.ContainsAny(p.Title, "\"\\") || p.Description == "" || len(p.Description) > 16384 || !slices.Contains([]string{"text", "longtext", "integer", "decimal", "date", "datetime", "boolean"}, p.Type) {
		return fmt.Errorf("property type needs an identity, scalar type and bounded meaning")
	}
	return nil
}
func (p PropertyType) CheckField(f FieldInfo) error {
	if err := p.Check(); err != nil {
		return err
	}
	if f.Type != p.Type || f.Title != p.Title || f.Ref != "" || len(f.Choices) != 0 {
		return fmt.Errorf("field differs from its bound shared property")
	}
	return nil
}
func (d Definition) PropertyVersion(version string) *Definition {
	if d.Version == version && d.PropertyType != nil {
		return &d
	}
	p, ok := d.PropertyVersions[version]
	if !ok {
		return nil
	}
	d.Version = version
	d.PropertyType = &p
	d.PropertyVersions = nil
	return &d
}

type SampleRecords struct {
	Type    string            `json:"type"`
	Records []json.RawMessage `json:"records"`
}

type SampleSet struct {
	Name    string          `json:"name"`
	Title   string          `json:"title"`
	Samples []SampleRecords `json:"samples"`
}

var sampleSetName = regexp.MustCompile(`^[a-z][a-z0-9]{0,63}$`)

func (s SampleSet) Check() error {
	if !sampleSetName.MatchString(s.Name) || s.Title == "" || len(s.Title) > 256 || len(s.Samples) > 8 {
		return fmt.Errorf("Sample set needs a name, title and at most eight model types")
	}
	seen := map[string]bool{}
	size := 0
	for _, group := range s.Samples {
		if ModelObject(group.Type).Check() != nil || seen[group.Type] || len(group.Records) > 100 {
			return fmt.Errorf("Sample model types must be unique, with at most 100 records each")
		}
		seen[group.Type] = true
		ids := map[string]bool{}
		for _, raw := range group.Records {
			size += len(raw)
			var row struct {
				ID string `json:"id"`
			}
			if size > 256<<10 || len(raw) > 32<<10 || invalidSampleRecord(raw) || json.Unmarshal(raw, &row) != nil || row.ID == "" || len(row.ID) > 128 || ids[row.ID] {
				return fmt.Errorf("Sample records need unique bounded IDs and at most 256 KiB in total")
			}
			ids[row.ID] = true
		}
	}
	return nil
}
func (s SampleSet) Dependencies() []AssetRef {
	refs := []AssetRef{}
	for _, group := range s.Samples {
		refs = append(refs, ModelObject(group.Type))
	}
	slices.SortFunc(refs, compareRef)
	return refs
}

func invalidSampleRecord(raw json.RawMessage) bool {
	_, err := DecodeValue(raw, 32<<10)
	return err != nil
}

type SchemaVersion struct {
	Version string `json:"version"`
	Family  uint32 `json:"family"`
}

func (v SchemaVersion) Check() error {
	raw, ok := strings.CutPrefix(v.Version, "schema.sha256.")
	decoded, err := hex.DecodeString(raw)
	if !ok || err != nil || len(decoded) != sha256.Size || strings.ToLower(raw) != raw || v.Family == 0 || v.Family > 64 {
		return fmt.Errorf("schema version needs its content identity and family")
	}
	return nil
}

func SchemaContentVersion(body json.RawMessage) (string, error) {
	canonical, err := canonicalBody(body)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return "schema.sha256." + hex.EncodeToString(sum[:]), nil
}

type SystemDefinitionHost interface {
	FreezeSystemDefinitions(Caller, []AssetRef, time.Time) (json.RawMessage, *kernel.Error)
	PrepareSystemDefinitions(Caller, json.RawMessage, string, time.Time) (json.RawMessage, *kernel.Error)
	ValidateSystemDefinitions(json.RawMessage) error
	ApplySystemDefinitions(json.RawMessage) error
}

type AttachedSystemDefinitions interface {
	AttachSystemDefinitions(SystemDefinitionHost)
}

type SystemDefinitionCandidate struct {
	ID    string `json:"id"`
	Bytes []byte `json:"bytes"`
}

func (saved *SystemDefinitionCandidate) UnmarshalJSON(raw []byte) error {
	var wire struct {
		ID    string          `json:"id"`
		Bytes json.RawMessage `json:"bytes"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return err
	}
	saved.ID = wire.ID
	if len(wire.Bytes) > 0 && wire.Bytes[0] == '"' {
		return json.Unmarshal(wire.Bytes, &saved.Bytes)
	}
	var legacy releaseBytes
	if err := json.Unmarshal(wire.Bytes, &legacy); err != nil {
		return err
	}
	if legacy.Format != 1 {
		return fmt.Errorf("unsupported retained system candidate")
	}
	roots := make([]AssetRef, len(legacy.Assets))
	for i, asset := range legacy.Assets {
		roots[i] = asset.Ref
	}
	checked, err := candidate(roots, legacy.Assets, true)
	if err != nil {
		return err
	}
	if checked.ID != wire.ID {
		return fmt.Errorf("retained system candidate identity differs")
	}
	saved.Bytes = checked.Bytes
	return nil
}

type SystemSource struct {
	Version int
	Digest  string
	Files   []SystemSourceFile
	Read    func(string) (string, error)
}

type SystemSourceFile struct {
	Path     string `json:"path"`
	Digest   string `json:"digest"`
	Size     int    `json:"size"`
	Mode     uint32 `json:"mode"`
	Editable bool   `json:"editable"`
}

type SystemBuildResult struct {
	OK           bool
	Work         string
	Binary       string
	Web          string
	Toolchain    string
	Diagnostics  string
	Manifest     map[string]string
	Source       string
	SourceDigest string
	Duration     time.Duration
}

type TestPlanDescriptor struct {
	Organization *OrgSeed        `json:"organization,omitempty"`
	Name         string          `json:"name"`
	Title        string          `json:"title"`
	Roots        []AssetRef      `json:"roots"`
	At           time.Time       `json:"at"`
	Samples      []SampleRecords `json:"samples,omitempty"`
	Steps        []TestStep      `json:"steps"`
}

func (p TestPlanDescriptor) Check() error {
	if err := CheckSimulationOrganization(p.Organization); err != nil {
		return err
	}
	if p.Organization != nil {
		for _, membership := range p.Organization.Memberships {
			if membership.Party != "" {
				return fmt.Errorf("Portable organization memberships use the member chosen for the run; leave party empty")
			}
		}
	}
	if !sampleSetName.MatchString(p.Name) || p.Title == "" || len(p.Title) > 256 || p.At.IsZero() || len(p.Roots) < 1 || len(p.Roots) > 16 || len(p.Steps) < 1 || len(p.Steps) > 20 {
		return fmt.Errorf("a test plan needs a name, title, definition roots, fixed time and 1–20 steps")
	}
	seen := map[AssetRef]bool{}
	for _, root := range p.Roots {
		if root.Check() != nil || root.App != "build" || root.Kind != AssetObject && root.Kind != AssetFlow || seen[root] {
			return fmt.Errorf("test roots must be distinct Build objects or workflows")
		}
		seen[root] = true
	}
	if err := (SampleSet{Name: p.Name, Title: p.Title, Samples: p.Samples}).Check(); err != nil {
		return err
	}
	for _, step := range p.Steps {
		if step.As != "" {
			return fmt.Errorf("portable test steps use the member chosen for the run")
		}
		if err := step.Check(); err != nil {
			return err
		}
	}
	raw, err := json.Marshal(p)
	if err != nil || len(raw) > 512<<10 {
		return fmt.Errorf("test plan exceeds its byte budget")
	}
	return nil
}

func CheckSimulationOrganization(seed *OrgSeed) error {
	if seed == nil {
		return nil
	}
	raw, err := json.Marshal(seed)
	if err != nil || len(raw) > 64<<10 || len(seed.Units) > 100 || len(seed.Structures) > 16 || len(seed.Edges) > 200 || len(seed.Memberships) > 100 {
		return fmt.Errorf("Synthetic organization exceeds its bounded fixture size")
	}
	return nil
}

func ReadTestPlan(raw []byte) (TestPlanDescriptor, error) {
	var p TestPlanDescriptor
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if len(raw) > 512<<10 || decoder.Decode(&p) != nil || decoder.Decode(new(any)) != io.EOF {
		return p, fmt.Errorf("invalid test plan descriptor")
	}
	return p, p.Check()
}

func (p TestPlanDescriptor) Dependencies() []AssetRef {
	refs := append([]AssetRef{}, p.Roots...)
	for _, sample := range p.Samples {
		refs = append(refs, ModelObject(sample.Type))
	}
	for _, step := range p.Steps {
		if step.Action != "" && step.Type != "build.process" {
			refs = append(refs, AssetRef{App: "build", Kind: AssetAction, Name: step.Action})
		}
		if step.Flow != "" {
			refs = append(refs, AssetRef{App: "build", Kind: AssetFlow, Name: step.Flow})
		}
	}
	return refs
}

type TestStep struct {
	Read           bool             `json:"read,omitempty"`
	Type           string           `json:"type"`
	ID             string           `json:"id"`
	Action         string           `json:"action"`
	Payload        string           `json:"payload"`
	Expect         string           `json:"expect" title:"Expected outcome"`
	As             string           `json:"as,omitempty" title:"Step member ID (empty: plan member)"`
	AdvanceSeconds int              `json:"advanceSeconds,omitempty" title:"Advance clock (seconds)"`
	Flow           string           `json:"flow,omitempty"`
	Step           string           `json:"step,omitempty"`
	Answer         string           `json:"answer,omitempty"`
	Function       *FunctionFixture `json:"function,omitempty"`
	Compute        *ComputeFixture  `json:"compute,omitempty" type:"json"`
}

func (s TestStep) Check() error {
	var payload map[string]json.RawMessage
	if !strings.HasPrefix(s.Type, "build.") || s.Type == "build.object" || s.Type == "build.page" || s.Type == "build.app" || s.Type == "build.testplan" || s.Type == "build.sample-set" || s.Type == "build.process" && s.Action != "build.process.run" && s.AdvanceSeconds == 0 || s.Type == "build.function" || strings.TrimSpace(s.ID) == "" || s.AdvanceSeconds < 0 || s.AdvanceSeconds > 86400 || s.Expect != "accepted" && s.Expect != "refused" || json.Unmarshal([]byte(s.Payload), &payload) != nil || payload == nil {
		return fmt.Errorf("Every test step needs a candidate action or answer, record ID, JSON object, bounded time and expected outcome")
	}
	if s.Compute != nil && !s.Compute.Check() {
		return fmt.Errorf("Use a bounded compute fixture with a completed or failed outcome")
	}
	if s.Function != nil && !s.Function.Check() {
		return fmt.Errorf("Use a bounded function fixture with a ready or rejected outcome")
	}
	if s.Read {
		if s.Action != "" || s.Answer != "" || s.Flow != "" || s.Step != "" || s.AdvanceSeconds != 0 {
			return fmt.Errorf("A read step needs only an object and record ID")
		}
	} else if s.Answer == "" {
		if (s.Action == "" && s.AdvanceSeconds == 0 || s.Action != "" && !strings.HasPrefix(s.Action, s.Type+".")) || s.Flow != "" || s.Step != "" {
			return fmt.Errorf("Choose one candidate action or workflow answer per test step")
		}
	} else if s.Action != "" || !strings.HasPrefix(s.Flow, "build.") || !sampleSetName.MatchString(s.Step) {
		return fmt.Errorf("Choose one candidate action or workflow answer per test step")
	}
	return nil
}

type ComputeFixture struct {
	App          string `json:"app,omitempty"`
	Name         string `json:"name"`
	Output       string `json:"output,omitempty"`
	Error        string `json:"error,omitempty"`
	ExpectState  string `json:"expectState" enum:"completed,failed"`
	ExpectOutput string `json:"expectOutput,omitempty"`
}

func (f ComputeFixture) Check() bool {
	return f.Name != "" && len(f.Name) <= 64 && len(f.Output) <= 48<<10 && len(f.Error) <= 1024 && len(f.ExpectOutput) <= 48<<10 && (f.ExpectState == "completed" && json.Valid([]byte(f.Output)) || f.ExpectState == "failed" && f.Error != "") && (f.ExpectOutput == "" || json.Valid([]byte(f.ExpectOutput)))
}

type FunctionFixture struct {
	Output       string `json:"output"`
	InputTokens  int    `json:"inputTokens"`
	OutputTokens int    `json:"outputTokens"`
	ExpectState  string `json:"expectState" enum:"ready,rejected"`
	ExpectOutput string `json:"expectOutput,omitempty"`
}

func (f FunctionFixture) Check() bool {
	return len(f.Output) <= 64<<10 && len(f.ExpectOutput) <= 32<<10 && f.InputTokens >= 0 && f.InputTokens <= 1<<20 && f.OutputTokens >= 0 && f.OutputTokens <= 1<<20 && (f.ExpectState == "ready" || f.ExpectState == "rejected") && (f.ExpectOutput == "" || f.ExpectState == "ready" && json.Valid([]byte(f.ExpectOutput)))
}

type ReleasePublication struct {
	Schema string          `json:"schema"`
	Image  json.RawMessage `json:"image"`
}

const AcceptedResultMaxBytes = 1 << 20

func CanonicalDigest(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	var normalized any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&normalized); err != nil {
		return "", err
	}
	canonical, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), nil
}

type DecisionDefinition struct {
	Observation       *ObservationDecisionEvidence `json:"observation,omitempty"`
	Definition        AssetBinding                 `json:"definition"`
	Contract          SchemaVersion                `json:"contract"`
	Closure           string                       `json:"closure,omitempty"`
	DependenciesState string                       `json:"dependenciesState"`
	Release           string                       `json:"release,omitempty"`
	Dependencies      []AssetBinding               `json:"dependencies"`
	Applications      []AssetBinding               `json:"applications"`
}

type ObservationDecisionEvidence struct {
	Key        string                     `json:"key"`
	Definition AssetBinding               `json:"definition"`
	Record     string                     `json:"record"`
	Device     string                     `json:"device"`
	Signal     string                     `json:"signal"`
	Window     ObservationWindow          `json:"window"`
	Intervals  *ObservationIntervalResult `json:"intervals,omitempty"`
}

func (b DecisionDefinition) Check(app, schema string) error {
	if b.Observation != nil {
		if err := b.Observation.Check(); err != nil {
			return err
		}
	}
	if b.Definition.Ref != (AssetRef{App: app, Kind: AssetAction, Name: schema}) || b.Definition.SourceVersion == "" || b.Contract.Check() != nil || len(b.Dependencies) > 1000 || len(b.Applications) > 1000 || slices.ContainsFunc(b.Applications, func(binding AssetBinding) bool { return binding.Ref.Kind != AssetApp }) {
		return fmt.Errorf("decision definition differs from its receipt")
	}
	for _, bindings := range [][]AssetBinding{b.Dependencies, b.Applications} {
		seen := map[AssetRef]bool{}
		for _, binding := range bindings {
			if binding.Ref.Check() != nil || binding.SourceVersion == "" || seen[binding.Ref] {
				return fmt.Errorf("decision dependency has an invalid or duplicate binding")
			}
			seen[binding.Ref] = true
		}
	}
	return nil
}

func (e ObservationDecisionEvidence) Check() error {
	if !strings.HasPrefix(e.Key, "observation:") || len(e.Key) > 200 || e.Definition.Ref.Kind != AssetObservation || e.Definition.Ref.Check() != nil || e.Definition.SourceVersion == "" || e.Record == "" || e.Device == "" || e.Signal == "" {
		return fmt.Errorf("observation decision evidence needs its original source and record")
	}
	if e.Intervals != nil {
		if e.Window != (ObservationWindow{}) {
			return fmt.Errorf("state observation evidence cannot contain a numeric window")
		}
		return e.Intervals.Check()
	}
	return e.Window.Check()
}

type ReleaseOrigin struct {
	From   string    `json:"from"`
	Member string    `json:"member"`
	At     time.Time `json:"at"`
}

type SealedArtifact struct {
	Tenant    string    `json:"tenant"`
	Candidate string    `json:"candidate"`
	Key       string    `json:"key"`
	Digest    string    `json:"digest"`
	Size      int       `json:"size"`
	Assets    int       `json:"assets"`
	SealedAt  time.Time `json:"sealedAt"`
}

const EmptyRecordHistory = "history.v1.empty"

func AppendRecordHistory(prior string, position int, change json.RawMessage) (string, error) {
	if position < 0 || position == 0 && prior != EmptyRecordHistory {
		return "", fmt.Errorf("invalid history chain predecessor")
	}
	if position > 0 {
		raw, err := hex.DecodeString(prior)
		if err != nil || len(raw) != sha256.Size {
			return "", fmt.Errorf("invalid history chain digest")
		}
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(change))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return "", err
	}
	raw, err := json.Marshal([]any{"platform.record-history.v1", position, prior, value})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

type ReleaseInstallation struct {
	ReleasePublication
	Before string    `json:"before"`
	Ref    *AssetRef `json:"ref,omitempty"`
	Record string    `json:"record,omitempty"`
}

func DecodeJSON(raw []byte, target any, name string) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("%s has trailing data", name)
	}
	return nil
}

type PackageContribution struct {
	View    string `json:"view"`
	Surface string `json:"surface,omitempty"`
	Title   string `json:"title,omitempty"`
	Kind    string `json:"kind,omitempty"`
	App     string `json:"app,omitempty"`
}

type PackageDescriptor struct {
	Blueprint      *PackageBlueprint     `json:"blueprint,omitempty"`
	ID             string                `json:"id"`
	Version        string                `json:"version"`
	Title          string                `json:"title,omitempty"`
	Namespace      string                `json:"namespace"`
	Requires       []string              `json:"requires,omitempty"`
	Capabilities   []CapabilityContract  `json:"capabilities,omitempty"`
	Compatibility  string                `json:"compatibility,omitempty"`
	Contributions  []PackageContribution `json:"contributions,omitempty"`
	ArtifactDigest string                `json:"artifactDigest,omitempty"`
	Description    string                `json:"description,omitempty"`
}

type PackageBlueprint struct {
	Roots  []AssetRef     `json:"roots"`
	Assets []ReleaseAsset `json:"assets"`
	Guide  string         `json:"guide,omitempty"`
	Setup  *PackageSetup  `json:"setup,omitempty"`
}

type PackageSetup struct {
	Roles []PackageSetupRole `json:"roles"`
	Steps []PackageSetupStep `json:"steps"`
}

type PackageSetupRole struct {
	App  string `json:"app"`
	Role string `json:"role"`
}

type PackageSetupStep struct {
	Name    string        `json:"name"`
	App     string        `json:"app"`
	Schema  string        `json:"schema"`
	Target  *pb.EntityRef `json:"target"`
	Payload string        `json:"payload"`
}

func (p PackageSetup) Check() error {
	if len(p.Steps) == 0 || len(p.Steps) > 128 || len(p.Roles) > 32 {
		return fmt.Errorf("a setup plan needs 1–128 requests and at most 32 role requirements")
	}
	for _, role := range p.Roles {
		if role.App == "" || role.Role == "" || len(role.App) > 128 || len(role.Role) > 128 {
			return fmt.Errorf("setup role requirements need an app and role")
		}
	}
	seen := map[string]bool{}
	for _, step := range p.Steps {
		if step.Name == "" || len(step.Name) > 128 || seen[step.Name] || step.App == "" ||
			!strings.HasPrefix(step.Schema, step.App+".") || step.Target.GetType() == "" || step.Target.GetId() == "" ||
			len(step.Payload) > 64<<10 || !json.Valid([]byte(step.Payload)) {
			return fmt.Errorf("setup requests need unique names, an owner, schema, target and bounded JSON payload")
		}
		seen[step.Name] = true
	}
	return nil
}

type InstalledPackage struct {
	ID            string                `json:"id"`
	Version       string                `json:"version"`
	Namespace     string                `json:"namespace"`
	Title         string                `json:"title,omitempty"`
	State         string                `json:"state"`
	Contributions []PackageContribution `json:"contributions,omitempty"`
	Digest        string                `json:"digest,omitempty"`
	Artifact      string                `json:"artifact,omitempty"`
	InstalledAt   time.Time             `json:"installedAt,omitempty"`
	ChangedAt     time.Time             `json:"changedAt,omitempty"`
	Retained      []RetainedArtifact    `json:"retained,omitempty"`
}

type RetainedArtifact struct {
	Version string    `json:"version"`
	Digest  string    `json:"digest,omitempty"`
	At      time.Time `json:"at"`
}

type Precheck struct {
	ID       string              `json:"id"`
	Version  string              `json:"version"`
	OK       bool                `json:"ok"`
	Problems []string            `json:"problems,omitempty"`
	Missing  []string            `json:"missing,omitempty"`
	Bindings []CapabilityBinding `json:"bindings,omitempty"`
}

type PackageView struct {
	Origin       string            `json:"origin,omitempty"`
	SourceDigest string            `json:"sourceDigest,omitempty"`
	Removal      *PackageRemoval   `json:"removal,omitempty"`
	Descriptor   PackageDescriptor `json:"descriptor"`
	Installed    *InstalledPackage `json:"installed,omitempty"`
	Precheck     Precheck          `json:"precheck"`
	Source       string            `json:"source"`
}

type Contribution struct {
	Key     string `json:"key"`
	View    string `json:"view"`
	Surface string `json:"surface,omitempty"`
	Title   string `json:"title,omitempty"`
	Kind    string `json:"kind,omitempty"`
	App     string `json:"app,omitempty"`
	Source  string `json:"source"`
	State   string `json:"state"`
}

type PackageIndex struct {
	Packages  []PackageDescriptor
	Directory string
}

func (p PackageDescriptor) Validate() error {
	if p.ID == "" || p.Version == "" || p.Namespace == "" {
		return fmt.Errorf("a package needs an ID, a version and a namespace")
	}
	for _, value := range []string{p.ID, p.Namespace, p.Version} {
		if len(value) > 200 || value == "." || value == ".." || strings.ContainsAny(value, "/\\") || strings.IndexFunc(value, func(r rune) bool { return r <= ' ' || r == 127 }) >= 0 {
			return fmt.Errorf("package ID, namespace and version must be safe path segments")
		}
	}
	for _, c := range p.Contributions {
		if c.View == "" {
			return fmt.Errorf("package %s: a contribution needs a view", p.ID)
		}
	}
	for _, need := range p.Capabilities {
		if err := need.Check(); err != nil {
			return err
		}
	}
	if p.Blueprint != nil {
		return p.Blueprint.Check()
	}
	return nil
}

func (b PackageBlueprint) Check() error {
	if len(b.Roots) == 0 || len(b.Assets) == 0 || len(b.Assets) > 1000 {
		return fmt.Errorf("a blueprint needs roots and at most 1000 owner definitions")
	}
	if b.Setup != nil {
		if err := b.Setup.Check(); err != nil {
			return err
		}
	}
	for _, root := range b.Roots {
		if root.Check() != nil || root.App != "build" {
			return fmt.Errorf("a package blueprint has only Build-owned roots")
		}
	}
	seen := map[AssetRef]bool{}
	for _, asset := range b.Assets {
		if seen[asset.Ref] || asset.Ref.App != "build" || asset.ContractVersion < 1 || asset.SourceVersion == "" {
			return fmt.Errorf("blueprint owner definitions need unique pinned identities")
		}
		seen[asset.Ref] = true
		if _, err := WithDependencies(asset); err != nil {
			return err
		}
	}
	return nil
}

func (i *PackageIndex) Latest(id string) (PackageDescriptor, bool) {
	var out PackageDescriptor
	found := false
	for _, p := range i.Packages {
		if p.ID == id && (!found || strings.Compare(p.Version, out.Version) > 0) {
			out, found = p, true
		}
	}
	return out, found
}

func (i *PackageIndex) Find(id, version string) (PackageDescriptor, bool) {
	for _, p := range i.Packages {
		if p.ID == id && (version == "" || p.Version == version) {
			return p, true
		}
	}
	return PackageDescriptor{}, false
}

type PackageSource struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Digest  string `json:"digest"`
	Origin  string `json:"origin"`
}

type PackageInspectRequest struct {
	URL     string          `json:"url,omitempty"`
	Content json.RawMessage `json:"content,omitempty"`
}

type PackagePreview struct {
	Descriptor PackageDescriptor `json:"descriptor"`
	Digest     string            `json:"digest"`
	Origin     string            `json:"origin"`
	Precheck   Precheck          `json:"precheck"`
}

type Offer struct {
	Descriptor PackageDescriptor `json:"descriptor"`
	Origin     string            `json:"origin"`
}

type PackageRemoval struct {
	ID       string   `json:"id"`
	OK       bool     `json:"ok"`
	Blockers []string `json:"blockers"`
}

type PackageState struct {
	Packages map[string]*InstalledPackage
	Sources  map[string]PackageSource
}
type PackageHost interface {
	Apps() []App
	CodeReleaseAssets() ([]ReleaseAsset, error)
	PutPackageArtifact(string, []byte) error
	GetPackageArtifact(string) ([]byte, error)
	BaseModules() []CapabilityModule
}
type ConsolePackages interface {
	BlueprintCatalog
	Attach(PackageHost)
	BindPackageIndex(*PackageIndex)
	BoundIndex() *PackageIndex
	SnapshotState() PackageState
	Restore(PackageState)
	Prepare(Caller, *pb.Submission) (func(*pb.ChangeRecord), *kernel.Error)
	BlueprintViews() []PackageView
	PackageViews() []PackageView
	Contributions() []Contribution
	Precheck(PackageDescriptor) Precheck
	Offer(string) (Offer, error)
	PreparationBlueprint(string, string) (PackageBlueprint, *InstalledPackage, error)
}

const PackageType = "platform.package"

type CompositeEdit struct {
	Schema  string          `json:"schema"`
	Target  *pb.EntityRef   `json:"target"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type CompositeAnswer struct {
	Key     string `json:"key"`
	Applied int    `json:"applied"`
	Digest  string `json:"digest,omitempty"`
}
