package releases

import (
	"encoding/json"
	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	acceptedformat "platformserver/kernel/acceptedformat"
	"platformserver/kernel/protocol"
	"platformserver/platform"
	"platformserver/services/resources/publication"

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

func ActivatedReleases(resolver publication.Resolver, answers map[string]json.RawMessage, active string) []string {
	activated := map[string]bool{}
	for key, raw := range answers {
		if !strings.HasPrefix(key, "release:") {
			continue
		}
		result, err := acceptedformat.DecodeRelease(raw, resolver.ReadCandidate)
		if err == nil && result.Active {
			activated[result.CandidateID] = true
		}
	}
	if active != "" {
		activated[active] = true
	}
	return slices.Sorted(maps.Keys(activated))
}

func ApprovalClosure(resolver publication.Resolver, sealed protocol.ReleaseCandidate, app, action, typ string) (protocol.ReleaseCandidate, error) {
	root, found := ApprovalRoot(sealed.Assets, app, action, typ)
	if !found {
		return protocol.ReleaseCandidate{}, nil
	}
	return resolver.Closure(sealed, []platform.AssetRef{root})
}

func StoredApprovalClosure(store *Store, release, app, action, typ string) (protocol.ReleaseCandidate, error) {
	if release == "" {
		return protocol.ReleaseCandidate{}, nil
	}
	sealed, err := store.Candidate(release)
	if err != nil {
		return protocol.ReleaseCandidate{}, err
	}
	return ApprovalClosure(store.resolver, sealed, app, action, typ)
}

func StoredApprovalExecution(store *Store, release, app, action, typ string) (protocol.ReleaseCandidate, error) {
	sealed, err := store.Candidate(release)
	if err != nil {
		return protocol.ReleaseCandidate{}, err
	}
	root, _ := ApprovalRoot(sealed.Assets, app, action, typ)
	return store.resolver.Closure(sealed, []platform.AssetRef{root})
}

func ApprovalExecution(store *Store, a protocol.ApprovalRequest, retained func([]platform.ReleaseAsset) (protocol.WorkHost, error)) (protocol.WorkHost, error) {
	typ, _, _ := strings.Cut(a.Target, "/")
	closed, err := StoredApprovalExecution(store, a.Release, a.App, a.Action, typ)
	if err != nil {
		return nil, err
	}
	bound, err := retained(closed.Assets)
	if err != nil {
		return nil, err
	}
	app, action, known := bound.Action(a.Action)
	if !known || app != a.App || action.Target != "" && action.Target != typ || action.Approval == nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The retained approval action is unavailable")
	}
	return bound, nil
}

func ApprovedRelease(activated []string, app, action, typ string, store *Store,
	running func(protocol.ReleaseCandidate) (protocol.ReleaseCandidate, error)) (string, error) {
	for _, release := range activated {
		if release == "" {
			continue
		}
		sealed, err := store.Candidate(release)
		if err != nil {
			return "", err
		}
		closed, err := ApprovalClosure(store.resolver, sealed, app, action, typ)
		if err != nil {
			return "", err
		}
		if closed.ID == "" {
			continue
		}
		active, err := running(closed)
		if err == nil && SameRetainedCapability(store.resolver, active, closed) {
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
