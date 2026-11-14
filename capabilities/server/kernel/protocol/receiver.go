package protocol

import (
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

type Caller struct{ Tenant, Principal string }

type Policy func(caller Caller, s *pb.Submission) bool

type Receiver struct {
	Changes interface {
		SubmitChecked(*pb.Submission, time.Time, func() *platform.Error) (*pb.ChangeRecord, *platform.Error)
		Records(string) []*pb.ChangeRecord
	}
	Authorities interface {
		Authorize(*pb.Submission) *platform.Error
	}
	Policy Policy
}

func (r *Receiver) Receive(c Caller, s *pb.Submission, now time.Time, domain func() *platform.Error) (*pb.ChangeRecord, *platform.Error) {
	if s.GetTenantId() != c.Tenant || s.GetPrincipalId() != c.Principal {
		return nil, errorf(pb.ErrorCode_ERROR_CODE_POLICY_DENIED)
	}
	return r.Changes.SubmitChecked(s, now, func() *platform.Error {
		if err := r.Authorities.Authorize(s); err != nil {
			return err
		}
		if r.Policy == nil || !r.Policy(c, s) {
			return errorf(pb.ErrorCode_ERROR_CODE_POLICY_DENIED)
		}
		if domain != nil {
			return domain()
		}
		return nil
	})
}

func errorf(code pb.ErrorCode) *platform.Error { return &platform.Error{Code: code} }
