package publication

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"platformserver/platform"
)

type assetOwnerKey struct {
	app  string
	kind platform.AssetKind
}

type AssetRegistry struct {
	resolver Resolver
	owners   map[string]platform.AssetLifecycleOwner
	kinds    map[assetOwnerKey]platform.AssetLifecycleOwner
}

func (resolver Resolver) NewAssetRegistry(owners ...platform.AssetLifecycleOwner) (*AssetRegistry, error) {
	registry := &AssetRegistry{resolver: resolver, owners: map[string]platform.AssetLifecycleOwner{}, kinds: map[assetOwnerKey]platform.AssetLifecycleOwner{}}
	for _, owner := range owners {
		if err := registry.Register(owner); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func (r *AssetRegistry) Register(owner platform.AssetLifecycleOwner) error {
	if owner == nil || reflect.ValueOf(owner).Kind() == reflect.Pointer && reflect.ValueOf(owner).IsNil() {
		return fmt.Errorf("asset lifecycle owner is nil")
	}
	app := owner.Manifest().ID
	ledger := owner.Authority()
	if app == "" || strings.Contains(app, "/") || ledger == nil || ledger.ID() != app {
		return fmt.Errorf("asset lifecycle owner needs its app identity and ledger")
	}
	if r.owners == nil {
		r.owners = map[string]platform.AssetLifecycleOwner{}
		r.kinds = map[assetOwnerKey]platform.AssetLifecycleOwner{}
	}
	if r.owners[app] != nil {
		return fmt.Errorf("asset lifecycle owner %s is registered twice", app)
	}
	kinds := slices.Clone(owner.AssetKinds())
	if len(kinds) == 0 {
		return fmt.Errorf("asset lifecycle owner %s declares no supported kinds", app)
	}
	seen := map[platform.AssetKind]bool{}
	for _, kind := range kinds {
		if err := (platform.AssetRef{App: app, Kind: kind, Name: "registered"}).Check(); err != nil {
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

func (r *AssetRegistry) Owner(ref platform.AssetRef) (platform.AssetLifecycleOwner, error) {
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

func (r *AssetRegistry) Owners() []platform.AssetLifecycleOwner {
	if r == nil {
		return nil
	}
	var owners []platform.AssetLifecycleOwner
	for _, app := range slices.Sorted(maps.Keys(r.owners)) {
		owners = append(owners, r.owners[app])
	}
	return owners
}

func (r *AssetRegistry) freezeAsset(asset platform.ReleaseAsset) (platform.ReleaseAsset, error) {
	if _, err := r.Owner(asset.Ref); err != nil {
		return platform.ReleaseAsset{}, err
	}
	if asset.ContractVersion < 1 || asset.SourceVersion == "" {
		return platform.ReleaseAsset{}, fmt.Errorf("asset %s has no pinned source or contract", asset.Ref)
	}
	body, err := platform.CanonicalAssetBody(asset.Body)
	if err != nil {
		return platform.ReleaseAsset{}, fmt.Errorf("asset %s: %w", asset.Ref, err)
	}
	asset.Body = body
	asset.Requires = slices.Clone(asset.Requires)
	for _, ref := range asset.Requires {
		if err := ref.Check(); err != nil {
			return platform.ReleaseAsset{}, err
		}
	}
	slices.SortFunc(asset.Requires, platform.CompareAssetRef)
	asset.Requires = slices.Compact(asset.Requires)
	if err := r.resolver.CheckAssetDependencies(asset); err != nil {
		return platform.ReleaseAsset{}, err
	}
	return asset, nil
}

func (r *AssetRegistry) ReleaseAssets() ([]platform.ReleaseAsset, error) {
	var assets []platform.ReleaseAsset
	seen := map[platform.AssetRef]bool{}
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
	slices.SortFunc(assets, func(a, b platform.ReleaseAsset) int { return platform.CompareAssetRef(a.Ref, b.Ref) })
	return assets, nil
}

func (r *AssetRegistry) ResolvePinned(ref platform.AssetRef, version string, contentVersion ...string) (platform.ReleaseAsset, error) {
	owner, err := r.Owner(ref)
	if err != nil {
		return platform.ReleaseAsset{}, err
	}
	if version == "" {
		return platform.ReleaseAsset{}, fmt.Errorf("asset %s requires an exact source version", ref)
	}
	if len(contentVersion) > 1 || len(contentVersion) == 1 && (ref.Kind != platform.AssetPage || contentVersion[0] == "") {
		return platform.ReleaseAsset{}, fmt.Errorf("asset %s requires one nonempty page content pin", ref)
	}
	if len(contentVersion) == 1 {
		if err := platform.CheckPageContentVersion(contentVersion[0]); err != nil {
			return platform.ReleaseAsset{}, err
		}
	}
	asset, err := owner.ResolvePinned(ref, version, contentVersion...)
	if err != nil {
		return platform.ReleaseAsset{}, err
	}
	if asset.Ref != ref || asset.SourceVersion != version {
		return platform.ReleaseAsset{}, fmt.Errorf("asset %s returned a different retained identity or version", ref)
	}
	frozen, err := r.freezeAsset(asset)
	if err != nil {
		return platform.ReleaseAsset{}, err
	}
	if len(contentVersion) == 1 {
		var page platform.Page
		if err := json.Unmarshal(frozen.Body, &page); err != nil {
			return platform.ReleaseAsset{}, err
		}
		actual, err := r.resolver.PageContentVersion(page)
		if err != nil || actual != contentVersion[0] {
			return platform.ReleaseAsset{}, fmt.Errorf("asset %s returned different retained page content", ref)
		}
	}
	return frozen, nil
}

func (r *AssetRegistry) groupedAssets(assets []platform.ReleaseAsset) (map[string][]platform.ReleaseAsset, error) {
	grouped := map[string][]platform.ReleaseAsset{}
	seen := map[platform.AssetRef]bool{}
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
		slices.SortFunc(grouped[app], func(a, b platform.ReleaseAsset) int { return platform.CompareAssetRef(a.Ref, b.Ref) })
	}
	return grouped, nil
}

func (r *AssetRegistry) EnvironmentRequirements(assets []platform.ReleaseAsset) ([]platform.AssetEnvironmentRequirement, error) {
	grouped, err := r.groupedAssets(assets)
	if err != nil {
		return nil, err
	}
	requirements := map[platform.AssetRef]platform.AssetEnvironmentRequirement{}
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
				if requirement.Ref.Kind != platform.AssetConnection {
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
	var out []platform.AssetEnvironmentRequirement
	for _, ref := range slices.SortedFunc(maps.Keys(requirements), platform.CompareAssetRef) {
		out = append(out, requirements[ref])
	}
	return out, nil
}

func (r *AssetRegistry) PrepareReleaseAssets(assets []platform.ReleaseAsset, context platform.AssetReleaseContext) ([]platform.AssetInstallation, error) {
	grouped, err := r.groupedAssets(assets)
	if err != nil {
		return nil, err
	}
	if _, err := r.EnvironmentRequirements(assets); err != nil {
		return nil, err
	}
	var out []platform.AssetInstallation
	seen := map[platform.AssetRef]bool{}
	records := map[string]bool{}
	for _, app := range slices.Sorted(maps.Keys(grouped)) {
		plans, err := r.owners[app].PrepareReleaseAssets(grouped[app], context)
		if err != nil {
			return nil, err
		}
		for _, plan := range plans {
			if plan.Ref.App != app || !slices.ContainsFunc(grouped[app], func(a platform.ReleaseAsset) bool { return a.Ref == plan.Ref }) || seen[plan.Ref] || records[plan.Record] {
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

func (r *AssetRegistry) ReleaseDrafts(assets []platform.ReleaseAsset, context platform.AssetReleaseContext) ([]platform.AssetDraft, error) {
	grouped, err := r.groupedAssets(assets)
	if err != nil {
		return nil, err
	}
	if _, err := r.EnvironmentRequirements(assets); err != nil {
		return nil, err
	}
	var out []platform.AssetDraft
	seen := map[string]bool{}
	refs := map[platform.AssetRef]bool{}
	for _, app := range slices.Sorted(maps.Keys(grouped)) {
		drafts, err := r.owners[app].ReleaseDrafts(grouped[app], context)
		if err != nil {
			return nil, err
		}
		for _, draft := range drafts {
			key := draft.Type + "/" + draft.Name
			if draft.Ref.App != app || !slices.ContainsFunc(grouped[app], func(a platform.ReleaseAsset) bool { return a.Ref == draft.Ref }) || refs[draft.Ref] || draft.Type == "" || draft.Name == "" || draft.Schema == "" || !json.Valid(draft.Image) || seen[key] {
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

func (r *AssetRegistry) BeginBootstrap(assets []platform.ReleaseAsset) (func(), error) {
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

func (r *AssetRegistry) DecodeAssetInstallation(app, schema string, image json.RawMessage) (platform.AssetInstallation, error) {
	if r == nil || r.owners[app] == nil {
		return platform.AssetInstallation{}, fmt.Errorf("installation has no lifecycle owner %s", app)
	}
	plan, err := r.owners[app].DecodeAssetInstallation(schema, bytes.Clone(image))
	if err != nil {
		return platform.AssetInstallation{}, err
	}
	if plan.Ref.App != app || plan.Schema != schema || !bytes.Equal(plan.Image, image) {
		return platform.AssetInstallation{}, fmt.Errorf("owner changed historical installation identity or bytes")
	}
	if err := r.checkInstallationShape(plan); err != nil {
		return platform.AssetInstallation{}, err
	}
	return plan, nil
}

func (r *AssetRegistry) checkInstallationShape(plan platform.AssetInstallation) error {
	if _, err := r.Owner(plan.Ref); err != nil {
		return err
	}
	typ, id, found := strings.Cut(plan.Record, "/")
	if !found || typ == "" || id == "" || plan.Schema == "" || !json.Valid(plan.Image) {
		return fmt.Errorf("asset %s has an incomplete installation image", plan.Ref)
	}
	return nil
}

func (r *AssetRegistry) checkInstallationImage(plan platform.AssetInstallation) error {
	decoded, err := r.DecodeAssetInstallation(plan.Ref.App, plan.Schema, plan.Image)
	if err != nil {
		return err
	}
	if decoded.Ref != plan.Ref || decoded.Record != plan.Record {
		return fmt.Errorf("asset %s installation identity differs from its saved image", plan.Ref)
	}
	return nil
}

func (r *AssetRegistry) ValidateInstallation(plan platform.AssetInstallation) error {
	if err := r.checkInstallationImage(plan); err != nil {
		return err
	}
	owner, _ := r.Owner(plan.Ref)
	plan.Image = bytes.Clone(plan.Image)
	return owner.ValidateAssetInstallation(plan)
}

func (r *AssetRegistry) ApplyInstallation(plan platform.AssetInstallation, context platform.AssetApplyContext) error {
	if err := r.checkInstallationImage(plan); err != nil {
		return err
	}
	owner, _ := r.Owner(plan.Ref)
	if context.Validate {
		if err := owner.ValidateAssetInstallation(platform.AssetInstallation{Ref: plan.Ref, Schema: plan.Schema, Record: plan.Record, Image: bytes.Clone(plan.Image)}); err != nil {
			return err
		}
	}
	plan.Image = bytes.Clone(plan.Image)
	return owner.ApplyAssetInstallation(plan, context)
}
