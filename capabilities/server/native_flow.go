package platformserver

import (
	"fmt"
	"platformserver/internal/host"
	"platformserver/platform"
)

func (t *Tenant) nativeFlowAsset(owner platform.App, name string, version int) (platform.ReleaseAsset, error) {
	provider, ok := t.procs.(host.NativeFlowAssets)
	if !ok {
		return platform.ReleaseAsset{}, fmt.Errorf("native flow has no original descriptor owner")
	}
	manifest := owner.Manifest()
	refs := []platform.AssetRef{}
	for _, definition := range t.definitions {
		if definition.Source == "code" && definition.Ref.App == manifest.ID && definition.Ref.Kind != platform.AssetApp && definition.Ref.Kind != platform.AssetFlow {
			refs = append(refs, definition.Ref)
		}
	}
	return provider.NativeFlowAsset(manifest, name, version, uniqueRefs(refs))
}

func (t *Tenant) flowReleaseAsset(app, name string, version int) (platform.ReleaseAsset, error) {
	owner := t.app(app)
	if owner == nil {
		return platform.ReleaseAsset{}, fmt.Errorf("flow owner is unavailable")
	}
	if provider, ok := owner.(host.FlowReleaseOwner); ok {
		return provider.FlowReleaseAsset(name, version)
	}
	return t.nativeFlowAsset(owner, name, version)
}
