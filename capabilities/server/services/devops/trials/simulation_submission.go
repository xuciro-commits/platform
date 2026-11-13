package trials

import (
	"google.golang.org/protobuf/proto"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type SubmissionTrial struct {
	Admits        func(platform.Member) *kernel.Error
	Authorization func(platform.Member) platform.Member
	Quarantined   func() bool
	Authorize     func(platform.Member) *kernel.Error
	Member        func(id string) (platform.Member, bool)
	Owner         func(schema string) platform.App
	Current       func(platform.App, *pb.Submission) *pb.Submission
	Bound         func(platform.Member, *pb.Submission) *kernel.Error
	Tenant        string
}

func (h SubmissionTrial) Prepare(member platform.Member, as, branch string, s *pb.Submission) (platform.Member, platform.Member, platform.App, *pb.Submission, *kernel.Error) {
	if err := h.Admits(member); err != nil {
		return member, member, nil, nil, err
	}
	member = h.Authorization(member)
	if !member.Active() {
		return member, member, nil, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	if s == nil || s.GetSchema() == nil || s.GetTarget() == nil {
		return member, member, nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "a submission with a schema and a target is required")
	}
	if h.Quarantined() {
		return member, member, nil, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	m := member
	if as != "" && as != member.ID || branch != "" {
		if refusal := h.Authorize(member); refusal != nil {
			return member, m, nil, nil, refusal
		}
	}
	if as != "" && as != member.ID {
		other, ok := h.Member(as)
		if !ok {
			return member, m, nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "{member} is not a member here", as)
		}
		m = other
	}
	a := h.Owner(s.GetSchema().GetName())
	if a == nil {
		return member, m, nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA, "{action} cannot be tried", s.GetSchema().GetName())
	}
	s = proto.Clone(s).(*pb.Submission)
	s.TenantId, s.PrincipalId, s.Authority = h.Tenant, m.ID, a.Manifest().ID
	s = h.Current(a, s)
	if err := h.Bound(m, s); err != nil {
		return member, m, a, s, err
	}
	if s.GetIdempotencyKey() == "" {
		s.IdempotencyKey = "simulate"
	}
	return member, m, a, s, nil
}
