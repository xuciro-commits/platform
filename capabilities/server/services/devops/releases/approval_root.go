package releases

import (
	"platformserver/platform"
	"slices"
)

func ApprovalRoot(assets []platform.ReleaseAsset, app, action, typ string) (platform.AssetRef, bool) {
	has := func(ref platform.AssetRef) bool {
		return slices.ContainsFunc(assets, func(asset platform.ReleaseAsset) bool { return asset.Ref == ref })
	}
	root := platform.AssetRef{App: app, Kind: platform.AssetAction, Name: action}
	if has(root) {
		return root, true
	}
	root = platform.AssetRef{App: app, Kind: platform.AssetObject, Name: typ}
	return root, has(root)
}

func NativeFlowRefs(definitions []platform.Definition, app string) []platform.AssetRef {
	refs := []platform.AssetRef{}
	for _, definition := range definitions {
		if definition.Source == "code" && definition.Ref.App == app && !slices.Contains([]platform.AssetKind{platform.AssetApp, platform.AssetFlow, platform.AssetProtocol, platform.AssetInterface}, definition.Ref.Kind) {
			refs = append(refs, definition.Ref)
		}
	}
	return UniqueRefs(refs)
}
