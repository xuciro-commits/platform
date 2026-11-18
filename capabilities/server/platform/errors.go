package platform

import pb "platformkernel/gen/platform/kernel/v1alpha1"

type Error struct {
	Code            pb.ErrorCode
	Message         string
	CurrentRevision *uint32 `json:",omitempty"`
}

func (e *Error) Error() string { return e.Code.String() }
