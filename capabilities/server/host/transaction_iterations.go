package host

import (
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/services/actions"
)

func (d *stagedDecision) CaptureTxnIteration(c platform.Caller, receipt *pb.ChangeRecord, iteration platform.TxnIteration) *kernel.Error {
	captured, refusal := actions.CaptureTxnIteration(d.tenant.ID, c, receipt, d.Iterations, iteration, func(ref string) bool { return d.Readable(c, ref) })
	if refusal != nil {
		if d.Failure == nil {
			d.Failure = refusal
		}
		return refusal
	}
	d.Iterations = append(d.Iterations, captured)
	return nil
}
