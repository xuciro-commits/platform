package host

import (
	"platformserver/platform"
	pageowner "platformserver/services/apps/pages"
)

func (t *Tenant) PageContentDefinition(m platform.Member, ref platform.AssetRef, version string) (platform.Definition, *platform.Error) {
	return pageowner.ContentHost{Admits: t.Admits, Current: func() []platform.Definition { return t.definitions }, Visible: t.Definitions,
		Pinned: func(ref platform.AssetRef, version, content string) (platform.ReleaseAsset, error) {
			registry, err := t.assetRegistryLocked()
			if err != nil {
				return platform.ReleaseAsset{}, err
			}
			return registry.ResolvePinned(ref, version, content)
		},
		Registered:  func() ([]platform.Definition, error) { return t.ownedAssetDefinitions(t.definitions) },
		Definitions: t.definitionsFrom,
	}.Definition(m, ref, version)
}
