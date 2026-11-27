package kernel

import (
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

func (s *Sequence) SubmissionPrior(owner, authority platform.Service, key string) (platform.Service, Prior, *platform.Error) {
	if owner != nil {
		return owner, s.PriorOf(owner, key), nil
	}
	if authority == nil {
		return nil, Prior{}, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
	}
	prior := s.PriorOf(authority, key)
	if !prior.Known() {
		return nil, Prior{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA, "The action definition is unavailable; refresh definitions")
	}
	return authority, prior, nil
}

func SubmitTo(c platform.Caller, owner platform.Service, missing *platform.Error, s *pb.Submission, at time.Time) (*pb.ChangeRecord, *platform.Error) {
	if owner == nil {
		return nil, missing
	}
	if declared, ok := owner.Manifest().Actions.Action(s.GetSchema().GetName()); ok && declared.Approval != nil && !c.Automation {
		return c.RequestApproval(owner.Manifest().ID, s, at)
	}
	return platform.Decide(c, owner, s, at)
}

func AttemptTo(c platform.Caller, owner func() platform.Service, missing *platform.Error, s *pb.Submission, at time.Time) (*pb.ChangeRecord, *platform.Error) {
	return platform.Attempt(c, func() (*pb.ChangeRecord, *platform.Error) {
		app := owner()
		if app == nil {
			return nil, missing
		}
		return platform.Decide(c, app, s, at)
	})
}
