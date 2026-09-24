package kernel

import (
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
)

// Caller is the principal a receiver authenticated (K6).
type Caller struct{ Tenant, Principal string }

// Policy decides whether a caller may submit s; supplied by the domain (K6 T3).
type Policy func(caller Caller, s *pb.Submission) bool

// Receiver applies the kernel's receiving order (K6 T2) for one authority.
type Receiver struct {
	Changes     *ChangeLog
	Authorities *Authorities
	Policy      Policy
}

// Receive accepts or rejects a submission; domain runs the domain's rules (K4 C10).
func (r *Receiver) Receive(c Caller, s *pb.Submission, now time.Time, domain func() *Error) (*pb.ChangeRecord, *Error) {
	if s.GetTenantId() != c.Tenant || s.GetPrincipalId() != c.Principal {
		return nil, errorf(pb.ErrorCode_ERROR_CODE_POLICY_DENIED) // T1
	}
	return r.Changes.SubmitChecked(s, now, func() *Error {
		if err := r.Authorities.Authorize(s); err != nil {
			return err // A3
		}
		if r.Policy == nil || !r.Policy(c, s) {
			return errorf(pb.ErrorCode_ERROR_CODE_POLICY_DENIED) // T3
		}
		if domain != nil {
			return domain()
		}
		return nil
	})
}
