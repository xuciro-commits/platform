package kernel

import pb "platformkernel/gen/platform/kernel/v1alpha1"

// Error carries a contract error code (contract/spec/errors.md) and, for
// people, why: the message is never contract, and vectors compare codes only.
type Error struct {
	Code    pb.ErrorCode
	Message string
}

func (e *Error) Error() string { return e.Code.String() }

func errorf(code pb.ErrorCode) *Error { return &Error{Code: code} }
