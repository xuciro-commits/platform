package platform

import (
	"encoding/json"
	"fmt"
	"slices"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
)

// CompactFacts uses the owner's original packed fact representation. A later
// committed image may contain more facts, but readers see only their applied
// immutable prefix; private decisions keep their own new facts separately.
func CompactFacts(log *kernel.FactLog, load func() (json.RawMessage, error)) {
	log.Compact(packedFactArchive{load: load})
}

type packedFactArchive struct {
	load func() (json.RawMessage, error)
}

func (a packedFactArchive) Records(_ string, through int) []*pb.FactRecord {
	if through == 0 {
		return nil
	}
	raw, err := a.load()
	if err != nil {
		panic(fmt.Errorf("fact history: %w", err))
	}
	records, err := Unprotos[*pb.FactRecord](raw)
	if err != nil {
		panic(fmt.Errorf("fact history: %w", err))
	}
	if len(records) < through {
		panic(fmt.Errorf("fact history prefix is incomplete"))
	}
	return records[:through:through]
}
func (a packedFactArchive) Accepted(tenant, key string, through int) *pb.FactRecord {
	for _, r := range a.Records(tenant, through) {
		if r.GetFact().GetTenantId() == tenant && r.GetFact().GetIdempotencyKey() == key {
			return r
		}
	}
	return nil
}
func (a packedFactArchive) Contains(tenant, id string, through int) bool {
	return slices.ContainsFunc(a.Records(tenant, through), func(r *pb.FactRecord) bool { return r.GetFact().GetTenantId() == tenant && r.GetFactId() == id })
}
