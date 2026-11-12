package kernel

import (
	"fmt"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	contract "platformkernel/kernel"
	"platformserver/kernel/acceptedformat"
)

type ClaimSet struct {
	Restore bool
	Origin  string
	Claims  []acceptedformat.SourceClaim
}

type SourceClaims struct {
	schemas  *contract.SchemaRegistry
	Facts    *contract.FactLog
	Identity *contract.Identity
}

func NewSourceClaims() SourceClaims {
	schemas := contract.NewSchemaRegistry([]*pb.SchemaRef{{Name: "integration.claim", Version: 1}})
	return SourceClaims{schemas: schemas, Facts: contract.NewFactLog(schemas), Identity: contract.NewIdentity(nil)}
}

func (c SourceClaims) Fork(tenant string) SourceClaims {
	fork := SourceClaims{schemas: c.schemas, Facts: contract.NewFactLog(c.schemas), Identity: contract.NewIdentity(nil)}
	fork.Facts.Restore(tenant, c.Facts.Records(tenant))
	fork.Identity.Restore(c.Identity.State())
	return fork
}

func (c SourceClaims) Prepare(tenant, typ, id, change string, prepared *ClaimSet) ([]*pb.Fact, error) {
	if prepared == nil {
		return nil, nil
	}
	facts := make([]*pb.Fact, 0, len(prepared.Claims))
	for _, claim := range prepared.Claims {
		fact := &pb.Fact{TenantId: tenant, Kind: pb.FactKind_FACT_KIND_CLAIM, Subject: &pb.EntityRef{Type: typ, Id: id}, Attribute: claim.Attribute,
			Schema:         &pb.SchemaRef{Name: "integration.claim", Version: 1},
			Provenance:     &pb.Provenance{Source: &pb.Provenance_ConnectorId{ConnectorId: claim.Connector}, SourceTime: timestamppb.New(claim.SourceTime), Confidence: 1},
			IdempotencyKey: typ + "/" + id + "/" + change + "/" + claim.Attribute, Payload: claim.Payload}
		if old := c.Facts.Accepted(tenant, fact.IdempotencyKey); old != nil && !proto.Equal(old.GetFact(), fact) {
			return nil, fmt.Errorf("source claim idempotency conflict")
		}
		facts = append(facts, fact)
	}
	if prepared.Origin != "" {
		resolved, failure := c.Identity.Resolve(&pb.EntityRef{Type: typ, Id: prepared.Origin})
		if failure == nil && (len(resolved) != 1 || resolved[0] != (contract.Ref{Type: typ, ID: id}) && resolved[0] != (contract.Ref{Type: typ, ID: prepared.Origin})) {
			return nil, fmt.Errorf("source identity already resolves elsewhere")
		}
	}
	return facts, nil
}

func (c SourceClaims) Apply(tenant, typ, id, change string, at time.Time, prepared *ClaimSet) error {
	if prepared == nil {
		return nil
	}
	facts, err := c.Prepare(tenant, typ, id, change, prepared)
	if err != nil {
		return err
	}
	for _, fact := range facts {
		if _, failure := c.Facts.Record(fact, at); failure != nil {
			return fmt.Errorf("accepted source claim: %v", failure)
		}
	}
	target := &pb.EntityRef{Type: typ, Id: id}
	if _, failure := c.Identity.Resolve(target); failure != nil {
		if failure := c.Identity.Create(target); failure != nil {
			return fmt.Errorf("accepted source identity: %v", failure)
		}
	}
	if prepared.Origin != "" {
		origin := &pb.EntityRef{Type: typ, Id: prepared.Origin}
		resolved, failure := c.Identity.Resolve(origin)
		if failure != nil {
			if failure := c.Identity.Create(origin); failure != nil {
				return fmt.Errorf("accepted source origin: %v", failure)
			}
			resolved = []contract.Ref{{Type: typ, ID: prepared.Origin}}
		}
		if len(resolved) == 1 && resolved[0] == (contract.Ref{Type: typ, ID: prepared.Origin}) {
			if failure := c.Identity.AddRedirect(&pb.Redirect{From: origin, To: []*pb.EntityRef{target}, Kind: pb.RedirectKind_REDIRECT_KIND_MERGE}); failure != nil {
				return fmt.Errorf("accepted source redirect: %v", failure)
			}
		}
	}
	return nil
}
