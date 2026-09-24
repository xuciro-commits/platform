package platform

import (
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
)

// Links are the entities linked to entity ("<type>/<id>") that c sees (ADR-0011).
func (c Caller) Links(entity string) []string {
	if c.rt == nil {
		return nil
	}
	return c.rt.Links(c, entity)
}

// Link relates two entities for c (an app linking what it just did, for example).
func (c Caller) Link(from, to *pb.EntityRef, key string, now time.Time) *kernel.Error {
	if c.rt == nil {
		return notFound()
	}
	return c.rt.Link(c, from, to, key, now)
}
