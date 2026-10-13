package platformserver

import (
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/host"
	"platformserver/platform"
)

func (h hostView) ApprovalExecution(c platform.Caller, a platform.ApprovalRequest) (host.WorkHost, *kernel.Error) {
	if a.Release == "" {
		return h, nil
	}
	sealed, err := h.t.releases.candidate(a.Release)
	if err != nil {
		return nil, migrationRefusal(err)
	}
	root := platform.AssetRef{App: a.App, Kind: platform.AssetAction, Name: a.Action}
	closed, err := platform.Candidate([]platform.AssetRef{root}, sealed.Assets)
	if err != nil {
		return nil, migrationRefusal(err)
	}
	execution, err := h.retainedExecution(c, a.Release, closed.Assets)
	if err != nil {
		return nil, migrationRefusal(err)
	}
	return execution.(host.WorkHost), nil
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
