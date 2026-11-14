package kernel

import (
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func (s *Sequence) ActionOwner(owner, authority platform.App, key string) (platform.App, *kernel.Error) {
	if owner != nil {
		return owner, nil
	}
	if authority == nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
	}
	if !s.PriorOf(authority, key).Known() {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA, "The action definition is unavailable; refresh definitions")
	}
	return authority, nil
}

func SubmitTo(c platform.Caller, owner platform.App, missing *kernel.Error, s *pb.Submission, at time.Time) (*pb.ChangeRecord, *kernel.Error) {
	if owner == nil {
		return nil, missing
	}
	if declared, ok := owner.Manifest().Actions.Action(s.GetSchema().GetName()); ok && declared.Approval != nil && !c.Automation {
		return c.RequestApproval(owner.Manifest().ID, s, at)
	}
	return platform.Decide(c, owner, s, at)
}

func AttemptTo(c platform.Caller, owner func() platform.App, missing *kernel.Error, s *pb.Submission, at time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return platform.Attempt(c, func() (*pb.ChangeRecord, *kernel.Error) {
		app := owner()
		if app == nil {
			return nil, missing
		}
		return platform.Decide(c, app, s, at)
	})
}
