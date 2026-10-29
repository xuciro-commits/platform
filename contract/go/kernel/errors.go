package kernel

import pb "platformkernel/gen/platform/kernel/v1alpha1"

type Error struct {
	Code    pb.ErrorCode
	Message string
}

func (e *Error) Error() string { return e.Code.String() }

func errorf(code pb.ErrorCode) *Error { return &Error{Code: code} }
