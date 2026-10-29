package platformserver

import releaseowner "platformserver/studio/releases"

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"

	"platformserver/platform"
)

// Typed compute and AI invocation close over the same immutable owner assets.
// They do not introduce a second release pointer or publication registry.
func (t *Tenant) capabilityClosure(ref platform.AssetRef, body json.RawMessage, sourceVersion string, retained *string) (platform.ReleaseCandidate, string, error) {
	available, err := t.releaseAssetsLocked(nil, false)
	if err != nil {
		return platform.ReleaseCandidate{}, "", err
	}
	i := slices.IndexFunc(available, func(a platform.ReleaseAsset) bool { return a.Ref == ref })
	if i < 0 {
		return platform.ReleaseCandidate{}, "", fmt.Errorf("capability %s has no release owner", ref)
	}
	available[i].Body = body
	if sourceVersion != "" {
		available[i].SourceVersion = sourceVersion
	}
	available[i], err = t.describeOwnedAsset(available[i])
	if err != nil {
		return platform.ReleaseCandidate{}, "", err
	}
	candidate, err := platform.Candidate([]platform.AssetRef{ref}, available)
	if err != nil {
		return platform.ReleaseCandidate{}, "", err
	}
	release := t.releases.Active()
	if retained != nil {
		release = *retained
	}
	if release == "" {
		return candidate, "", nil
	}
	saved, err := t.releases.Candidate(release)
	if err != nil {
		return platform.ReleaseCandidate{}, "", err
	}
	if !slices.ContainsFunc(saved.Assets, func(a platform.ReleaseAsset) bool { return a.Ref == ref }) {
		if retained != nil {
			return platform.ReleaseCandidate{}, "", fmt.Errorf("capability %s is missing from its retained release", ref)
		}
		return candidate, "", nil
	}
	active, err := saved.Closure([]platform.AssetRef{ref})
	if err != nil || !sameRetainedCapability(candidate, active) {
		// A published version retained by its owner may be used by an old page
		// after a later compute-only activation. Resolve only an exact saved
		// closure, never a caller-selected release or an arbitrary descriptor.
		if retained == nil && sourceVersion != "" {
			ids := t.releases.Ids()
			for _, id := range ids {
				prior, err := t.releases.Candidate(id)
				if err != nil {
					continue
				}
				closed, err := prior.Closure([]platform.AssetRef{ref})
				if err == nil && sameRetainedCapability(candidate, closed) {
					return closed, id, nil
				}
			}
		}
		return platform.ReleaseCandidate{}, "", fmt.Errorf("capability %s differs from its activated release", ref)
	}
	return active, release, nil
}

func sameRetainedCapability(current, retained platform.ReleaseCandidate) bool {
	if current.ID == retained.ID {
		return true
	}
	for _, original := range retained.Assets {
		index := slices.IndexFunc(current.Assets, func(asset platform.ReleaseAsset) bool { return asset.Ref == original.Ref })
		if index < 0 {
			return false
		}
		same, err := releaseowner.ImmutableCodeAssetMatches(current.Assets[index], original)
		if err != nil || !same {
			return false
		}
	}
	return len(retained.Assets) > 0
}
func (t *Tenant) operationClosure(app string, op platform.Operation, version int, retained *string) (platform.ReleaseCandidate, string, error) {
	body, err := json.Marshal(op)
	if err != nil {
		return platform.ReleaseCandidate{}, "", err
	}
	sourceVersion := ""
	if version > 0 {
		sourceVersion = t.app(app).Manifest().Version + ".compute-" + strconv.Itoa(version)
	}
	return t.capabilityClosure(platform.AssetRef{App: app, Kind: platform.AssetCompute, Name: op.Name}, body, sourceVersion, retained)
}
