package releases

import "platformserver/platform"

func NativeFlowAsset(provider platform.NativeFlowAssets, definitions []platform.Definition, owner platform.App, name string, version int) (platform.ReleaseAsset, error) {
	manifest := owner.Manifest()
	return provider.NativeFlowAsset(manifest, name, version, NativeFlowRefs(definitions, manifest.ID))
}

func FlowReleaseAsset(provider platform.FlowReleaseOwner, name string, version int, native func() (platform.ReleaseAsset, error)) (platform.ReleaseAsset, error) {
	if provider != nil {
		return provider.FlowReleaseAsset(name, version)
	}
	return native()
}
