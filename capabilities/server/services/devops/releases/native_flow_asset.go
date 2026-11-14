package releases

import (
	"fmt"

	"platformserver/platform"
)

func NativeFlowAsset(processes platform.Processes, definitions []platform.Definition, owner platform.App, name string, version int) (platform.ReleaseAsset, error) {
	provider, ok := processes.(platform.NativeFlowAssets)
	if !ok {
		return platform.ReleaseAsset{}, fmt.Errorf("native flow has no original descriptor owner")
	}
	manifest := owner.Manifest()
	return provider.NativeFlowAsset(manifest, name, version, NativeFlowRefs(definitions, manifest.ID))
}

func FlowReleaseAsset(owner platform.App, name string, version int, native func(owner platform.App, name string, version int) (platform.ReleaseAsset, error)) (platform.ReleaseAsset, error) {
	if owner == nil {
		return platform.ReleaseAsset{}, fmt.Errorf("flow owner is unavailable")
	}
	if provider, ok := owner.(platform.FlowReleaseOwner); ok {
		return provider.FlowReleaseAsset(name, version)
	}
	return native(owner, name, version)
}
