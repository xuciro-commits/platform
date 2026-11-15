package host

import (
	"fmt"
	"platformserver/platform"
	releaseowner "platformserver/services/devops/releases"
)

func (h hostView) ValidateCodeAsset(asset platform.ReleaseAsset) error {
	return releaseowner.ValidateCodeAsset(asset, h.t.App, h.t.codeReleaseAssetsLocked)
}
func (t *Tenant) assetRegistryLocked() (*platform.AssetRegistry, error) {
	return releaseowner.NewRegistry(t.LiveApps(), t.codeReleaseAssetsLocked, func(app platform.App, ref platform.AssetRef, source string) (platform.ReleaseAsset, error) {
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
	})
}
