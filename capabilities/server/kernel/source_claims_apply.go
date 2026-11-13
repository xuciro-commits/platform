package kernel

import (
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/acceptedformat"
)

func (c SourceClaims) RecordClaims(tenant string, result acceptedformat.Record, receipt *pb.ChangeRecord) (func() error, error) {
	var source *ClaimSet
	if len(result.Claims) > 0 {
		source = &ClaimSet{Origin: result.Origin, Claims: result.Claims}
		if _, err := c.Prepare(tenant, result.Row.Type, result.Row.ID, receipt.GetChangeId(), source); err != nil {
			return nil, err
		}
	}
	return func() error {
		return c.Apply(tenant, result.Row.Type, result.Row.ID, receipt.GetChangeId(), result.At, source)
	}, nil
}
