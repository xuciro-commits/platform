package platformserver

import (
	"fmt"
	"platformserver/platform"
	releaseowner "platformserver/studio/releases"
)

func (h hostView) ValidateCodeAsset(asset platform.ReleaseAsset) error {
	owner := h.t.app(asset.Ref.App)
	if owner == nil {
		return fmt.Errorf("code asset %s has no original owner", asset.Ref)
	}
	assets, err := h.t.codeReleaseAssetsLocked(owner)
	if err != nil {
		return err
	}
	return releaseowner.ValidateCodeAsset(owner, assets, asset)
}
func (t *Tenant) assetRegistryLocked() (*platform.AssetRegistry, error) {
	return t.newAssetRegistryLocked(false)
}
func (t *Tenant) newAssetRegistryLocked(systemUpdate bool) (*platform.AssetRegistry, error) {
	return releaseowner.NewRegistry(t.liveApps(), t.codeReleaseAssetsLocked, t.sharedSystemAssets, func(app platform.App, ref platform.AssetRef, source string) (platform.ReleaseAsset, error) {
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
	}, systemUpdate)
}
