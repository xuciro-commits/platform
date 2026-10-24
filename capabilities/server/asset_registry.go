package platformserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"platformserver/internal/acceptance"
	"slices"
	"strings"

	"platformserver/platform"
)

// Code and controlled definitions retain one owner registration. Code assets
// are immutable runtime prerequisites, not remotely installable tenant hooks.
type runtimeAssetOwner struct {
	platform.App
	lifecycle  platform.AssetLifecycleOwner
	code       []platform.ReleaseAsset
	nativeFlow func(platform.AssetRef, string) (platform.ReleaseAsset, error)
}

// ValidateCodeAsset checks the immutable prerequisite through its original
// code owner. Build uses this when a business definition refers to a native
// platform record; it must never compile that record as a tenant object.
func (h hostView) ValidateCodeAsset(asset platform.ReleaseAsset) error {
	owner := h.t.app(asset.Ref.App)
	if owner == nil {
		return fmt.Errorf("code asset %s has no original owner", asset.Ref)
	}
	assets, err := h.t.codeReleaseAssetsLocked(owner)
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(assets, func(current platform.ReleaseAsset) bool { return current.Ref == asset.Ref }) {
		return fmt.Errorf("asset %s is not an original code declaration", asset.Ref)
	}
	remaining, err := (runtimeAssetOwner{App: owner, code: assets}).controlled([]platform.ReleaseAsset{asset})
	if err != nil {
		return err
	}
	if len(remaining) != 0 {
		return fmt.Errorf("asset %s is not an immutable prerequisite", asset.Ref)
	}
	return nil
}

func (o runtimeAssetOwner) AssetKinds() []platform.AssetKind {
	var kinds []platform.AssetKind
	if o.lifecycle != nil {
		kinds = append(kinds, o.lifecycle.AssetKinds()...)
	}
	for _, asset := range o.code {
		if !slices.Contains(kinds, asset.Ref.Kind) {
			kinds = append(kinds, asset.Ref.Kind)
		}
	}
	return kinds
}
func (o runtimeAssetOwner) ReleaseAssets() ([]platform.ReleaseAsset, error) {
	out := slices.Clone(o.code)
	if o.lifecycle != nil {
		owned, err := o.lifecycle.ReleaseAssets()
		if err != nil {
			return nil, err
		}
		out = append(out, owned...)
	}
	return out, nil
}
func (o runtimeAssetOwner) ResolvePinned(ref platform.AssetRef, version string, contentVersion ...string) (platform.ReleaseAsset, error) {
	for _, asset := range o.code {
		if asset.Ref == ref && asset.SourceVersion == version && len(contentVersion) == 0 {
			return asset, nil
		}
	}
	if ref.Kind == platform.AssetFlow && len(contentVersion) == 0 && o.nativeFlow != nil {
		return o.nativeFlow(ref, version)
	}
	if o.lifecycle != nil {
		return o.lifecycle.ResolvePinned(ref, version, contentVersion...)
	}
	return platform.ReleaseAsset{}, fmt.Errorf("code asset %s has no retained source version %s", ref, version)
}
func (o runtimeAssetOwner) controlled(assets []platform.ReleaseAsset) ([]platform.ReleaseAsset, error) {
	var out []platform.ReleaseAsset
	for _, asset := range assets {
		i := slices.IndexFunc(o.code, func(code platform.ReleaseAsset) bool { return code.Ref == asset.Ref })
		if i < 0 {
			out = append(out, asset)
			continue
		}
		selected := o.code[i]
		if asset.Ref.Kind == platform.AssetFlow && o.nativeFlow != nil {
			retained, err := o.nativeFlow(asset.Ref, asset.SourceVersion)
			if err != nil {
				return nil, err
			}
			selected = retained
		}
		matches, err := immutableCodeAssetMatches(selected, asset)
		if err != nil {
			return nil, err
		}
		if !matches {
			return nil, fmt.Errorf("immutable code asset %s differs from the target runtime", asset.Ref)
		}
	}
	if len(out) > 0 && o.lifecycle == nil {
		return nil, fmt.Errorf("app %s has no controlled asset lifecycle", o.Manifest().ID)
	}
	return out, nil
}

// A retained candidate predates dependency metadata only when Provides is
// absent. Its original code contract still has to match this exact owner and
// source version; the release is never rewritten with today's metadata.
func immutableCodeAssetMatches(current, retained platform.ReleaseAsset) (bool, error) {
	if retained.Ref != current.Ref || retained.SourceVersion != current.SourceVersion || retained.ContractVersion != current.ContractVersion {
		return false, nil
	}
	if retained.Provides != nil {
		before, err := acceptance.Digest(current)
		if err != nil {
			return false, err
		}
		after, err := acceptance.Digest(retained)
		return err == nil && before == after, err
	}
	if err := platform.CheckAssetDependencies(retained); err != nil {
		return false, nil
	}
	if err := platform.CheckAssetDependencies(current); err != nil {
		return false, err
	}
	for _, dependency := range retained.Requires {
		if !slices.Contains(current.Requires, dependency) {
			return false, nil
		}
	}
	var original, selected map[string]json.RawMessage
	if err := json.Unmarshal(retained.Body, &original); err != nil {
		return false, err
	}
	if err := json.Unmarshal(current.Body, &selected); err != nil {
		return false, err
	}
	// Only metadata newly added by the owner may be absent in old bytes.
	// A binding already present in the retained body must match exactly.
	for _, key := range []string{"objectBindings", "protocolBindings", "toolBindings"} {
		if _, exists := original[key]; !exists {
			delete(selected, key)
		}
	}
	if current.Ref.Kind == platform.AssetApp {
		var before, after []platform.AssetRef
		if err := json.Unmarshal(original["resources"], &before); err != nil && len(original["resources"]) != 0 {
			return false, err
		}
		if err := json.Unmarshal(selected["resources"], &after); err != nil && len(selected["resources"]) != 0 {
			return false, err
		}
		// Protocols and interfaces now have their own descriptor. Their new
		// code resources, like the observation authoring records added later,
		// add no execution semantics to the old application.
		after = slices.DeleteFunc(after, func(ref platform.AssetRef) bool {
			observation := ref.Kind == platform.AssetObservation || ref.App == "build" && (ref.Name == "build.observation" || strings.HasPrefix(ref.Name, "build.observation."))
			return (ref.Kind == platform.AssetProtocol || ref.Kind == platform.AssetInterface || observation) && !slices.Contains(before, ref)
		})
		if len(after) == 0 && len(original["resources"]) == 0 {
			delete(selected, "resources")
		} else if len(selected["resources"]) != 0 {
			var err error
			selected["resources"], err = json.Marshal(after)
			if err != nil {
				return false, err
			}
		}
	}
	before, err := acceptance.Digest(original)
	if err != nil {
		return false, err
	}
	after, err := acceptance.Digest(selected)
	return err == nil && before == after, err
}

func (o runtimeAssetOwner) AssetEnvironmentRequirements(asset platform.ReleaseAsset) ([]platform.AssetEnvironmentRequirement, error) {
	controlled, err := o.controlled([]platform.ReleaseAsset{asset})
	if err != nil {
		return nil, err
	}
	if len(controlled) == 0 {
		return nil, nil
	}
	return o.lifecycle.AssetEnvironmentRequirements(asset)
}
func (o runtimeAssetOwner) ResolveAssetEnvironment(requirement platform.AssetEnvironmentRequirement) (platform.AssetEnvironmentTarget, error) {
	if o.lifecycle == nil {
		return platform.AssetEnvironmentTarget{}, fmt.Errorf("code owner %s has no environment bindings", o.Manifest().ID)
	}
	return o.lifecycle.ResolveAssetEnvironment(requirement)
}
func (o runtimeAssetOwner) ReleaseDrafts(assets []platform.ReleaseAsset, context platform.AssetReleaseContext) ([]platform.AssetDraft, error) {
	controlled, err := o.controlled(assets)
	if err != nil {
		return nil, err
	}
	if len(controlled) == 0 {
		return nil, nil
	}
	return o.lifecycle.ReleaseDrafts(controlled, context)
}
func (o runtimeAssetOwner) BeginAssetBootstrap(assets []platform.ReleaseAsset) (func(), error) {
	controlled, err := o.controlled(assets)
	if err != nil {
		return nil, err
	}
	if len(controlled) == 0 {
		return func() {}, nil
	}
	return o.lifecycle.BeginAssetBootstrap(controlled)
}
func (o runtimeAssetOwner) PrepareReleaseAssets(assets []platform.ReleaseAsset, context platform.AssetReleaseContext) ([]platform.AssetInstallation, error) {
	controlled, err := o.controlled(assets)
	if err != nil {
		return nil, err
	}
	if len(controlled) == 0 {
		return nil, nil
	}
	return o.lifecycle.PrepareReleaseAssets(controlled, context)
}
func (o runtimeAssetOwner) DecodeAssetInstallation(schema string, image json.RawMessage) (platform.AssetInstallation, error) {
	if o.lifecycle == nil {
		return platform.AssetInstallation{}, fmt.Errorf("code owner %s cannot install %s", o.Manifest().ID, schema)
	}
	return o.lifecycle.DecodeAssetInstallation(schema, bytes.Clone(image))
}
func (o runtimeAssetOwner) ValidateAssetInstallation(plan platform.AssetInstallation) error {
	if o.lifecycle == nil {
		return fmt.Errorf("code owner %s has no installation image", o.Manifest().ID)
	}
	return o.lifecycle.ValidateAssetInstallation(plan)
}
func (o runtimeAssetOwner) ApplyAssetInstallation(plan platform.AssetInstallation, context platform.AssetApplyContext) error {
	if o.lifecycle == nil {
		return fmt.Errorf("code owner %s has no installation image", o.Manifest().ID)
	}
	return o.lifecycle.ApplyAssetInstallation(plan, context)
}

func (t *Tenant) assetRegistryLocked() (*platform.AssetRegistry, error) {
	registry, err := platform.NewAssetRegistry()
	if err != nil {
		return nil, err
	}
	for _, app := range t.apps {
		code, err := t.codeReleaseAssetsLocked(app)
		if err != nil {
			return nil, err
		}
		owner, _ := app.(platform.AssetLifecycleOwner)
		wrapper := runtimeAssetOwner{App: app, lifecycle: owner, code: code, nativeFlow: func(ref platform.AssetRef, source string) (platform.ReleaseAsset, error) {
			return platform.ReleaseAsset{}, fmt.Errorf("native flow is not retained by its code owner")
		}}
		if len(wrapper.AssetKinds()) == 0 {
			continue
		}
		if err := registry.Register(wrapper); err != nil {
			return nil, err
		}
	}
	return registry, nil
}
