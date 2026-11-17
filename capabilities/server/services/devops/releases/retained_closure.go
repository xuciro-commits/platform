package releases

import (
	"fmt"

	"slices"

	"platformserver/platform"
	"platformserver/services/resources/publication"
)

func (r *Store) RetainedClosure(candidate platform.ReleaseCandidate, ref platform.AssetRef, sourceVersion string, retained *string) (platform.ReleaseCandidate, string, error) {
	release := r.Active()
	if retained != nil {
		release = *retained
	}
	if release == "" {
		return candidate, "", nil
	}
	saved, err := r.Candidate(release)
	if err != nil {
		return platform.ReleaseCandidate{}, "", err
	}
	if !slices.ContainsFunc(saved.Assets, func(a platform.ReleaseAsset) bool { return a.Ref == ref }) {
		if retained != nil {
			return platform.ReleaseCandidate{}, "", fmt.Errorf("capability %s is missing from its retained release", ref)
		}
		return candidate, "", nil
	}
	active, err := r.resolver.Closure(saved, []platform.AssetRef{ref})
	if err != nil || !SameRetainedCapability(r.resolver, candidate, active) {
		if retained == nil && sourceVersion != "" {
			ids := r.Ids()
			for _, id := range ids {
				prior, err := r.Candidate(id)
				if err != nil {
					continue
				}
				closed, err := r.resolver.Closure(prior, []platform.AssetRef{ref})
				if err == nil && SameRetainedCapability(r.resolver, candidate, closed) {
					return closed, id, nil
				}
			}
		}
		return platform.ReleaseCandidate{}, "", fmt.Errorf("capability %s differs from its activated release", ref)
	}
	return active, release, nil
}

func SameRetainedCapability(resolver publication.Resolver, current, retained platform.ReleaseCandidate) bool {
	if current.ID == retained.ID {
		return true
	}
	for _, original := range retained.Assets {
		index := slices.IndexFunc(current.Assets, func(asset platform.ReleaseAsset) bool { return asset.Ref == original.Ref })
		if index < 0 {
			return false
		}
		same, err := ImmutableCodeAssetMatches(resolver, current.Assets[index], original)
		if err != nil || !same {
			return false
		}
	}
	return len(retained.Assets) > 0
}
