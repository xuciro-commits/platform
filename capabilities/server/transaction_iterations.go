package platformserver

import (
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func validateTxnIterations(app string, iterations []platform.TxnIteration) error {
	total, steps := 0, map[string]bool{}
	for _, iteration := range iterations {
		hash, err := hex.DecodeString(iteration.Definition)
		if err != nil || len(hash) != 32 || iteration.Step == "" || iteration.Set == "" || steps[iteration.Step] || !strings.HasPrefix(iteration.Object, app+".") {
			return fmt.Errorf("transaction iteration witness is invalid")
		}
		steps[iteration.Step] = true
		ids := map[string]bool{}
		for _, id := range iteration.IDs {
			if id == "" || ids[id] {
				return fmt.Errorf("transaction iteration identities must be unique")
			}
			ids[id] = true
		}
		total += len(iteration.IDs)
		if total > platform.TxnMaxIterations {
			return fmt.Errorf("transaction has %d iterations; limit is %d", total, platform.TxnMaxIterations)
		}
	}
	return nil
}

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
	if err := validateTxnIterations(c.App, all); err != nil {
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
