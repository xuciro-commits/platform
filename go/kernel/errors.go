package kernel

import pb "platformkernel/gen/platform/kernel/v1alpha1"

// Error carries a contract error code (Contract/spec/errors.md).
type Error struct{ Code pb.ErrorCode }

func (e *Error) Error() string { return e.Code.String() }

func errorf(code pb.ErrorCode) *Error { return &Error{Code: code} }
