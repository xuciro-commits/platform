package platform

import (
	"bytes"
	"crypto/sha256"
	"encoding/base32"
	"encoding/json"
	"fmt"
	"maps"
	"platformkernel/kernel"
	"reflect"
	"slices"
	"strings"
	"time"
)

func ResourceID(tenant string, ref AssetRef) string {
	sum := sha256.Sum256([]byte(tenant + "/" + ref.String()))
	return "ri." + ref.App + "." + tenant + "." + string(ref.Kind) + "." + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:16]))
}

type AssetEnvironmentRequirement struct {
	Ref        AssetRef `json:"ref"`
	Profile    string   `json:"profile,omitempty"`
	Credential bool     `json:"credential,omitempty"`
}

type AssetReleaseContext struct {
	ManagedAssets map[AssetRef]bool
	ManagedBy     string
	DraftsOnly    bool
	DraftIDs      map[AssetRef]string
	Project       string
	Member        Member
	At            time.Time
	Upgrade       bool
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
