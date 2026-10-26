package platform

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"time"
)

// AssetEnvironmentBinding maps a portable asset to an existing record of its
// target owner. TargetRecord is that record's ID, never an endpoint, credential
// value, source cursor or data copy. Each owner validates its concrete type.
type AssetEnvironmentBinding struct {
	Ref          AssetRef `json:"ref"`
	TargetRecord string   `json:"targetRecord"`
}

// AssetEnvironmentRequirement is declared by the trusted descriptor owner.
// Profile and Credential constrain the target's existing connection; they do
// not carry a URL, secret name or secret value into candidate bytes.
type AssetEnvironmentRequirement struct {
	Ref        AssetRef `json:"ref"`
	Profile    string   `json:"profile,omitempty"`
	Credential bool     `json:"credential,omitempty"`
}

// AssetEnvironmentTarget is the owner's resolved local configuration. Its
// credential name is preparation-only host context, not a portable asset.
type AssetEnvironmentTarget struct {
	TargetRecord string
	Credential   string
}

// AssetReleaseContext is transient preparation context. Execution identities
// are rebound to this target member and frozen in the accepted installation;
// the candidate never acquires this principal or the target binding records.
type AssetReleaseContext struct {
	DraftsOnly bool
	DraftIDs   map[AssetRef]string
	Member     Member
	At         time.Time
	Upgrade    bool
	Bindings   []AssetEnvironmentBinding
}

// AssetApplyContext separates private compatibility trials from pure recovery.
// Validate never means authorization to install live state: callers prepare
// the entire set in a private owner/runtime and promote it only after acceptance.
type AssetApplyContext struct {
	// Private prevents owner-ledger/executor publication while the complete
	// installation is being validated in a private host view. The accepted
	// image is later registered live with Private=false, without new policies.
	Private  bool
	Validate bool
	Upgrade  bool
}

// AssetInstallation is an owner's immutable metadata image. Ref remains the
// original portable asset identity; Record is its target owner record type/ID.
// Schema/Image retain the original publication representation for historical
// release decoding. The owner, not the coordinator, interprets the image.
type AssetInstallation struct {
	Ref    AssetRef        `json:"ref"`
	Schema string          `json:"schema"`
	Record string          `json:"record"`
	Image  json.RawMessage `json:"image"`
}

// AssetDraft bootstraps a missing target-owner draft through its original
// create action, under the target member's permissions. It is private work in
// the enclosing release result, not an independently accepted submission.
type AssetDraft struct {
	Ref    AssetRef        `json:"ref"`
	Type   string          `json:"type"`
	Name   string          `json:"name"`
	Schema string          `json:"schema"`
	Image  json.RawMessage `json:"image"`
}

// AssetLifecycleOwner is trusted compiled owner code, registered at host
// composition. Tenant descriptors cannot register methods or execute hooks.
// The same role is attached to a private installation runtime for trials and
// pure recovery; preparation must not change its live owner or perform I/O.
// Code-only declarations may validate their exact descriptor set and produce
// no mutable installation; generated actions belong to their object's plan.
type AssetLifecycleOwner interface {
	App
	AssetKinds() []AssetKind
	ReleaseAssets() ([]ReleaseAsset, error)
	ResolvePinned(ref AssetRef, sourceVersion string, contentVersion ...string) (ReleaseAsset, error)
	AssetEnvironmentRequirements(ReleaseAsset) ([]AssetEnvironmentRequirement, error)
	ResolveAssetEnvironment(AssetEnvironmentRequirement) (AssetEnvironmentTarget, error)
	// BeginAssetBootstrap exposes the complete immutable shape set only in a
	// private owner while its original create actions construct missing drafts.
	// The returned release restores transient lookups; no definition is installed.
	BeginAssetBootstrap([]ReleaseAsset) (func(), error)
	ReleaseDrafts([]ReleaseAsset, AssetReleaseContext) ([]AssetDraft, error)
	PrepareReleaseAssets([]ReleaseAsset, AssetReleaseContext) ([]AssetInstallation, error)
	// Decode is a pure structural read, including for historical images. It
	// must not inspect current grants or require installed dependencies.
	DecodeAssetInstallation(schema string, image json.RawMessage) (AssetInstallation, error)
	// Validate runs against the private host after preceding dependency plans.
	ValidateAssetInstallation(AssetInstallation) error
	// The registry performs Validate when requested before calling Apply;
	// pure recovery applies the saved image without re-running policy.
	ApplyAssetInstallation(AssetInstallation, AssetApplyContext) error
}

type assetOwnerKey struct {
	app  string
	kind AssetKind
}

// AssetRegistry dispatches only to registered trusted owners. It has no asset
// bytes or execution engine of its own, and does not replace Candidate's
// canonical identities, closure checks or the accepted-result store.
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

func (r *AssetRegistry) validateBindings(assets []ReleaseAsset, context AssetReleaseContext) error {
	provided := map[AssetRef]string{}
	for _, binding := range context.Bindings {
		if _, err := r.Owner(binding.Ref); err != nil {
			return err
		}
		if binding.Ref.Kind != AssetConnection || binding.TargetRecord == "" || provided[binding.Ref] != "" {
			return fmt.Errorf("environment binding %s is missing, duplicate or unsupported", binding.Ref)
		}
		provided[binding.Ref] = binding.TargetRecord
	}
	required, err := r.EnvironmentRequirements(assets)
	if err != nil {
		return err
	}
	for _, requirement := range required {
		if provided[requirement.Ref] == "" && !context.DraftsOnly {
			return fmt.Errorf("release needs a target environment binding for %s", requirement.Ref)
		}
	}
	return nil
}

func (r *AssetRegistry) PrepareReleaseAssets(assets []ReleaseAsset, context AssetReleaseContext) ([]AssetInstallation, error) {
	grouped, err := r.groupedAssets(assets)
	if err != nil {
		return nil, err
	}
	if err := r.validateBindings(assets, context); err != nil {
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
	return out, nil // owners retain their dependency installation order
}

func (r *AssetRegistry) ReleaseDrafts(assets []ReleaseAsset, context AssetReleaseContext) ([]AssetDraft, error) {
	grouped, err := r.groupedAssets(assets)
	if err != nil {
		return nil, err
	}
	if err := r.validateBindings(assets, context); err != nil {
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

// BeginBootstrap scopes each private owner's immutable dependency lookups.
// Failed preparation and successful completion both release in reverse order.
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
