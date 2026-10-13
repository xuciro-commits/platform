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
	if prior.Release != "" {
		closed, err := h.retainedFlowClosure(app, name, version, prior)
		if err != nil {
			return host.FlowBinding{}, err
		}
		bound := prior
		bound.Assets = nil
		for _, asset := range closed.Assets {
			bound.Assets = append(bound.Assets, asset.Ref)
		}
		return bound, nil
	}
	closure, root, err := h.flowClosure(app, name, version)
	if err != nil {
		return host.FlowBinding{}, err
	}
	if closure.ID == "" {
		if prior.Dependencies != "" {
			return host.FlowBinding{}, fmt.Errorf("flow %s.%s has no release descriptor owner", app, name)
		}
		return host.FlowBinding{}, nil
	}
	binding := host.FlowBinding{Dependencies: closure.ID}
	for _, asset := range closure.Assets {
		binding.Assets = append(binding.Assets, asset.Ref)
	}
	if prior.Dependencies != "" {
		if prior.Dependencies != closure.ID {
			return binding, fmt.Errorf("flow %s version %d dependency release changed", root, version)
		}
		return binding, nil
	}
	binding.Release = h.t.releases.active
	if binding.Release == "" {
		return binding, nil
	}
	saved, err := h.t.releases.candidate(binding.Release)
	if err != nil {
		return binding, fmt.Errorf("flow release %s: %w", binding.Release, err)
	}
	if !slices.ContainsFunc(saved.Assets, func(a platform.ReleaseAsset) bool { return a.Ref == root }) {
		binding.Release = ""
		return binding, nil
	}
	closed, err := platform.Candidate([]platform.AssetRef{root}, saved.Assets)
	if err != nil || closed.ID != closure.ID {
		return binding, fmt.Errorf("flow %s version %d differs from its activated release", root, version)
	}
	return binding, nil
}

func (h hostView) retainedFlowClosure(app, name string, version int, prior host.FlowBinding) (platform.ReleaseCandidate, error) {
	owner, ok := h.t.app(app).(host.FlowReleaseOwner)
	if !ok || prior.Dependencies == "" {
		return platform.ReleaseCandidate{}, fmt.Errorf("flow has no retained starting dependency descriptor")
	}
	root, err := owner.FlowReleaseAsset(name, version)
	if err != nil {
		return platform.ReleaseCandidate{}, err
	}
	saved, err := h.t.releases.candidate(prior.Release)
	if err != nil {
		return platform.ReleaseCandidate{}, err
	}
	closed, err := platform.Candidate([]platform.AssetRef{root.Ref}, saved.Assets)
	if err != nil || closed.ID != prior.Dependencies {
		return platform.ReleaseCandidate{}, fmt.Errorf("flow starting dependency descriptor differs")
	}
	for _, asset := range closed.Assets {
		if asset.Ref == root.Ref {
			before, _ := canonicalDigest(asset)
			after, _ := canonicalDigest(root)
			if before != after {
				return platform.ReleaseCandidate{}, fmt.Errorf("retained native flow differs from its starting release")
			}
		}
	}
	return closed, nil
}

func (h hostView) flowClosure(app, name string, version int) (platform.ReleaseCandidate, platform.AssetRef, error) {
	owner, ok := h.t.app(app).(host.FlowReleaseOwner)
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
	closure, err := h.t.candidateWithBindings([]platform.AssetRef{asset.Ref}, available, nil)
	return closure, asset.Ref, err
}
