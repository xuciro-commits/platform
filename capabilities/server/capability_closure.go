package platformserver

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
	candidate, err := platform.Candidate([]platform.AssetRef{ref}, available)
	if err != nil {
		return platform.ReleaseCandidate{}, "", err
	}
	release := t.activeRelease
	if retained != nil {
		release = *retained
	}
	if release == "" {
		return candidate, "", nil
	}
	saved, err := platform.ReadCandidate(release, t.releaseCandidates[release])
	if err != nil {
		return platform.ReleaseCandidate{}, "", err
	}
	if !slices.ContainsFunc(saved.Assets, func(a platform.ReleaseAsset) bool { return a.Ref == ref }) {
		if retained != nil {
			return platform.ReleaseCandidate{}, "", fmt.Errorf("capability %s is missing from its retained release", ref)
		}
		return candidate, "", nil
	}
	active, err := platform.Candidate([]platform.AssetRef{ref}, saved.Assets)
	if err != nil || active.ID != candidate.ID {
		// A published version retained by its owner may be used by an old page
		// after a later compute-only activation. Resolve only an exact saved
		// closure, never a caller-selected release or an arbitrary descriptor.
		if retained == nil && sourceVersion != "" {
			ids := make([]string, 0, len(t.releaseCandidates))
			for id := range t.releaseCandidates {
				ids = append(ids, id)
			}
			slices.Sort(ids)
			for _, id := range ids {
				prior, err := platform.ReadCandidate(id, t.releaseCandidates[id])
				if err != nil {
					continue
				}
				closed, err := platform.Candidate([]platform.AssetRef{ref}, prior.Assets)
				if err == nil && closed.ID == candidate.ID {
					return candidate, id, nil
				}
			}
		}
		return platform.ReleaseCandidate{}, "", fmt.Errorf("capability %s differs from its activated release", ref)
	}
	return candidate, release, nil
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
