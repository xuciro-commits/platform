package platform

import (
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
)

// Assignment asks people to do something about a record (ADR-0017): a task in
// their inbox, due by a time, escalated when it is late. To lists who may take
// it; Key keeps one open task per app and key.
type Assignment struct {
	Title string
	Body  string
	Ref   string // the record it is about, "<type>/<id>"
	To    []Recipient
	Due   time.Time
	Key   string
	// Answers are what the person may answer (a flow's Ask); none: "done".
	Answers []string
}

// Assign creates a task as part of the decision r (inside its input, so
// replay creates it again).
func (c Caller) Assign(r *pb.ChangeRecord, a Assignment) *kernel.Error {
	if c.rt == nil {
		return notFound()
	}
	return c.rt.Assign(c, r, a)
}
