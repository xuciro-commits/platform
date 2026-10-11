package platformserver

import (
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func (d *stagedDecision) RequestApproval(c platform.Caller, app string, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	a, ok := d.tenant.app(app).(platform.App)
	if !ok || c.Automation || c.Tenant != d.tenant.ID || s.GetAuthority() != app || s.GetPrincipalId() != c.ID {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The approval needs its original authorised member")
	}
	if d.probing {
		return d.Decide(c, a, s, now)
	}
	return d.requestApproval(a, c.Member, s, now)
}

func (d *stagedDecision) requestApproval(a platform.App, m platform.Member, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	// Permission/attribute/rule checks occur privately. Even a rule that uses
	// another app cannot leave anything in the actual request transaction.
	c := platform.NewCaller(d, m, a.Manifest().ID, false, false)
	if err := d.ProbeDecision(c, func(probe platform.Caller) *kernel.Error {
		_, err := platform.Decide(probe, a, s, now)
		return err
	}); err != nil {
		return nil, err
	}
	owner, ok := d.tenant.app(platform.WorkApp).(platform.ApprovalOwner)
	if !ok {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The approval owner is unavailable")
	}
	return owner.RequestApproval(c, s, now)
}
