package data

import (
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

type SourceClaims struct {
	schemas  platform.SchemaRegistry
	Facts    platform.Facts
	Identity platform.Identity
}

func NewSourceClaims(kernel platform.KernelFactory) SourceClaims {
	schemas := kernel.Schemas([]*pb.SchemaRef{{Name: "integration.claim", Version: 1}})
	return SourceClaims{schemas: schemas, Facts: kernel.Facts(schemas), Identity: kernel.Identity(nil)}
}

func (c SourceClaims) Fork(kernel platform.KernelFactory, tenant string) SourceClaims {
	fork := SourceClaims{schemas: c.schemas, Facts: kernel.Facts(c.schemas), Identity: kernel.Identity(nil)}
	fork.Facts.Restore(tenant, c.Facts.Records(tenant))
	fork.Identity.Restore(c.Identity.State())
	return fork
}
