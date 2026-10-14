package platformserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"

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
		prior, err := canonicalDigest(selected)
		if err != nil {
			return nil, err
		}
		next, err := canonicalDigest(asset)
		if err != nil || next != prior {
			return nil, fmt.Errorf("immutable code asset %s differs from the target runtime", asset.Ref)
		}
	}
	if len(out) > 0 && o.lifecycle == nil {
		return nil, fmt.Errorf("app %s has no controlled asset lifecycle", o.Manifest().ID)
	}
	return out, nil
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
			for _, flow := range app.Manifest().Flows {
				asset, err := t.nativeFlowAsset(app, flow.Name, flow.Version)
				if err != nil {
					return asset, err
				}
				if asset.Ref == ref && asset.SourceVersion == source {
					return asset, nil
				}
			}
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
