package platform

import (
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"time"
)

type Assignment struct {
	Close            bool
	Title, Body, Ref string
	To               []Recipient
	Due              time.Time
	Key              string
	Answers          []string
}

func (c Caller) Assign(r *pb.ChangeRecord, a Assignment) *Error {
	if c.rt == nil {
		return notFound()
	}
	return c.rt.Assign(c, r, a)
}

type Tasks interface {
	Assign(c Caller, r *pb.ChangeRecord, a Assignment) *Error
	Close(c Caller, r *pb.ChangeRecord, id string)
}
