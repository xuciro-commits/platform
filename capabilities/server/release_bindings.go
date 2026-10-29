package platformserver

import "platformserver/platform"
import releaseowner "platformserver/studio/releases"

// The lane binds the original retained-owner read; candidate planning belongs to Studio.
func (t *Tenant) candidateWithBindings(roots []platform.AssetRef, available []platform.ReleaseAsset, fixed []platform.AssetRef) (platform.ReleaseCandidate, error) {
	registry, err := t.assetRegistryLocked()
	if err != nil {
		return platform.ReleaseCandidate{}, err
	}
	return releaseowner.CandidateWithBindings(roots, available, fixed, registry.ResolvePinned)
}
