package platformserver

import (
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func conflictRefusal(err error) *kernel.Error {
	if err == nil {
		return nil
	}
	if refusal, ok := err.(*kernel.Error); ok {
		return refusal
	}
	return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
}
