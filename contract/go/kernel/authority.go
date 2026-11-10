package kernel

import (
	"slices"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
)

type Authorities struct {
	current map[[2]string]*pb.AuthorityDeclaration
}

func NewAuthorities() *Authorities {
	return &Authorities{current: map[[2]string]*pb.AuthorityDeclaration{}}
}

func (a *Authorities) Declare(d *pb.AuthorityDeclaration) *Error {
	if slices.Contains([]string{d.GetTenantId(), d.GetDataClass(), d.GetAuthorityId()}, "") ||
		d.GetKind() == pb.AuthorityKind_AUTHORITY_KIND_UNSPECIFIED {
		return errorf(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	}
	key := [2]string{d.GetTenantId(), d.GetDataClass()}
	if d.GetEpoch() != a.current[key].GetEpoch()+1 {
		return errorf(pb.ErrorCode_ERROR_CODE_CONFLICT)
	}
	a.current[key] = d
	return nil
}

func (a *Authorities) declaration(s *pb.Submission) (*pb.AuthorityDeclaration, *Error) {
	d := a.current[[2]string{s.GetTenantId(), s.GetTarget().GetType()}]
	if d == nil {
		return nil, errorf(pb.ErrorCode_ERROR_CODE_NOT_FOUND)
	}
	return d, nil
}

func (a *Authorities) Authorize(s *pb.Submission) *Error {
	d, err := a.declaration(s)
	if err == nil && s.GetAuthority() != d.GetAuthorityId() {
		err = errorf(pb.ErrorCode_ERROR_CODE_NOT_AUTHORITY)
	}
	return err
}
