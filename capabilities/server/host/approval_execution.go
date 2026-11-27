package host

import (
	runtimekernel "platformserver/kernel"
	"platformserver/kernel/protocol"
	releaseowner "platformserver/services/devops/releases"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"

	"platformserver/platform"
)

func (h hostView) ApprovalRelease(app, action, typ string) (string, *platform.Error) {
	release, err := releaseowner.ApprovedRelease(
		releaseowner.ActivatedReleases(PublicationResolver(), h.t.Committed.AllAnswers("active"), h.ActiveRelease()),
		app, action, typ, &h.t.releases, h.t.releaseAcceptance().Running)
	return release, runtimekernel.ConflictRefusal(err)
}

func (h flowExecution) ApprovalRelease(app, action, typ string) (string, *platform.Error) {
	closed, err := releaseowner.StoredApprovalClosure(&h.rt.view.releases, h.rt.release, app, action, typ)
	if err != nil || closed.ID == "" {
		return "", runtimekernel.ConflictRefusal(err)
	}
	return h.rt.release, nil
}

func (h hostView) ApprovalExecution(c platform.Caller, a protocol.ApprovalRequest) (protocol.WorkHost, *platform.Error) {
	if a.Release == "" {
		return h, nil
	}
	bound, err := releaseowner.ApprovalExecution(&h.t.releases, a, func(assets []platform.ReleaseAsset) (protocol.WorkHost, error) {
		execution, err := h.retainedExecution(c, a.Release, assets)
		if err != nil {
			return nil, err
		}
		return execution.(protocol.WorkHost), nil
	})
	if err != nil {
		return nil, runtimekernel.ConflictRefusal(err)
	}
	return bound, nil
}

func (r *flowExecutionRuntime) ProbeDecision(c platform.Caller, decide func(platform.Caller) *platform.Error) *platform.Error {
	probe := r.stagedDecision.privateFork()
	bound := *r
	bound.stagedDecision = probe
	return probe.runProbe(c, func(_ platform.Caller) *platform.Error {
		return decide(platform.NewCaller(&bound, c.Member, c.App, c.Replaying, c.Automation))
	})
}

func (r *flowExecutionRuntime) RequestApproval(c platform.Caller, app string, s *pb.Submission, at time.Time) (*pb.ChangeRecord, *platform.Error) {
	return runtimekernel.ApprovalRequests{
		Tenant: r.tenant.ID, Runtime: r, App: func(id string) platform.Service {
			if id == protocol.WorkApp {
				return r.stateApp(r.tenant.App(protocol.WorkApp))
			}
			return r.view.App(id)
		},
		Decide: r.Decide, Probe: r.ProbeDecision,
		Bind: func(work protocol.ApprovalOwner) {
			ctx := hostView{t: r.tenant, app: work, decision: r.stagedDecision}
			bindings := ctx.ownerBindings()
			bindings.Work = flowExecution{hostView: ctx, rt: r}
			r.tenant.services.BindOwner(work, bindings)
		},
	}.Request(r.ProbeOnly, c, app, s, at)
}
