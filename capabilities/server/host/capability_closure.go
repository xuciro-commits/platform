package host

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"

	"platformserver/platform"
)

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
	available[i], err = t.describer().DescribeOwnedAsset(available[i])
	if err != nil {
		return platform.ReleaseCandidate{}, "", err
	}
	candidate, err := platform.Candidate([]platform.AssetRef{ref}, available)
	if err != nil {
		return platform.ReleaseCandidate{}, "", err
	}
	return t.releases.RetainedClosure(candidate, ref, sourceVersion, retained)
}

func (t *Tenant) operationClosure(app string, op platform.Operation, version int, retained *string) (platform.ReleaseCandidate, string, error) {
	body, err := json.Marshal(op)
	if err != nil {
		return platform.ReleaseCandidate{}, "", err
	}
	sourceVersion := ""
	if version > 0 {
		sourceVersion = t.App(app).Manifest().Version + ".compute-" + strconv.Itoa(version)
	}
	return t.capabilityClosure(platform.AssetRef{App: app, Kind: platform.AssetCompute, Name: op.Name}, body, sourceVersion, retained)
}
