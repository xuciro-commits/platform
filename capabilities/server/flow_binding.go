package platformserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"

	"platformserver/apps/build"
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
		if raw := h.t.releases.raw(prior.Release); raw != nil {
			if saved, err := platform.ReadCandidate(prior.Release, raw); err == nil {
				if original, err := platform.Candidate([]platform.AssetRef{root}, saved.Assets); err == nil && original.ID == prior.Dependencies && compatibleFlowStorageExpansion(original, closure) {
					bound := prior
					bound.Assets = nil
					for _, asset := range original.Assets {
						bound.Assets = append(bound.Assets, asset.Ref)
					}
					return bound, nil
				}
			}
		}
		return binding, fmt.Errorf("flow %s version %d dependency release changed", root, version)
	}
	if prior.Dependencies == "" {
		binding.Release = h.t.releases.active
	}
	if binding.Release == "" {
		return binding, nil
	}
	saved, err := h.t.releases.candidate(binding.Release)
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

// Keep the original dependency identity. Only additive storage differs; every
// executable asset must remain byte-identical to the instance's saved closure.
func compatibleFlowStorageExpansion(before, after platform.ReleaseCandidate) bool {
	if len(before.Assets) != len(after.Assets) {
		return false
	}
	for _, old := range before.Assets {
		at := slices.IndexFunc(after.Assets, func(next platform.ReleaseAsset) bool { return next.Ref == old.Ref })
		if at < 0 {
			return false
		}
		next := after.Assets[at]
		a, _ := json.Marshal(old)
		b, _ := json.Marshal(next)
		if bytes.Equal(a, b) {
			continue
		}
		if old.Ref.App != build.ID || old.Ref.Kind != platform.AssetObject || old.SourceVersion != next.SourceVersion || old.ContractVersion != next.ContractVersion || !slices.Equal(old.Requires, next.Requires) {
			return false
		}
		var oldObject, nextObject build.Object
		if json.Unmarshal(old.Body, &oldObject) != nil || json.Unmarshal(next.Body, &nextObject) != nil || !build.CompatibleObjectExpansion(oldObject, nextObject) {
			return false
		}
	}
	return true
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
	closure, err := h.t.candidateWithBindings([]platform.AssetRef{asset.Ref}, available, nil)
	return closure, asset.Ref, err
}
