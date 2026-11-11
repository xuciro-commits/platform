package data

import (
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
)

type SourceClaims struct {
	schemas  *kernel.SchemaRegistry
	Facts    *kernel.FactLog
	Identity *kernel.Identity
}

func NewSourceClaims() SourceClaims {
	schemas := kernel.NewSchemaRegistry([]*pb.SchemaRef{{Name: "integration.claim", Version: 1}})
	return SourceClaims{schemas: schemas, Facts: kernel.NewFactLog(schemas), Identity: kernel.NewIdentity(nil)}
}

func (c SourceClaims) Fork(tenant string) SourceClaims {
	fork := SourceClaims{schemas: c.schemas, Facts: kernel.NewFactLog(c.schemas), Identity: kernel.NewIdentity(nil)}
	fork.Facts.Restore(tenant, c.Facts.Records(tenant))
	fork.Identity.Restore(c.Identity.State())
	return fork
}
