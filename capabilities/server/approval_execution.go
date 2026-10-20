package platformserver

import (
	"maps"
	resultformat "platformserver/internal/acceptance/result"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/host"
	"platformserver/platform"
)

func (h hostView) ApprovalRelease(app, action, typ string) (string, *kernel.Error) {
	activated := map[string]bool{}
	for key, raw := range h.t.committed.allAnswers() {
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

func (h flowExecution) ApprovalRelease(app, action, typ string) (string, *kernel.Error) {
	closed, refusal := h.rt.view.approvalClosure(h.rt.release, app, action, typ)
	if refusal != nil || closed.ID == "" {
		return "", refusal
	}
	return h.rt.release, nil
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
	closed, err := sealed.Closure([]platform.AssetRef{root})
	if err != nil {
		return nil, migrationRefusal(err)
	}
	execution, err := h.retainedExecution(c, a.Release, closed.Assets)
	if err != nil {
		return nil, migrationRefusal(err)
	}
	bound := execution.(host.WorkHost)
	app, action, known := bound.Action(a.Action)
	if !known || app != a.App || action.Target != "" && action.Target != typ || action.Approval == nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The retained approval action is unavailable")
	}
	return bound, nil
}

func (r *flowExecutionRuntime) ProbeDecision(c platform.Caller, decide func(platform.Caller) *kernel.Error) *kernel.Error {
	probe := r.stagedDecision.privateFork()
	bound := *r
	bound.stagedDecision = probe
	return probe.runProbe(c, func(_ platform.Caller) *kernel.Error {
		return decide(platform.NewCaller(&bound, c.Member, c.App, c.Replaying, c.Automation))
	})
}

func (r *flowExecutionRuntime) RequestApproval(c platform.Caller, app string, s *pb.Submission, at time.Time) (*pb.ChangeRecord, *kernel.Error) {
	owner := r.view.app(app)
	if owner == nil || c.Automation || c.Tenant != r.tenant.ID || s.GetAuthority() != app || s.GetPrincipalId() != c.ID {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The approval needs its original authorised member")
	}
	if r.probing {
		return r.Decide(c, owner, s, at)
	}
	if refusal := r.ProbeDecision(c, func(probe platform.Caller) *kernel.Error {
		_, refusal := platform.Decide(probe, owner, s, at)
		return refusal
	}); refusal != nil {
		return nil, refusal
	}
	work, ok := r.stateApp(r.tenant.app(platform.WorkApp)).(platform.ApprovalOwner)
	if !ok {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The approval owner is unavailable")
	}
	work.(host.AttachedWork).AttachWork(flowExecution{hostView: hostView{t: r.tenant, app: work, decision: r.stagedDecision}, rt: r})
	return work.RequestApproval(c, s, at)
}
