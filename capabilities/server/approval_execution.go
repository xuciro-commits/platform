package platformserver

import (
	"maps"
	resultformat "platformserver/internal/acceptance/result"
	"slices"
	"strings"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/host"
	"platformserver/platform"
)

func (h hostView) ApprovalRelease(app, action, typ string) (string, *kernel.Error) {
	activated := map[string]bool{}
	for key, raw := range storedValues(&h.t.committed, "answer", h.t.committed.answers, "active") {
		if !strings.HasPrefix(key, "release:") {
			continue
		}
		result, err := resultformat.DecodeRelease(raw)
		if err == nil && result.Active {
			activated[result.CandidateID] = true
		}
	}
	if release := h.ActiveRelease(); release != "" {
		activated[release] = true
	}
	for _, release := range slices.Sorted(maps.Keys(activated)) {
		closed, refusal := h.t.approvalClosure(release, app, action, typ)
		if refusal != nil {
			return "", refusal
		}
		if closed.ID == "" {
			continue
		}
		running, err := h.t.runningReleaseLocked(closed)
		if err == nil && sameRetainedCapability(running, closed) {
			return release, nil
		}
	}
	return "", nil
}

// A tenant's latest activation may belong to another application. Select an
// approval's original complete action from an actual installed candidate.
func (t *Tenant) approvalClosure(release, app, action, typ string) (platform.ReleaseCandidate, *kernel.Error) {
	if release == "" {
		return platform.ReleaseCandidate{}, nil
	}
	sealed, err := t.releases.candidate(release)
	if err != nil {
		return platform.ReleaseCandidate{}, migrationRefusal(err)
	}
	root := platform.AssetRef{App: app, Kind: platform.AssetAction, Name: action}
	if !slices.ContainsFunc(sealed.Assets, func(asset platform.ReleaseAsset) bool { return asset.Ref == root }) {
		root = platform.AssetRef{App: app, Kind: platform.AssetObject, Name: typ}
		if !slices.ContainsFunc(sealed.Assets, func(asset platform.ReleaseAsset) bool { return asset.Ref == root }) {
			return platform.ReleaseCandidate{}, nil
		}
	}
	closed, err := sealed.Closure([]platform.AssetRef{root})
	return closed, migrationRefusal(err)
}

func (h hostView) ApprovalExecution(c platform.Caller, a platform.ApprovalRequest) (host.WorkHost, *kernel.Error) {
	if a.Release == "" {
		return h, nil
	}
	sealed, err := h.t.releases.candidate(a.Release)
	if err != nil {
		return nil, migrationRefusal(err)
	}
	root := platform.AssetRef{App: a.App, Kind: platform.AssetAction, Name: a.Action}
	typ, _, _ := strings.Cut(a.Target, "/")
	if !slices.ContainsFunc(sealed.Assets, func(asset platform.ReleaseAsset) bool { return asset.Ref == root }) {
		// Object assets already contain their complete action declarations.
		// An application need not separately list each contained action.
		root = platform.AssetRef{App: a.App, Kind: platform.AssetObject, Name: typ}
	}
	bound := host.WorkHost(h)
	app, action, known := bound.Action(a.Action)
	if !known || app != a.App || action.Target != "" && action.Target != typ || action.Approval == nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The retained approval action is unavailable")
	}
	return bound, nil
}
