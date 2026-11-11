package host

import releaseowner "platformserver/services/devops/releases"

import (
	"fmt"
	"platformserver/platform"
	automation "platformserver/services/automate/runtime"
	"slices"
)

func (h hostView) flowBindings() automation.FlowBindingContext {
	return automation.FlowBindingContext{App: h.t.App, Asset: h.t.flowReleaseAsset, Assets: func() ([]platform.ReleaseAsset, error) { return h.t.releaseAssetsLocked(nil, false) }, Active: h.t.releases.Active, Candidate: h.t.releases.Candidate, Applications: func(ref platform.AssetRef, release, dependencies string) ([]platform.AssetBinding, error) {
		return h.t.bindApplications(ref, release, dependencies)
	}, CloseCandidate: h.t.candidateWithBindings, SameRetained: sameRetainedCapability, CodeMatches: releaseowner.ImmutableCodeAssetMatches}
}
func (h hostView) BindFlow(app, name string, version int, prior platform.FlowBinding) (platform.FlowBinding, error) {
	return h.flowBindings().BindFlow(app, name, version, prior)
}
func (t *Tenant) nativeFlowAsset(owner platform.App, name string, version int) (platform.ReleaseAsset, error) {
	provider, ok := t.procs.(platform.NativeFlowAssets)
	if !ok {
		return platform.ReleaseAsset{}, fmt.Errorf("native flow has no original descriptor owner")
	}
	manifest := owner.Manifest()
	refs := []platform.AssetRef{}
	for _, definition := range t.definitions {
		if definition.Source == "code" && definition.Ref.App == manifest.ID && !slices.Contains([]platform.AssetKind{platform.AssetApp, platform.AssetFlow, platform.AssetProtocol, platform.AssetInterface}, definition.Ref.Kind) {
			refs = append(refs, definition.Ref)
		}
	}
	return provider.NativeFlowAsset(manifest, name, version, releaseowner.UniqueRefs(refs))
}
func (t *Tenant) flowReleaseAsset(app, name string, version int) (platform.ReleaseAsset, error) {
	owner := t.App(app)
	if owner == nil {
		return platform.ReleaseAsset{}, fmt.Errorf("flow owner is unavailable")
	}
	if provider, ok := owner.(platform.FlowReleaseOwner); ok {
		return provider.FlowReleaseAsset(name, version)
	}
	return t.nativeFlowAsset(owner, name, version)
}
