package releases

import (
	"encoding/json"
	"maps"
	acceptedformat "platformserver/kernel/acceptedformat"
	"platformserver/platform"
	"slices"
	"strings"
)

func ApprovalRoot(assets []platform.ReleaseAsset, app, action, typ string) (platform.AssetRef, bool) {
	has := func(ref platform.AssetRef) bool {
		return slices.ContainsFunc(assets, func(asset platform.ReleaseAsset) bool { return asset.Ref == ref })
	}
	root := platform.AssetRef{App: app, Kind: platform.AssetAction, Name: action}
	if has(root) {
		return root, true
	}
	root = platform.AssetRef{App: app, Kind: platform.AssetObject, Name: typ}
	return root, has(root)
}

func ActivatedReleases(answers map[string]json.RawMessage, active string) []string {
	activated := map[string]bool{}
	for key, raw := range answers {
		if !strings.HasPrefix(key, "release:") {
			continue
		}
		result, err := acceptedformat.DecodeRelease(raw)
		if err == nil && result.Active {
			activated[result.CandidateID] = true
		}
	}
	if active != "" {
		activated[active] = true
	}
	return slices.Sorted(maps.Keys(activated))
}

func ApprovalClosure(sealed platform.ReleaseCandidate, app, action, typ string) (platform.ReleaseCandidate, error) {
	root, found := ApprovalRoot(sealed.Assets, app, action, typ)
	if !found {
		return platform.ReleaseCandidate{}, nil
	}
	return sealed.Closure([]platform.AssetRef{root})
}

func StoredApprovalClosure(store *Store, release, app, action, typ string) (platform.ReleaseCandidate, error) {
	if release == "" {
		return platform.ReleaseCandidate{}, nil
	}
	sealed, err := store.Candidate(release)
	if err != nil {
		return platform.ReleaseCandidate{}, err
	}
	return ApprovalClosure(sealed, app, action, typ)
}

func StoredApprovalExecution(store *Store, release, app, action, typ string) (platform.ReleaseCandidate, error) {
	sealed, err := store.Candidate(release)
	if err != nil {
		return platform.ReleaseCandidate{}, err
	}
	root, _ := ApprovalRoot(sealed.Assets, app, action, typ)
	return sealed.Closure([]platform.AssetRef{root})
}

func ApprovedRelease(activated []string, app, action, typ string, store *Store,
	running func(platform.ReleaseCandidate) (platform.ReleaseCandidate, error)) (string, error) {
	for _, release := range activated {
		if release == "" {
			continue
		}
		sealed, err := store.Candidate(release)
		if err != nil {
			return "", err
		}
		closed, err := ApprovalClosure(sealed, app, action, typ)
		if err != nil {
			return "", err
		}
		if closed.ID == "" {
			continue
		}
		active, err := running(closed)
		if err == nil && SameRetainedCapability(active, closed) {
			return release, nil
		}
	}
	return "", nil
}

func NativeFlowRefs(definitions []platform.Definition, app string) []platform.AssetRef {
	refs := []platform.AssetRef{}
	for _, definition := range definitions {
		if definition.Source == "code" && definition.Ref.App == app && !slices.Contains([]platform.AssetKind{platform.AssetApp, platform.AssetFlow, platform.AssetProtocol, platform.AssetInterface}, definition.Ref.Kind) {
			refs = append(refs, definition.Ref)
		}
	}
	return UniqueRefs(refs)
}
