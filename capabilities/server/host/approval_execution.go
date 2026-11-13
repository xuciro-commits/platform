package host

import (
	"maps"
	acceptedformat "platformserver/kernel/acceptedformat"
	releaseowner "platformserver/services/devops/releases"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

	"platformserver/platform"
)

func (h hostView) ApprovalRelease(app, action, typ string) (string, *kernel.Error) {
	activated := map[string]bool{}
	for key, raw := range h.t.Committed.AllAnswers("active") {
		if !strings.HasPrefix(key, "release:") {
			continue
		}
		result, err := acceptedformat.DecodeRelease(raw)
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
		if err == nil && releaseowner.SameRetainedCapability(running, closed) {
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

func (t *Tenant) approvalClosure(release, app, action, typ string) (platform.ReleaseCandidate, *kernel.Error) {
	if release == "" {
		return platform.ReleaseCandidate{}, nil
	}
	sealed, err := t.releases.Candidate(release)
	if err != nil {
		return platform.ReleaseCandidate{}, conflictRefusal(err)
	}
	root, found := releaseowner.ApprovalRoot(sealed.Assets, app, action, typ)
	if !found {
		return platform.ReleaseCandidate{}, nil
	}
	closed, err := sealed.Closure([]platform.AssetRef{root})
	return closed, conflictRefusal(err)
}

func (h hostView) ApprovalExecution(c platform.Caller, a platform.ApprovalRequest) (platform.WorkHost, *kernel.Error) {
	if a.Release == "" {
		return h, nil
	}
	sealed, err := h.t.releases.Candidate(a.Release)
	if err != nil {
		return nil, conflictRefusal(err)
	}
	typ, _, _ := strings.Cut(a.Target, "/")
	root, _ := releaseowner.ApprovalRoot(sealed.Assets, a.App, a.Action, typ)
	closed, err := sealed.Closure([]platform.AssetRef{root})
	if err != nil {
		return nil, conflictRefusal(err)
	}
	execution, err := h.retainedExecution(c, a.Release, closed.Assets)
	if err != nil {
		return nil, conflictRefusal(err)
	}
	bound := execution.(platform.WorkHost)
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
	owner := r.view.App(app)
	if owner == nil || c.Automation || c.Tenant != r.tenant.ID || s.GetAuthority() != app || s.GetPrincipalId() != c.ID {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The approval needs its original authorised member")
	}
	if r.ProbeOnly {
		return r.Decide(c, owner, s, at)
	}
	if refusal := r.ProbeDecision(c, func(probe platform.Caller) *kernel.Error {
		_, refusal := platform.Decide(probe, owner, s, at)
		return refusal
	}); refusal != nil {
		return nil, refusal
	}
	work, ok := r.stateApp(r.tenant.App(platform.WorkApp)).(platform.ApprovalOwner)
	if !ok {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The approval owner is unavailable")
	}
	context := hostView{t: r.tenant, app: work, decision: r.stagedDecision}
	bindings := context.ownerBindings()
	bindings.Work = flowExecution{hostView: context, rt: r}
	r.tenant.services.BindOwner(work, bindings)
	return work.RequestApproval(c, s, at)
}
