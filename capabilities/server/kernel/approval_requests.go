package kernel

import (
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

type ApprovalRequests struct {
	Tenant  string
	Runtime platform.Runtime
	App     func(string) platform.App
	Decide  func(platform.Caller, platform.App, *pb.Submission, time.Time) (*pb.ChangeRecord, *platform.Error)
	Probe   func(c platform.Caller, decide func(platform.Caller) *platform.Error) *platform.Error
}

func (h ApprovalRequests) Request(probeOnly bool, c platform.Caller, app string, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *platform.Error) {
	a := h.App(app)
	if a == nil || c.Automation || c.Tenant != h.Tenant || s.GetAuthority() != app || s.GetPrincipalId() != c.ID {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The approval needs its original authorised member")
	}
	if probeOnly {
		return h.Decide(c, a, s, now)
	}
	return h.Submit(a, c.Member, s, now)
}

func (h ApprovalRequests) Submit(a platform.App, m platform.Member, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *platform.Error) {
	c := platform.NewCaller(h.Runtime, m, a.Manifest().ID, false, false)
	if err := h.Probe(c, func(probe platform.Caller) *platform.Error {
		_, err := platform.Decide(probe, a, s, now)
		return err
	}); err != nil {
		return nil, err
	}
	owner := h.App(platform.WorkApp)
	if owner == nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The approval owner is unavailable")
	}
	return owner.(platform.ApprovalOwner).RequestApproval(c, s, now)
}
