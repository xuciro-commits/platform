package platformserver

import (
	"fmt"
	"slices"

	"platformserver/internal/host"
	"platformserver/platform"
)

// BindFlow runs under the tenant input/recovery lock. The flow owner supplies
// its retained native version; dependency owners supply their complete assets.
func (h hostView) BindFlow(app, name string, version int, prior host.FlowBinding) (host.FlowBinding, error) {
	closure, root, err := h.flowClosure(app, name, version)
	if err != nil {
		return host.FlowBinding{}, err
	}
	if closure.ID == "" {
		if prior.Dependencies != "" || prior.Release != "" {
			return host.FlowBinding{}, fmt.Errorf("flow %s.%s has no release descriptor owner", app, name)
		}
		return host.FlowBinding{}, nil
	}
	binding := host.FlowBinding{Dependencies: closure.ID, Release: prior.Release}
	for _, dependency := range closure.Assets {
		binding.Assets = append(binding.Assets, dependency.Ref)
	}
	if prior.Dependencies != "" && prior.Dependencies != closure.ID {
		return binding, fmt.Errorf("flow %s version %d dependency release changed", root, version)
	}
	if prior.Dependencies == "" {
		binding.Release = h.t.activeRelease
	}
	if binding.Release == "" {
		return binding, nil
	}
	saved, err := platform.ReadCandidate(binding.Release, h.t.releaseCandidates[binding.Release])
	if err != nil {
		return binding, fmt.Errorf("flow release %s: %w", binding.Release, err)
	}
	contains := slices.ContainsFunc(saved.Assets, func(a platform.ReleaseAsset) bool { return a.Ref == root })
	if !contains && prior.Dependencies == "" {
		binding.Release = "" // An unrelated active release does not bind this run.
		return binding, nil
	}
	closed, err := platform.Candidate([]platform.AssetRef{root}, saved.Assets)
	if err != nil || closed.ID != closure.ID {
		return binding, fmt.Errorf("flow %s version %d differs from its activated release", root, version)
	}
	return binding, nil
}

func (h hostView) flowClosure(app, name string, version int) (platform.ReleaseCandidate, platform.AssetRef, error) {
	owner, ok := h.t.app(app).(interface {
		FlowReleaseAsset(string, int) (platform.ReleaseAsset, error)
	})
	if !ok { // Code flows outside the bounded builder release descriptor scope.
		return platform.ReleaseCandidate{}, platform.AssetRef{}, nil
	}
	asset, err := owner.FlowReleaseAsset(name, version)
	if err != nil {
		return platform.ReleaseCandidate{}, platform.AssetRef{}, err
	}
	available, err := h.t.releaseAssetsLocked(nil, false)
	if err != nil {
		return platform.ReleaseCandidate{}, asset.Ref, err
	}
	i := slices.IndexFunc(available, func(a platform.ReleaseAsset) bool { return a.Ref == asset.Ref })
	if i < 0 {
		return platform.ReleaseCandidate{}, asset.Ref, fmt.Errorf("flow %s has no installed release asset", asset.Ref)
	}
	available[i] = asset
	closure, err := h.t.candidateWithFunctions([]platform.AssetRef{asset.Ref}, available, nil)
	return closure, asset.Ref, err
}
