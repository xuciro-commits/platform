package platformserver

import (
	resultformat "platformserver/internal/acceptance/result"

	"slices"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func (d *stagedDecision) CaptureTxnIteration(c platform.Caller, receipt *pb.ChangeRecord, iteration platform.TxnIteration) *kernel.Error {
	fail := func(message string) *kernel.Error {
		err := platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, message)
		if d.failure == nil {
			d.failure = err
		}
		return err
	}
	if !c.Staging() || c.Replaying || receipt.GetChangeId() == "" || receipt.GetSubmission().GetAuthority() != c.App || receipt.GetSubmission().GetPrincipalId() != c.ID || receipt.GetSubmission().GetTenantId() != d.tenant.ID {
		return fail("Iteration capture is outside its private authority")
	}
	all := append(slices.Clone(d.iterations), iteration)
	if err := resultformat.ValidateTxnIterations(c.App, all); err != nil {
		return fail(err.Error())
	}
	for _, id := range iteration.IDs {
		if !d.Readable(c, iteration.Object+"/"+id) {
			return fail("Iteration member is unavailable")
		}
	}
	iteration.IDs = slices.Clone(iteration.IDs)
	d.iterations = append(d.iterations, iteration)
	return nil
}
